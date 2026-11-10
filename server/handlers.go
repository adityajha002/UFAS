package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Campus time is fixed UTC+5:30 (India has no daylight saving).
var ist = time.FixedZone("IST", 5*3600+1800)

type Config struct {
	DBFile, Listen                   string
	GeoLat, GeoLon                   float64
	RadiusM, MaxAccM                 float64
	WinStart, WinEnd                 int // minutes after midnight, campus time
	AdminListen, AdminTokenSHA256    string
	AuditRetentionDays, AuditMaxRows int
}

type Server struct {
	db           *sql.DB
	cfg          atomic.Pointer[Config]
	now          func() time.Time // injectable clock; the server clock decides everything
	shutdownReq  chan struct{}
	shutdownOnce sync.Once
}

func newServer(db *sql.DB, cfg Config, now func() time.Time) *Server {
	s := &Server{db: db, now: now, shutdownReq: make(chan struct{})}
	s.setConfig(cfg)
	return s
}

func (s *Server) requestShutdown() {
	if s == nil {
		return
	}
	s.shutdownOnce.Do(func() { close(s.shutdownReq) })
}

func (s *Server) config() Config {
	if s == nil || s.cfg.Load() == nil {
		return Config{}
	}
	return *s.cfg.Load()
}

func (s *Server) setConfig(c Config) { s.cfg.Store(&c) }

type policySnap struct {
	Open   bool
	Reason string
	Window string
	At     time.Time
	Next   time.Time
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
	"already_in":          {409, "already in"},
	"not_in":              {409, "not in"},
	"too_many_challenges": {429, "too many open challenges"},
}

type handler func(w http.ResponseWriter, r *http.Request) (int, any, error)

func (s *Server) wrap(endpoint string, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &auditRec{}
		r = r.WithContext(context.WithValue(r.Context(), auditCtxKey{}, rec))
		start := time.Now()
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
		if endpoint != "" {
			if err := s.writeAudit(r, endpoint, start, status, outcome, rec); err != nil {
				log.Printf("audit %s failed: %v", endpoint, err)
			}
		}
	}
}

func (s *Server) writeAudit(r *http.Request, endpoint string, start time.Time, status int, outcome string, rec *auditRec) error {
	if s.db == nil {
		return nil
	}
	if rec == nil {
		rec = &auditRec{}
	}
	remoteIP := r.RemoteAddr
	if i := strings.IndexByte(remoteIP, ':'); i >= 0 {
		remoteIP = remoteIP[:i]
	}
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	var lat, lon, acc, dist *int64
	if rec.LatMicro != nil {
		lat = rec.LatMicro
	}
	if rec.LonMicro != nil {
		lon = rec.LonMicro
	}
	if rec.AccuracyCm != nil {
		acc = rec.AccuracyCm
	}
	if rec.DistanceM != nil {
		dist = rec.DistanceM
	}
	_, err := s.db.Exec(`INSERT INTO audit_log
		(ts, endpoint, method, path, status, outcome, faculty_id, challenge_id,
		 remote_ip, user_agent, lat_micro, lon_micro, accuracy_cm, distance_m, policy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		start.Unix(), endpoint, r.Method, r.URL.Path, status, outcome,
		rec.FacultyID, rec.ChallengeID, remoteIP, ua,
		lat, lon, acc, dist, valueOfPolicy(rec.Policy))
	return err
}

func valueOfPolicy(p *policySnap) string {
	if p == nil {
		return ""
	}
	if p.Open {
		return "open"
	}
	return p.Reason
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

func (s *Server) corsOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) shutdownServer(w http.ResponseWriter, r *http.Request) (int, any, error) {
	if r.Method == http.MethodOptions {
		s.corsOptions(w, r)
		return http.StatusNoContent, nil, nil
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := decode(w, r, &req); err != nil || req.Confirm != "shutdown" {
		return 0, nil, apiErr{"bad_request"}
	}
	s.requestShutdown()
	return 202, map[string]any{"status": "shutting_down", "at_ist": s.now().In(ist).Format(time.RFC3339)}, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", s.wrap("register", s.register))
	mux.HandleFunc("POST /challenge", s.wrap("challenge", s.challenge))
	mux.HandleFunc("POST /attendance", s.wrap("attendance", s.attendance))
	mux.HandleFunc("POST /history", s.wrap("history", s.history))
	mux.HandleFunc("POST /server/shutdown", s.wrap("server_shutdown", s.shutdownServer))
	mux.HandleFunc("POST /admin/api/server/shutdown", s.wrap("server_shutdown", s.shutdownServer))
	mux.HandleFunc("OPTIONS /server/shutdown", s.corsOptions)
	mux.HandleFunc("OPTIONS /admin/api/server/shutdown", s.corsOptions)
	return mux
}

func (s *Server) adminRoutes() http.Handler {
	mux := http.NewServeMux()
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

func (s *Server) windowState(now time.Time, cfg Config) windowInfo {
	day := now.In(ist)
	m := day.Hour()*60 + day.Minute()
	if cfg.WinStart == 0 && cfg.WinEnd == 24*60 {
		return windowInfo{Open: true, Reason: "open", NextChange: day.Add(24 * time.Hour)}
	}
	if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return windowInfo{Open: false, Reason: "weekend", NextChange: time.Date(day.Year(), day.Month(), day.Day()+1, 9, 0, 0, 0, day.Location())}
	}
	if m < cfg.WinStart {
		start := time.Date(day.Year(), day.Month(), day.Day(), cfg.WinStart/60, cfg.WinStart%60, 0, 0, day.Location())
		return windowInfo{Open: false, Reason: "before_start", NextChange: start}
	}
	if m >= cfg.WinEnd {
		next := time.Date(day.Year(), day.Month(), day.Day()+1, cfg.WinStart/60, cfg.WinStart%60, 0, 0, day.Location())
		return windowInfo{Open: false, Reason: "after_end", NextChange: next}
	}
	return windowInfo{Open: true, Reason: "open", NextChange: time.Date(day.Year(), day.Month(), day.Day(), cfg.WinEnd/60, cfg.WinEnd%60, 0, 0, day.Location())}
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
		Action      string `json:"action"`
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
	audit := auditFrom(r.Context())
	audit.FacultyID = req.FacultyID
	audit.ChallengeID = req.ChallengeID
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
	cfg := s.config()
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
	if req.Action != "" {
		if req.Action != "in" && req.Action != "out" {
			return 0, nil, apiErr{"bad_request"}
		}
		if !verifySig(pub, attendMsgV2(req.Action, req.ChallengeID, nonce, req.FacultyID, req.LatMicro, req.LonMicro, req.AccuracyCm), req.Signature) {
			return 0, nil, apiErr{"bad_signature"}
		}
		if !windowOpenAt(now, cfg) {
			return 0, nil, apiErr{"closed"}
		}
		if req.AccuracyCm > int64(cfg.MaxAccM*100) {
			return 0, nil, apiErr{"poor_accuracy"}
		}
		if dist > cfg.RadiusM {
			return 0, nil, apiErr{"outside_area"}
		}
		day := now.In(ist).Format("2006-01-02")
		distM := int64(math.Round(dist))
		var stateIsIn bool
		var todayWorkedS int64
		var startedAt sql.NullInt64
		err = withTx(ctx, s.db, func(tx *sql.Tx) error {
			if err := consumeChallenge(ctx, tx, req.ChallengeID, nowU); err != nil {
				return err
			}
			if req.Action == "in" {
				var inState int
				if err := tx.QueryRowContext(ctx, `SELECT is_in FROM attendance_state WHERE faculty_id=? AND day=?`, req.FacultyID, day).Scan(&inState); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						_, err = tx.ExecContext(ctx, `INSERT INTO attendance_state (faculty_id, day, is_in, started_at, updated_at) VALUES (?, ?, 1, ?, ?)`, req.FacultyID, day, nowU, nowU)
						if err != nil {
							return err
						}
						_, err = tx.ExecContext(ctx, `INSERT INTO attendance (faculty_id, day, marked_at, lat_micro, lon_micro, accuracy_cm, distance_m)
							VALUES (?,?,?,?,?,?,?)
							ON CONFLICT(faculty_id, day) DO UPDATE SET marked_at=excluded.marked_at, lat_micro=excluded.lat_micro, lon_micro=excluded.lon_micro, accuracy_cm=excluded.accuracy_cm, distance_m=excluded.distance_m`,
							req.FacultyID, day, nowU, req.LatMicro, req.LonMicro, req.AccuracyCm, distM)
						if err != nil {
							return err
						}
						stateIsIn = true
						return nil
					}
					return err
				}
				if inState == 1 {
					return apiErr{"already_in"}
				}
				_, err = tx.ExecContext(ctx, `UPDATE attendance_state SET is_in=1, started_at=?, updated_at=? WHERE faculty_id=? AND day=?`, nowU, nowU, req.FacultyID, day)
				if err != nil {
					return err
				}
				_, err = tx.ExecContext(ctx, `INSERT INTO attendance (faculty_id, day, marked_at, lat_micro, lon_micro, accuracy_cm, distance_m)
					VALUES (?,?,?,?,?,?,?)
					ON CONFLICT(faculty_id, day) DO UPDATE SET marked_at=excluded.marked_at, lat_micro=excluded.lat_micro, lon_micro=excluded.lon_micro, accuracy_cm=excluded.accuracy_cm, distance_m=excluded.distance_m`,
					req.FacultyID, day, nowU, req.LatMicro, req.LonMicro, req.AccuracyCm, distM)
				if err != nil {
					return err
				}
				stateIsIn = true
				return nil
			}
			if err := tx.QueryRowContext(ctx, `SELECT is_in, started_at FROM attendance_state WHERE faculty_id=? AND day=?`, req.FacultyID, day).Scan(&stateIsIn, &startedAt); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return apiErr{"not_in"}
				}
				return err
			}
			if !stateIsIn {
				return apiErr{"not_in"}
			}
			todayWorkedS = nowU - startedAt.Int64
			_, err = tx.ExecContext(ctx, `UPDATE attendance_state SET is_in=0, started_at=NULL, today_worked_s=?, updated_at=? WHERE faculty_id=? AND day=?`, todayWorkedS, nowU, req.FacultyID, day)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO attendance (faculty_id, day, marked_at, lat_micro, lon_micro, accuracy_cm, distance_m)
				VALUES (?,?,?,?,?,?,?)
				ON CONFLICT(faculty_id, day) DO UPDATE SET marked_at=excluded.marked_at, lat_micro=excluded.lat_micro, lon_micro=excluded.lon_micro, accuracy_cm=excluded.accuracy_cm, distance_m=excluded.distance_m`,
				req.FacultyID, day, nowU, req.LatMicro, req.LonMicro, req.AccuracyCm, distM)
			if err != nil {
				return err
			}
			stateIsIn = false
			return nil
		})
		if err != nil {
			return 0, nil, err
		}
		return 201, map[string]any{"action": req.Action, "day": day, "at_ist": now.In(ist).Format(time.RFC3339), "distance_m": distM, "is_in": stateIsIn, "today_worked_s": todayWorkedS}, nil
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
	var isIn bool
	var openSince string
	var todayWorkedS int64
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
		if err := rows.Err(); err != nil {
			return err
		}
		var startedAt sql.NullInt64
		var worked int64
		err = tx.QueryRowContext(ctx, `SELECT is_in, started_at, today_worked_s FROM attendance_state WHERE faculty_id=? AND day=?`, req.FacultyID, today).Scan(&isIn, &startedAt, &worked)
		switch {
		case err == nil:
			if isIn && startedAt.Valid {
				openSince = time.Unix(startedAt.Int64, 0).In(ist).Format(time.RFC3339)
				todayWorkedS = nowU - startedAt.Int64
			} else {
				todayWorkedS = worked
			}
		case errors.Is(err, sql.ErrNoRows):
			isIn = false
			openSince = ""
			dayWorked := 0
			_ = dayWorked
		default:
			return err
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	markedToday := len(days) > 0 && days[0] == today
	return 200, map[string]any{
		"name":           name,
		"today":          today,
		"marked_today":   markedToday,
		"window_open":    s.windowOpen(now),
		"days":           days,
		"is_in":          isIn,
		"open_since_ist": openSince,
		"today_worked_s": todayWorkedS,
	}, nil
}
