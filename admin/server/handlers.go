package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Campus time is fixed UTC+5:30 (India has no daylight saving).
var ist = time.FixedZone("IST", 5*3600+1800)

type AttendanceWindow struct {
	ID    string `yaml:"id" json:"id"`
	Start int    `yaml:"-" json:"-"`
	End   int    `yaml:"-" json:"-"`
}

type AttendanceWindows struct {
	IN  []AttendanceWindow `yaml:"in_windows" json:"in_windows"`
	OUT []AttendanceWindow `yaml:"out_windows" json:"out_windows"`
}

type Config struct {
	DBFile, Listen                   string
	GeoLat, GeoLon                   float64
	RadiusM, MaxAccM                 float64
	WinStart, WinEnd                 int // minutes after midnight, campus time
	AttendanceWindows                AttendanceWindows
	AdminListen, AdminTokenSHA256    string
	AuditRetentionDays, AuditMaxRows int
}

type Server struct {
	db  *sql.DB
	cfg atomic.Pointer[Config]
	now func() time.Time // injectable clock; the server clock decides everything

	// admin panel state (see admin*.go)
	base      Config     // effective config before any stored override (i.e. the YAML values)
	startedAt time.Time  // for uptime in /admin/api/status
	cfgMu     sync.Mutex // serialises config PATCH/DELETE (read-modify-write)
	warnMu    sync.Mutex
	warnings  map[string]string // stored override key -> why it was ignored at startup
	throttle  authThrottle
	netAddrs  func() []netAddr // injectable for tests

	shutdownReq  chan struct{} // closed when the admin panel asks the process to stop
	shutdownOnce sync.Once
}

func newServer(db *sql.DB, cfg Config, now func() time.Time) *Server {
	s := &Server{db: db, now: now, base: cfg, startedAt: now(), netAddrs: listNetAddrs,
		shutdownReq: make(chan struct{})}
	s.setConfig(cfg)
	return s
}

// requestShutdown asks serve() to stop gracefully. Safe to call repeatedly.
func (s *Server) requestShutdown() { s.shutdownOnce.Do(func() { close(s.shutdownReq) }) }

func (s *Server) config() Config {
	if s == nil || s.cfg.Load() == nil {
		return Config{}
	}
	return *s.cfg.Load()
}

func (s *Server) setConfig(c Config) { s.cfg.Store(&c) }

// policySnap is the geofence/window in force when a request was judged. Config
// is editable, so "rejected outside_area at 9740 m" only makes sense next to
// the radius that applied at that moment.
type policySnap struct {
	GeoLat   float64 `json:"geo_lat"`
	GeoLon   float64 `json:"geo_lon"`
	RadiusM  float64 `json:"radius_m"`
	MaxAccM  float64 `json:"max_accuracy_m"`
	WinStart string  `json:"window_start"`
	WinEnd   string  `json:"window_end"`
}

func snapPolicy(c Config) *policySnap {
	return &policySnap{GeoLat: c.GeoLat, GeoLon: c.GeoLon, RadiusM: c.RadiusM, MaxAccM: c.MaxAccM,
		WinStart: fmtHM(c.WinStart), WinEnd: fmtHM(c.WinEnd)}
}

type auditCtxKey struct{}

type auditRec struct {
	FacultyID, ChallengeID                    string
	LatMicro, LonMicro, AccuracyCm, DistanceM *int64
	Policy                                    *policySnap
}

func auditFrom(ctx context.Context) *auditRec {
	if ctx == nil {
		return &auditRec{}
	}
	if a, ok := ctx.Value(auditCtxKey{}).(*auditRec); ok && a != nil {
		return a
	}
	return &auditRec{}
}

// ---- errors ----

type apiErr struct{ code string }

func (e apiErr) Error() string { return e.code }

var errTable = map[string]struct {
	status int
	msg    string
}{
	"bad_request":         {400, "malformed request"},
	"not_enrolled":        {403, "no phone enrolled; visit the office"},
	"code_invalid":        {403, "enrollment code is unknown, used or expired"},
	"challenge_invalid":   {400, "unknown challenge"},
	"challenge_expired":   {410, "challenge expired"},
	"challenge_used":      {409, "challenge already used"},
	"bad_signature":       {401, "signature does not verify"},
	"closed":              {409, "attendance is closed right now"},
	"poor_accuracy":       {422, "GPS accuracy is too low"},
	"outside_area":        {422, "you are outside the allowed area"},
	"already_marked":      {409, "already marked today"},
	"too_many_challenges": {429, "too many open challenges"},

	// admin panel only; the phone-facing handlers never produce these
	"unauthorized":      {401, "missing or wrong admin token"},
	"too_many_attempts": {429, "too many failed attempts; try again in a minute"},
	"bad_date":          {400, "date must be a real date as YYYY-MM-DD"},
	"bad_range":         {400, "from and to are required, and from must not be after to"},
	"range_too_large":   {400, "date range is too large"},
	"invalid_config":    {422, "configuration is invalid"},
	"not_found":         {404, "not found"},
	"already_exists":    {409, "that employee_id already exists"},
	"invalid_input":     {422, "one or more fields are invalid"},
	"future_date":       {400, "date must not be in the future"},
	"confirm_required":  {400, "confirmation is missing or wrong"},

	// early-exit approval (admin only)
	"already_approved": {409, "this exit request was already approved"},
	"not_in":           {409, "faculty member is not currently IN"},
}

type handler func(w http.ResponseWriter, r *http.Request) (int, any, error)

func (s *Server) wrap(endpoint string, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &auditRec{}
		r = r.WithContext(context.WithValue(r.Context(), auditCtxKey{}, rec))
		ts := s.now() // server clock (injectable); wall time only for the duration
		t0 := time.Now()
		status, body, err := h(w, r)
		outcome := "ok"
		if err != nil {
			var ae apiErr
			if errors.As(err, &ae) {
				e := errTable[ae.code]
				log.Printf("reject %s %s: %s", r.Method, r.URL.Path, ae.code)
				status, body = e.status, map[string]string{"error": ae.code, "message": e.msg}
				outcome = ae.code
			} else {
				log.Printf("internal error %s: %v", r.URL.Path, err)
				status, body = 500, map[string]string{"error": "internal", "message": "internal error"}
				outcome = "internal"
			}
		}
		// The audit row is written before the response so that a client which
		// has seen the response can always see its audit row. A failure here is
		// logged and never changes what the phone receives.
		if endpoint != "" {
			if err := s.writeAudit(r, endpoint, ts, time.Since(t0), status, outcome, rec); err != nil {
				log.Printf("audit %s failed: %v", endpoint, err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) writeAudit(r *http.Request, endpoint string, ts time.Time, dur time.Duration, status int, outcome string, rec *auditRec) error {
	if s.db == nil {
		return nil
	}
	if rec == nil {
		rec = &auditRec{}
	}
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	var policy any
	if rec.Policy != nil {
		if b, err := json.Marshal(rec.Policy); err == nil {
			policy = string(b)
		}
	}
	// Deliberately never stored: signatures, nonces, enrollment codes, public keys.
	_, err := s.db.Exec(`INSERT INTO audit_log
		(ts, endpoint, method, path, status, outcome, faculty_id, challenge_id,
		 remote_ip, user_agent, lat_micro, lon_micro, accuracy_cm, distance_m, policy, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts.Unix(), endpoint, r.Method, r.URL.Path, status, outcome,
		nullStr(rec.FacultyID), nullStr(rec.ChallengeID), remoteIP(r), ua,
		rec.LatMicro, rec.LonMicro, rec.AccuracyCm, rec.DistanceM, policy, dur.Milliseconds())
	return err
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apiErr{"bad_request"}
	}
	if _, err := dec.Token(); err != io.EOF { // trailing data
		return apiErr{"bad_request"}
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", s.wrap("register", s.register))
	mux.HandleFunc("POST /challenge", s.wrap("challenge", s.challenge))
	mux.HandleFunc("POST /attendance", s.wrap("attendance", s.attendance))
	mux.HandleFunc("POST /history", s.wrap("history", s.history))
	return mux
}

// ---- policy ----

func windowOpenAt(now time.Time, cfg Config) bool {
	t := now.In(ist)
	m := t.Hour()*60 + t.Minute()
	if cfg.WinStart == 0 && cfg.WinEnd == 24*60 {
		return true
	}
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	if cfg.WinEnd == 24*60 {
		return m >= cfg.WinStart
	}
	return m >= cfg.WinStart && m < cfg.WinEnd
}

type windowInfo struct {
	Open       bool
	Reason     string
	NextChange time.Time
}

// nextWeekdayStart is the start of the window on the first weekday after day.
func nextWeekdayStart(day time.Time, startMin int) time.Time {
	d := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, day.Location())
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	return d.Add(time.Duration(startMin) * time.Minute)
}

// windowState explains windowOpenAt: whether the window is open, why not, and
// when that next changes. A zero NextChange means "never" (open around the clock).
func (s *Server) windowState(now time.Time, cfg Config) windowInfo {
	day := now.In(ist)
	m := day.Hour()*60 + day.Minute()
	midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	if cfg.WinStart == 0 && cfg.WinEnd == 24*60 {
		return windowInfo{Open: true, Reason: "open"}
	}
	if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return windowInfo{Open: false, Reason: "weekend", NextChange: nextWeekdayStart(day, cfg.WinStart)}
	}
	if m < cfg.WinStart {
		return windowInfo{Open: false, Reason: "before_start", NextChange: midnight.Add(time.Duration(cfg.WinStart) * time.Minute)}
	}
	if m >= cfg.WinEnd {
		return windowInfo{Open: false, Reason: "after_end", NextChange: nextWeekdayStart(day, cfg.WinStart)}
	}
	return windowInfo{Open: true, Reason: "open", NextChange: midnight.Add(time.Duration(cfg.WinEnd) * time.Minute)}
}

func (s *Server) windowOpen(now time.Time) bool { return windowOpenAt(now, s.config()) }

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthR = 6371000.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthR * math.Asin(math.Min(1, math.Sqrt(a)))
}

// twoMonthsBack returns the date two calendar months earlier, clamping the day
// (e.g. 30 April -> 28/29 February) instead of overflowing into March.
func twoMonthsBack(t time.Time) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m-2, 1, 0, 0, 0, 0, t.Location())
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, t.Location())
}

// ---- shared challenge check (steps 2-3 of /attendance, same for /history) ----

type queryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func checkChallenge(ctx context.Context, q queryer, challengeID, facultyID string, nowU int64) (nonce string, pub []byte, err error) {
	var expires, used int64
	err = q.QueryRowContext(ctx, `SELECT c.nonce, c.expires_at, c.used, f.public_key
		FROM challenges c LEFT JOIN faculty f ON f.id = c.faculty_id
		WHERE c.id = ? AND c.faculty_id = ?`, challengeID, facultyID).Scan(&nonce, &expires, &used, &pub)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil, apiErr{"challenge_invalid"}
	case err != nil:
		return "", nil, err
	case used != 0:
		return "", nil, apiErr{"challenge_used"}
	case nowU >= expires:
		return "", nil, apiErr{"challenge_expired"}
	case len(pub) == 0:
		return "", nil, apiErr{"not_enrolled"}
	}
	return nonce, pub, nil
}

// consumeChallenge marks the challenge used; exactly one caller can win.
func consumeChallenge(ctx context.Context, tx *sql.Tx, id string, nowU int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE challenges SET used=1 WHERE id=? AND used=0 AND expires_at>?`, id, nowU)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return apiErr{"challenge_used"}
	}
	return nil
}

// ---- POST /register ----

func (s *Server) register(w http.ResponseWriter, r *http.Request) (int, any, error) {
	var req struct {
		Code      string `json:"code"`
		PublicKey string `json:"public_key"`
	}
	if err := decode(w, r, &req); err != nil {
		return 0, nil, err
	}
	if req.Code == "" {
		return 0, nil, apiErr{"bad_request"}
	}
	der, err := parsePublicKey(req.PublicKey)
	if err != nil {
		return 0, nil, apiErr{"bad_request"}
	}
	var id, name string
	err = s.db.QueryRowContext(r.Context(), `UPDATE faculty SET public_key=?, code_hash=NULL, code_expires=NULL
		WHERE code_hash=? AND code_expires>? RETURNING id, name`,
		der, hashCode(req.Code), s.now().Unix()).Scan(&id, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, apiErr{"code_invalid"}
	}
	if err != nil {
		return 0, nil, err
	}
	auditFrom(r.Context()).FacultyID = id
	return 201, map[string]string{"faculty_id": id, "name": name}, nil
}

// ---- POST /challenge ----

func (s *Server) challenge(w http.ResponseWriter, r *http.Request) (int, any, error) {
	var req struct {
		FacultyID string `json:"faculty_id"`
	}
	if err := decode(w, r, &req); err != nil {
		return 0, nil, err
	}
	auditFrom(r.Context()).FacultyID = req.FacultyID
	nowU := s.now().Unix()
	id, nonce := randHex(16), randNonce()
	err := withTx(r.Context(), s.db, func(tx *sql.Tx) error {
		var hasKey int
		err := tx.QueryRowContext(r.Context(), `SELECT public_key IS NOT NULL FROM faculty WHERE id=?`, req.FacultyID).Scan(&hasKey)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && hasKey == 0) {
			return apiErr{"not_enrolled"}
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM challenges WHERE expires_at < ?`, nowU-600); err != nil {
			return err
		}
		var live int
		if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM challenges WHERE faculty_id=? AND used=0 AND expires_at>?`,
			req.FacultyID, nowU).Scan(&live); err != nil {
			return err
		}
		if live >= 5 {
			return apiErr{"too_many_challenges"}
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO challenges (id, faculty_id, nonce, expires_at) VALUES (?,?,?,?)`,
			id, req.FacultyID, nonce, nowU+60)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	return 201, map[string]any{"challenge_id": id, "nonce": nonce, "expires_in": 60}, nil
}

// ---- POST /attendance ----

func (s *Server) attendance(w http.ResponseWriter, r *http.Request) (int, any, error) {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		FacultyID   string `json:"faculty_id"`
		LatMicro    int64  `json:"lat_micro"`
		LonMicro    int64  `json:"lon_micro"`
		AccuracyCm  int64  `json:"accuracy_cm"`
		Signature   string `json:"signature"`
	}
	if err := decode(w, r, &req); err != nil {
		return 0, nil, err
	}
	cfg := s.config() // one snapshot for the whole request
	audit := auditFrom(r.Context())
	audit.FacultyID = req.FacultyID
	audit.ChallengeID = req.ChallengeID
	audit.Policy = snapPolicy(cfg)
	audit.LatMicro = &req.LatMicro
	audit.LonMicro = &req.LonMicro
	audit.AccuracyCm = &req.AccuracyCm
	// 1. field ranges
	if req.ChallengeID == "" || req.FacultyID == "" || req.Signature == "" ||
		req.LatMicro < -90_000_000 || req.LatMicro > 90_000_000 ||
		req.LonMicro < -180_000_000 || req.LonMicro > 180_000_000 ||
		req.AccuracyCm < 0 || req.AccuracyCm > 1_000_000_000 {
		return 0, nil, apiErr{"bad_request"}
	}
	now := s.now()
	nowU := now.Unix()
	ctx := r.Context()
	dist := haversineM(float64(req.LatMicro)/1e6, float64(req.LonMicro)/1e6, cfg.GeoLat, cfg.GeoLon)
	audit.DistanceM = new(int64)
	*audit.DistanceM = int64(math.Round(dist))
	// 2-3. challenge exists, is owned, unused, unexpired; faculty has a key
	nonce, pub, err := checkChallenge(ctx, s.db, req.ChallengeID, req.FacultyID, nowU)
	if err != nil {
		return 0, nil, err
	}
	// 4. signature over the string rebuilt from the stored nonce
	if !verifySig(pub, attendMsg(req.ChallengeID, nonce, req.FacultyID, req.LatMicro, req.LonMicro, req.AccuracyCm), req.Signature) {
		return 0, nil, apiErr{"bad_signature"}
	}
	// 5. policy (nothing has changed yet, so the phone may retry on this challenge)
	if !windowOpenAt(now, cfg) {
		return 0, nil, apiErr{"closed"}
	}
	if req.AccuracyCm > int64(cfg.MaxAccM*100) {
		return 0, nil, apiErr{"poor_accuracy"}
	}
	if dist > cfg.RadiusM {
		return 0, nil, apiErr{"outside_area"}
	}
	// 6. one transaction: consume the challenge and write the record
	day := now.In(ist).Format("2006-01-02")
	distM := int64(math.Round(dist))
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := consumeChallenge(ctx, tx, req.ChallengeID, nowU); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO attendance
			(faculty_id, day, marked_at, lat_micro, lon_micro, accuracy_cm, distance_m)
			VALUES (?,?,?,?,?,?,?) ON CONFLICT(faculty_id, day) DO NOTHING`,
			req.FacultyID, day, nowU, req.LatMicro, req.LonMicro, req.AccuracyCm, distM)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return apiErr{"already_marked"} // rolls back, challenge stays unused
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return 201, map[string]any{"day": day, "distance_m": distM}, nil
}

// ---- POST /history ----

func (s *Server) history(w http.ResponseWriter, r *http.Request) (int, any, error) {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		FacultyID   string `json:"faculty_id"`
		Signature   string `json:"signature"`
	}
	if err := decode(w, r, &req); err != nil {
		return 0, nil, err
	}
	auditFrom(r.Context()).FacultyID = req.FacultyID
	auditFrom(r.Context()).ChallengeID = req.ChallengeID
	if req.ChallengeID == "" || req.FacultyID == "" || req.Signature == "" {
		return 0, nil, apiErr{"bad_request"}
	}
	now := s.now()
	nowU := now.Unix()
	ctx := r.Context()
	todayT := now.In(ist)
	today := todayT.Format("2006-01-02")
	from := twoMonthsBack(todayT).Format("2006-01-02")

	var name string
	days := []string{}
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		nonce, pub, err := checkChallenge(ctx, tx, req.ChallengeID, req.FacultyID, nowU)
		if err != nil {
			return err
		}
		if !verifySig(pub, historyMsg(req.ChallengeID, nonce, req.FacultyID), req.Signature) {
			return apiErr{"bad_signature"}
		}
		if err := consumeChallenge(ctx, tx, req.ChallengeID, nowU); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT name FROM faculty WHERE id=?`, req.FacultyID).Scan(&name); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT day FROM attendance
			WHERE faculty_id=? AND day>=? AND day<=? ORDER BY day DESC`, req.FacultyID, from, today)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				return err
			}
			days = append(days, d)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, nil, err
	}
	markedToday := len(days) > 0 && days[0] == today
	return 200, map[string]any{
		"name":         name,
		"today":        today,
		"marked_today": markedToday,
		"window_open":  s.windowOpen(now),
		"days":         days,
	}, nil
}
