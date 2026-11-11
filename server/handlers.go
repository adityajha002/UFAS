package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Campus time is fixed UTC+5:30 (India has no daylight saving).
var ist = time.FixedZone("IST", 5*3600+1800)

type AttendanceWindow struct {
	ID    string `yaml:"id"`
	Start int    `yaml:"-"`
	End   int    `yaml:"-"`
}

type AttendanceWindows struct {
	IN  []AttendanceWindow `yaml:"in_windows"`
	OUT []AttendanceWindow `yaml:"out_windows"`
}

type Config struct {
	DBFile, Listen                   string
	GeoLat, GeoLon                   float64
	RadiusM, MaxAccM                 float64
	WinStart, WinEnd                 int // deprecated legacy single-window fallback
	AttendanceWindows                AttendanceWindows
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
	"request_pending":     {409, "an early-exit request is already pending"},
	"in_window_open":      {409, "the IN window is open now; use MARK IN instead"},
	"out_window_open":     {409, "the OUT window is open now; use MARK OUT instead"},
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

var adminServerUpstreamURL = ""

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, ngrok-skip-browser-warning")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Retry-After, WWW-Authenticate")
	w.Header().Set("Access-Control-Max-Age", "600")
}

func (s *Server) corsOptions(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func adminUpstreamCandidates() []string {
	if u := strings.TrimSpace(adminServerUpstreamURL); u != "" {
		return []string{strings.TrimRight(u, "/")}
	}
	var out []string
	if env := strings.TrimSpace(os.Getenv("ADMIN_UPSTREAM_URL")); env != "" {
		out = append(out, strings.TrimRight(env, "/"))
	}
	out = append(out, "http://127.0.0.1:9090", "http://admin-server:9090")
	return out
}

func (s *Server) proxyAdminAPI(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_request", "message": "malformed request"})
		return
	}
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for _, base := range adminUpstreamCandidates() {
		targetURL := base + r.URL.RequestURI()
		req, reqErr := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			lastErr = reqErr
			continue
		}
		for k, vals := range r.Header {
			if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Connection") {
				continue
			}
			for _, v := range vals {
				req.Header.Add(k, v)
			}
		}
		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		defer resp.Body.Close()
		for k, vals := range resp.Header {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				continue
			}
			w.Header().Del(k)
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		setCORSHeaders(w)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}
	log.Printf("admin proxy error %s %s: %v", r.Method, r.URL.Path, lastErr)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "admin_upstream_unavailable",
		"message": "admin server is not reachable on port 9090",
	})
}

func (s *Server) shutdownServer(w http.ResponseWriter, r *http.Request) (int, any, error) {
	if r.Method == http.MethodOptions {
		s.corsOptions(w, r)
		return http.StatusNoContent, nil, nil
	}
	setCORSHeaders(w)
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
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /register", s.wrap("register", s.register))
	mux.HandleFunc("POST /challenge", s.wrap("challenge", s.challenge))
	mux.HandleFunc("POST /attendance", s.wrap("attendance", s.attendance))
	mux.HandleFunc("POST /attendance/request-out", s.wrap("attendance_request_out", s.requestOut))
	mux.HandleFunc("POST /history", s.wrap("history", s.history))
	mux.HandleFunc("POST /server/shutdown", s.wrap("server_shutdown", s.shutdownServer))
	mux.HandleFunc("POST /admin/api/server/shutdown", s.wrap("server_shutdown", s.shutdownServer))
	mux.HandleFunc("OPTIONS /server/shutdown", s.corsOptions)
	mux.HandleFunc("OPTIONS /admin/api/server/shutdown", s.corsOptions)
	mux.HandleFunc("/admin/api/", s.proxyAdminAPI)
	mux.HandleFunc("GET /assets/", s.proxyAdminAPI)
	mux.HandleFunc("GET /{$}", s.proxyAdminAPI)
	return mux
}

func (s *Server) adminRoutes() http.Handler {
	mux := http.NewServeMux()
	return mux
}

// ---- policy ----

func legacyWindowOpenAt(now time.Time, cfg Config) bool {
	if cfg.WinStart == 0 && cfg.WinEnd == 24*60 {
		return true
	}
	if cfg.WinEnd == 0 {
		return false
	}
	t := now.In(ist)
	m := t.Hour()*60 + t.Minute()
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	if cfg.WinEnd == 24*60 {
		return m >= cfg.WinStart
	}
	return m >= cfg.WinStart && m < cfg.WinEnd
}

func isWindowOpenAtMinute(minute int, w AttendanceWindow) bool {
	if w.Start == 0 && w.End == 24*60 {
		return true
	}
	if w.Start < 0 || w.End <= w.Start || w.Start >= 24*60 || w.End > 24*60 {
		return false
	}
	return w.Start <= minute && minute < w.End
}

func actionWindowOpenAt(now time.Time, cfg Config, action string) bool {
	windows := cfg.AttendanceWindows.IN
	if action == "out" {
		windows = cfg.AttendanceWindows.OUT
	}
	if len(windows) > 0 {
		local := now.In(ist)
		minute := local.Hour()*60 + local.Minute()
		for _, w := range windows {
			if isWindowOpenAtMinute(minute, w) {
				return true
			}
		}
		return false
	}
	return legacyWindowOpenAt(now, cfg)
}

func actionWindowAt(now time.Time, cfg Config, action string) (AttendanceWindow, bool) {
	windows := cfg.AttendanceWindows.IN
	if action == "out" {
		windows = cfg.AttendanceWindows.OUT
	}
	if len(windows) == 0 {
		return AttendanceWindow{}, false
	}
	local := now.In(ist)
	minute := local.Hour()*60 + local.Minute()
	for _, w := range windows {
		if isWindowOpenAtMinute(minute, w) {
			return w, true
		}
	}
	return AttendanceWindow{}, false
}

func windowOpenAt(now time.Time, cfg Config) bool {
	if len(cfg.AttendanceWindows.IN) > 0 || len(cfg.AttendanceWindows.OUT) > 0 {
		return actionWindowOpenAt(now, cfg, "in") || actionWindowOpenAt(now, cfg, "out")
	}
	return legacyWindowOpenAt(now, cfg)
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

func encodeWindow(day time.Time, w AttendanceWindow) map[string]any {
	startT := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()).Add(time.Duration(w.Start) * time.Minute)
	endT := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()).Add(time.Duration(w.End) * time.Minute)
	return map[string]any{"id": w.ID, "start": startT.Format("15:04"), "end": endT.Format("15:04")}
}

func windowStatus(now time.Time, cfg Config, action string) map[string]any {
	windows := cfg.AttendanceWindows.IN
	if action == "out" {
		windows = cfg.AttendanceWindows.OUT
	}
	day := now.In(ist)
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	minute := day.Hour()*60 + day.Minute()
	out := map[string]any{"open": false, "active": nil, "next": nil}
	if len(windows) == 0 {
		out["open"] = actionWindowOpenAt(now, cfg, action)
		return out
	}
	for _, w := range windows {
		if isWindowOpenAtMinute(minute, w) {
			out["open"] = true
			out["active"] = encodeWindow(dayStart, w)
			return out
		}
	}
	for _, w := range windows {
		if w.Start > minute {
			out["next"] = encodeWindow(dayStart, w)
			return out
		}
	}
	return out
}

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
	if req.Action != "" && req.Action != "in" && req.Action != "out" {
		return 0, nil, apiErr{"bad_request"}
	}
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
	nonce, pub, err := checkChallenge(ctx, s.db, req.ChallengeID, req.FacultyID, nowU)
	if err != nil {
		return 0, nil, err
	}
	if req.Action != "" {
		if !verifySig(pub, attendMsgV2(req.Action, req.ChallengeID, nonce, req.FacultyID, req.LatMicro, req.LonMicro, req.AccuracyCm), req.Signature) {
			return 0, nil, apiErr{"bad_signature"}
		}
		if !actionWindowOpenAt(now, cfg, req.Action) {
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
		err = withTx(ctx, s.db, func(tx *sql.Tx) error {
			if err := consumeChallenge(ctx, tx, req.ChallengeID, nowU); err != nil {
				return err
			}
			if req.Action == "in" {
				var inState int
				var previousWorked int64
				err = tx.QueryRowContext(ctx, `SELECT is_in, today_worked_s FROM attendance_state WHERE faculty_id=? AND day=?`, req.FacultyID, day).Scan(&inState, &previousWorked)
				switch {
				case errors.Is(err, sql.ErrNoRows):
					_, err = tx.ExecContext(ctx, `INSERT INTO attendance_state (faculty_id, day, is_in, started_at, today_worked_s, updated_at) VALUES (?, ?, 1, ?, 0, ?)`, req.FacultyID, day, nowU, nowU)
					if err != nil {
						return err
					}
					stateIsIn = true
					todayWorkedS = 0
				case err != nil:
					return err
				case inState == 1:
					return apiErr{"already_in"}
				default:
					_, err = tx.ExecContext(ctx, `UPDATE attendance_state SET is_in=1, started_at=?, today_worked_s=?, updated_at=? WHERE faculty_id=? AND day=?`, nowU, previousWorked, nowU, req.FacultyID, day)
					if err != nil {
						return err
					}
					stateIsIn = true
					todayWorkedS = previousWorked
				}
			} else {
				var inState int
				var startedAt sql.NullInt64
				var previousWorked int64
				err = tx.QueryRowContext(ctx, `SELECT is_in, started_at, today_worked_s FROM attendance_state WHERE faculty_id=? AND day=?`, req.FacultyID, day).Scan(&inState, &startedAt, &previousWorked)
				switch {
				case errors.Is(err, sql.ErrNoRows):
					return apiErr{"not_in"}
				case err != nil:
					return err
				case inState == 0:
					return apiErr{"not_in"}
				case !startedAt.Valid:
					return apiErr{"not_in"}
				default:
					elapsed := nowU - startedAt.Int64
					newWorked := previousWorked + elapsed
					_, err = tx.ExecContext(ctx, `UPDATE attendance_state SET is_in=0, started_at=NULL, today_worked_s=?, updated_at=? WHERE faculty_id=? AND day=?`, newWorked, nowU, req.FacultyID, day)
					if err != nil {
						return err
					}
					stateIsIn = false
					todayWorkedS = newWorked
				}
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO attendance (faculty_id, day, marked_at, lat_micro, lon_micro, accuracy_cm, distance_m)
				VALUES (?,?,?,?,?,?,?)
				ON CONFLICT(faculty_id, day) DO UPDATE SET marked_at=excluded.marked_at, lat_micro=excluded.lat_micro, lon_micro=excluded.lon_micro, accuracy_cm=excluded.accuracy_cm, distance_m=excluded.distance_m`,
				req.FacultyID, day, nowU, req.LatMicro, req.LonMicro, req.AccuracyCm, distM)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return 0, nil, err
		}
		return 201, map[string]any{"action": req.Action, "day": day, "at_ist": now.In(ist).Format(time.RFC3339), "distance_m": distM, "is_in": stateIsIn, "today_worked_s": todayWorkedS}, nil
	}
	if !verifySig(pub, attendMsg(req.ChallengeID, nonce, req.FacultyID, req.LatMicro, req.LonMicro, req.AccuracyCm), req.Signature) {
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
			return apiErr{"already_marked"}
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return 201, map[string]any{"day": day, "distance_m": distM}, nil
}

// ---- POST /attendance/request-out ----

func (s *Server) requestOut(w http.ResponseWriter, r *http.Request) (int, any, error) {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		FacultyID   string `json:"faculty_id"`
		Action      string `json:"action"`
		LatMicro    int64  `json:"lat_micro"`
		LonMicro    int64  `json:"lon_micro"`
		AccuracyCm  int64  `json:"accuracy_cm"`
		Signature   string `json:"signature"`
	}
	if err := decode(w, r, &req); err != nil {
		return 0, nil, err
	}
	requestType := strings.ToLower(req.Action)
	if requestType == "" {
		requestType = "out"
	}
	if requestType != "in" && requestType != "out" {
		return 0, nil, apiErr{"bad_request"}
	}
	audit := auditFrom(r.Context())
	audit.FacultyID = req.FacultyID
	audit.ChallengeID = req.ChallengeID
	audit.LatMicro = &req.LatMicro
	audit.LonMicro = &req.LonMicro
	audit.AccuracyCm = &req.AccuracyCm
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
	if req.AccuracyCm > int64(cfg.MaxAccM*100) {
		return 0, nil, apiErr{"poor_accuracy"}
	}
	if dist > cfg.RadiusM {
		return 0, nil, apiErr{"outside_area"}
	}
	nonce, pub, err := checkChallenge(ctx, s.db, req.ChallengeID, req.FacultyID, nowU)
	if err != nil {
		return 0, nil, err
	}
	challengeAction := "request_out"
	if requestType == "in" {
		challengeAction = "request_in"
	}
	if !verifySig(pub, attendMsgV2(challengeAction, req.ChallengeID, nonce, req.FacultyID, req.LatMicro, req.LonMicro, req.AccuracyCm), req.Signature) {
		return 0, nil, apiErr{"bad_signature"}
	}
	if actionWindowOpenAt(now, cfg, requestType) {
		if requestType == "in" {
			return 0, nil, apiErr{"in_window_open"}
		}
		return 0, nil, apiErr{"out_window_open"}
	}
	day := now.In(ist).Format("2006-01-02")
	var inState int
	var requestID string
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		var pendingID string
		err = tx.QueryRowContext(ctx, `SELECT id FROM exit_requests WHERE faculty_id=? AND day=? AND status='pending' LIMIT 1`, req.FacultyID, day).Scan(&pendingID)
		switch {
		case err == nil:
			return apiErr{"request_pending"}
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if requestType == "in" {
			err = tx.QueryRowContext(ctx, `SELECT is_in FROM attendance_state WHERE faculty_id=? AND day=?`, req.FacultyID, day).Scan(&inState)
			switch {
			case err == nil && inState != 0:
				return apiErr{"already_in"}
			case err != nil && !errors.Is(err, sql.ErrNoRows):
				return err
			}
		} else {
			if err := tx.QueryRowContext(ctx, `SELECT is_in FROM attendance_state WHERE faculty_id=? AND day=?`, req.FacultyID, day).Scan(&inState); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return apiErr{"not_in"}
				}
				return err
			}
			if inState == 0 {
				return apiErr{"not_in"}
			}
		}
		if err := consumeChallenge(ctx, tx, req.ChallengeID, nowU); err != nil {
			return err
		}
		requestID = randHex(16)
		_, err = tx.ExecContext(ctx, `INSERT INTO exit_requests (id, faculty_id, day, requested_at, lat_micro, lon_micro, accuracy_cm, distance_m, challenge_id, request_type, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`, requestID, req.FacultyID, day, nowU, req.LatMicro, req.LonMicro, req.AccuracyCm, int64(math.Round(dist)), req.ChallengeID, requestType)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return 201, map[string]any{
		"id":               requestID,
		"faculty_id":       req.FacultyID,
		"day":              day,
		"request_type":     requestType,
		"requested_at_ist": now.In(ist).Format(time.RFC3339),
		"status":           "pending",
	}, nil
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
	cfg := s.config()

	var name string
	days := []string{}
	var isIn bool
	var openSince string
	var todayWorkedS int64
	var inWindowOpen bool
	var outWindowOpen bool
	var nextAction string
	var pendingReq map[string]any
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
		rows, err := tx.QueryContext(ctx, `SELECT day FROM attendance WHERE faculty_id=? AND day>=? AND day<=? ORDER BY day DESC`, req.FacultyID, from, today)
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
				todayWorkedS = worked + nowU - startedAt.Int64
			} else {
				todayWorkedS = worked
			}
		case errors.Is(err, sql.ErrNoRows):
			isIn = false
			openSince = ""
		default:
			return err
		}
		var pendingID, pendingStatus, pendingType string
		var pendingAt int64
		err = tx.QueryRowContext(ctx, `SELECT id, requested_at, status, request_type FROM exit_requests WHERE faculty_id=? AND day=? AND status='pending' ORDER BY requested_at DESC LIMIT 1`, req.FacultyID, today).Scan(&pendingID, &pendingAt, &pendingStatus, &pendingType)
		switch {
		case err == nil:
			pendingReq = map[string]any{
				"id":               pendingID,
				"requested_at_ist": time.Unix(pendingAt, 0).In(ist).Format(time.RFC3339),
				"request_type":     pendingType,
				"status":           pendingStatus,
			}
		case errors.Is(err, sql.ErrNoRows):
			pendingReq = nil
		default:
			return err
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	inWindowOpen = actionWindowOpenAt(now, cfg, "in")
	outWindowOpen = actionWindowOpenAt(now, cfg, "out")
	nextAction = "in"
	if isIn {
		nextAction = "out"
	}
	windowOpen := actionWindowOpenAt(now, cfg, nextAction)
	activeWindow := map[string]any(nil)
	if w, ok := actionWindowAt(now, cfg, nextAction); ok {
		activeWindow = map[string]any{"id": w.ID, "start": time.Date(todayT.Year(), todayT.Month(), todayT.Day(), 0, 0, 0, 0, todayT.Location()).Add(time.Duration(w.Start) * time.Minute).Format("15:04"), "end": time.Date(todayT.Year(), todayT.Month(), todayT.Day(), 0, 0, 0, 0, todayT.Location()).Add(time.Duration(w.End) * time.Minute).Format("15:04")}
	}
	markedToday := len(days) > 0 && days[0] == today
	return 200, map[string]any{
		"name":                 name,
		"today":                today,
		"marked_today":         markedToday,
		"window_open":          windowOpen,
		"in_window_open":       inWindowOpen,
		"out_window_open":      outWindowOpen,
		"next_action":          nextAction,
		"active_window":        activeWindow,
		"in_window":            windowStatus(now, cfg, "in"),
		"out_window":           windowStatus(now, cfg, "out"),
		"pending_exit_request": pendingReq,
		"is_in":                isIn,
		"open_since_ist":       openSince,
		"today_worked_s":       todayWorkedS,
		"days":                 days,
	}, nil
}
