package main

// Admin control panel: second listener, separate mux, bearer-token auth.
// Admin routes are never registered on the phone-facing mux (routes()).

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	istLayout    = "2006-01-02 15:04:05"
	dayLayout    = "2006-01-02"
	maxRangeDays = 366
)

var mainServerShutdownURL = "http://127.0.0.1:8080"

func fmtIST(unix int64) string { return time.Unix(unix, 0).In(ist).Format(istLayout) }

// ---- errors ----

// adminErr is an admin-panel error. msg overrides the errTable message and
// fields carries per-field problems (invalid_config).
type adminErr struct {
	code   string
	msg    string
	fields map[string]string
}

func (e adminErr) Error() string { return e.code }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeAdminErr(w http.ResponseWriter, r *http.Request, err error) {
	var ae adminErr
	var pe apiErr
	switch {
	case errors.As(err, &ae):
		e := errTable[ae.code]
		msg := e.msg
		if ae.msg != "" {
			msg = ae.msg
		}
		body := map[string]any{"error": ae.code, "message": msg}
		if ae.fields != nil {
			body["fields"] = ae.fields
		}
		writeJSON(w, e.status, body)
	case errors.As(err, &pe):
		e := errTable[pe.code]
		writeJSON(w, e.status, map[string]string{"error": pe.code, "message": e.msg})
	default:
		log.Printf("admin internal error %s %s: %v", r.Method, r.URL.Path, err)
		writeJSON(w, 500, map[string]string{"error": "internal", "message": "internal error"})
	}
}

// adminJSON adapts a handler. Handlers return their whole body, so no database
// cursor is ever held while the response is written (see D6).
func (s *Server) adminJSON(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, body, err := h(w, r)
		if err != nil {
			writeAdminErr(w, r, err)
			return
		}
		writeJSON(w, status, body)
	}
}

// adminRaw is for non-JSON responses (CSV). The handler must not write anything
// before it returns a nil error.
func (s *Server) adminRaw(h func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeAdminErr(w, r, err)
		}
	}
}

// ---- routes ----

func (s *Server) adminRoutes() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /admin/api/status", s.adminJSON(s.adminStatus))
	api.HandleFunc("GET /admin/api/attendance/day", s.adminJSON(s.adminDay))
	api.HandleFunc("POST /admin/api/attendance/manual", s.adminJSON(s.adminManualMark))
	api.HandleFunc("GET /admin/api/exit-requests", s.adminJSON(s.adminExitRequestList))
	api.HandleFunc("GET /admin/api/exit-requests/{request_id}", s.adminJSON(s.adminExitRequestOne))
	api.HandleFunc("POST /admin/api/exit-requests/{request_id}/approve", s.adminJSON(s.adminExitRequestApprove))
	api.HandleFunc("POST /admin/api/exit-requests/{request_id}/reject", s.adminJSON(s.adminExitRequestReject))
	api.HandleFunc("GET /admin/api/faculty", s.adminJSON(s.adminFacultyList))
	api.HandleFunc("POST /admin/api/faculty", s.adminJSON(s.adminFacultyAdd))
	api.HandleFunc("GET /admin/api/faculty/{employee_id}/attendance", s.adminJSON(s.adminFacultyAttendance))
	api.HandleFunc("POST /admin/api/faculty/{employee_id}/code", s.adminJSON(s.adminCodeIssue))
	api.HandleFunc("POST /admin/api/faculty/{employee_id}/revoke", s.adminJSON(s.adminFacultyRevoke))
	api.HandleFunc("DELETE /admin/api/faculty/{employee_id}", s.adminJSON(s.adminFacultyDelete))
	api.HandleFunc("GET /admin/api/config", s.adminJSON(s.adminConfigGet))
	api.HandleFunc("PATCH /admin/api/config", s.adminJSON(s.adminConfigPatch))
	api.HandleFunc("DELETE /admin/api/config/overrides", s.adminJSON(s.adminConfigReset))
	api.HandleFunc("GET /admin/api/config/history", s.adminJSON(s.adminConfigHistory))
	api.HandleFunc("GET /admin/api/audit", s.adminJSON(s.adminAuditList))
	api.HandleFunc("GET /admin/api/audit/{id}", s.adminJSON(s.adminAuditOne))
	api.HandleFunc("GET /admin/api/actions", s.adminJSON(s.adminActionsList))
	api.HandleFunc("GET /admin/api/export/attendance.csv", s.adminRaw(s.adminExport))
	api.HandleFunc("POST /admin/api/server/shutdown", s.adminJSON(s.adminShutdown))
	api.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeAdminErr(w, r, adminErr{code: "not_found"})
	})

	root := http.NewServeMux()
	root.Handle("/admin/api/", s.adminMiddleware(api)) // token + throttle: API calls only
	root.Handle("/", uiHandler(uiRoot()))              // static UI: no token
	return root
}

// ---- auth ----

func setAdminCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, ngrok-skip-browser-warning")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Retry-After, WWW-Authenticate")
	w.Header().Set("Access-Control-Max-Age", "600")
}

func (s *Server) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setAdminCORSHeaders(w)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		ip, now := remoteIP(r), s.now()
		if s.throttle.blocked(ip, now) {
			w.Header().Set("Retry-After", "60")
			writeAdminErr(w, r, adminErr{code: "too_many_attempts"})
			return
		}
		if !s.tokenOK(r) {
			s.throttle.fail(ip, now)
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeAdminErr(w, r, adminErr{code: "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenOK compares SHA-256(token) with the configured hash in constant time.
// No configured hash means admin auth is disabled and every request is accepted.
// Only safe while admin.listen is loopback (see the warning in serve()).
func (s *Server) tokenOK(r *http.Request) bool {
	wantHex := s.config().AdminTokenSHA256
	if wantHex == "" {
		return true
	}
	want, err := hex.DecodeString(wantHex)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	h := r.Header.Get("Authorization")
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(h[7:])))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

const (
	authMaxFails = 10
	authWindow   = time.Minute
	authBlock    = time.Minute
)

type throttleEnt struct {
	winStart     time.Time
	fails        int
	blockedUntil time.Time
}

// authThrottle blocks an IP for a minute after 10 failed attempts within a
// minute. In-memory is enough: a restart only forgives, never weakens, anything.
type authThrottle struct {
	mu sync.Mutex
	m  map[string]*throttleEnt
}

func (t *authThrottle) blocked(ip string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.m[ip]
	return e != nil && now.Before(e.blockedUntil)
}

func (t *authThrottle) fail(ip string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]*throttleEnt{}
	}
	if len(t.m) > 4096 { // bound memory against many source addresses
		for k, e := range t.m {
			if !now.Before(e.blockedUntil) && now.Sub(e.winStart) > authWindow {
				delete(t.m, k)
			}
		}
	}
	e := t.m[ip]
	if e == nil {
		e = &throttleEnt{winStart: now}
		t.m[ip] = e
	}
	if now.Sub(e.winStart) > authWindow {
		e.winStart, e.fails = now, 0
	}
	e.fails++
	if e.fails >= authMaxFails {
		e.blockedUntil = now.Add(authBlock)
		e.winStart, e.fails = now, 0
	}
}

// ---- request parsing helpers ----

// parseDay parses YYYY-MM-DD as midnight IST.
func parseDay(s string) (time.Time, error) {
	t, err := time.ParseInLocation(dayLayout, s, ist)
	if err != nil || t.Format(dayLayout) != s {
		return time.Time{}, adminErr{code: "bad_date"}
	}
	return t, nil
}

func todayIST(now time.Time) time.Time {
	n := now.In(ist)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, ist)
}

// checkSpan validates an inclusive from..to range.
func checkSpan(from, to time.Time) error {
	if from.After(to) {
		return adminErr{code: "bad_range", msg: "from must not be after to"}
	}
	if int(to.Sub(from).Hours()/24)+1 > maxRangeDays {
		return adminErr{code: "range_too_large", msg: fmt.Sprintf("range is limited to %d days", maxRangeDays)}
	}
	return nil
}

func intParam(q url.Values, name string, def, min, max int64) (int64, error) {
	v := q.Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < min || n > max {
		return 0, apiErr{"bad_request"}
	}
	return n, nil
}

// ---- address helpers ----

func isWildcardHost(h string) bool {
	return h == "" || h == "0.0.0.0" || h == "::" || h == "[::]"
}

// isLoopbackAddr reports whether a listen address is bound to loopback only.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type netAddr struct {
	Interface string `json:"interface"`
	IP        string `json:"ip"`
}

// listNetAddrs lists IPv4 addresses of interfaces that are up (v1: no IPv6,
// no loopback, no link-local).
func listNetAddrs() []netAddr {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []netAddr
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, netAddr{Interface: ifc.Name, IP: ip4.String()})
		}
	}
	return out
}
