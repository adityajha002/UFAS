package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestPhone(e *env, emp string) *phone {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		e.t.Fatal(err)
	}
	return &phone{e: e, key: key, employee: emp}
}

func actionOutcomes(t *testing.T, ac *adminClient, action string) []map[string]any {
	t.Helper()
	st, out := ac.call("GET", "/admin/api/actions?limit=200&action="+action, "")
	if st != 200 {
		t.Fatalf("actions: %d %v", st, out)
	}
	return auditItems(out)
}

// ---- G2 + G3: write routes and their trail ----

func TestAdminFacultyWriteRoutes(t *testing.T) {
	e, ac := newAdminEnv(t)

	st, out := ac.call("POST", "/admin/api/faculty", `{"employee_id":" E101 ","name":"Dr. A Sharma"}`)
	if st != 201 || out["employee_id"] != "E101" || out["name"] != "Dr. A Sharma" || out["enrolled"] != false {
		t.Fatalf("add: %d %v", st, out)
	}
	ac.wantErr("POST", "/admin/api/faculty", `{"employee_id":"E101","name":"Someone else"}`, 409, "already_exists")
	for _, body := range []string{
		`{"employee_id":"","name":"x"}`,
		`{"employee_id":"a/b","name":"x"}`,
		`{"employee_id":"a\\b","name":"x"}`,
		`{"employee_id":"E2","name":""}`,
		`{"employee_id":"E2","name":"na\u0007me"}`,
		`{"employee_id":"E2","name":"` + strings.Repeat("x", 101) + `"}`,
	} {
		out := ac.wantErr("POST", "/admin/api/faculty", body, 422, "invalid_input")
		if len(asMap(out["fields"])) == 0 {
			t.Fatalf("%s: no per-field messages: %v", body, out)
		}
	}
	ac.wantErr("POST", "/admin/api/faculty", `{"employee_id":"E2","nmae":"x"}`, 400, "bad_request")
	if n := e.dbCount(`SELECT count(*) FROM faculty`); n != 1 {
		t.Fatalf("rejected adds created rows: %d", n)
	}

	// enrollment codes: a new code replaces the previous unused one
	st, out = ac.call("POST", "/admin/api/faculty/E101/code", "")
	code1, _ := out["code"].(string)
	if st != 201 || len(code1) != 20 || out["expires_in_s"] != 900.0 || out["expires_at_ist"] != "2026-10-23 10:15:00" {
		t.Fatalf("code: %d %v", st, out)
	}
	_, out = ac.call("POST", "/admin/api/faculty/E101/code", "")
	code2, _ := out["code"].(string)
	if code2 == "" || code2 == code1 {
		t.Fatalf("second code: %v", out)
	}
	p := newTestPhone(e, "E101")
	if st, _ := e.post("/register", map[string]any{"code": code1, "public_key": p.pubB64()}); st == 201 {
		t.Fatal("the replaced code must not enroll")
	}
	st, out = e.post("/register", map[string]any{"code": code2, "public_key": p.pubB64()})
	if st != 201 {
		t.Fatalf("register with the new code: %d %v", st, out)
	}
	p.facultyID = out["faculty_id"].(string)
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatal("enrolled phone should attend")
	}

	// revoke: the phone is rejected from then on
	st, out = ac.call("POST", "/admin/api/faculty/E101/revoke", "")
	if st != 200 || out["enrolled"] != false {
		t.Fatalf("revoke: %d %v", st, out)
	}
	if st, out := e.post("/challenge", map[string]any{"faculty_id": p.facultyID}); st != 403 || out["error"] != "not_enrolled" {
		t.Fatalf("revoked phone: %d %v", st, out)
	}
	ac.wantErr("POST", "/admin/api/faculty/NOPE/code", "", 404, "not_found")
	ac.wantErr("POST", "/admin/api/faculty/NOPE/revoke", "", 404, "not_found")

	// delete: removes the faculty and all of their attendance/challenge state in one shot
	st, out = ac.call("DELETE", "/admin/api/faculty/E101", "")
	if st != 200 || out["employee_id"] != "E101" || out["deleted"] != true {
		t.Fatalf("delete: %d %v", st, out)
	}
	if n := e.dbCount(`SELECT count(*) FROM faculty WHERE employee_id='E101'`); n != 0 {
		t.Fatalf("deleted faculty still remains: %d", n)
	}
	if n := e.dbCount(`SELECT count(*) FROM attendance WHERE faculty_id = (SELECT id FROM faculty WHERE employee_id='E101')`); n != 0 {
		t.Fatalf("deleted faculty still has attendance rows: %d", n)
	}
	if n := e.dbCount(`SELECT count(*) FROM challenges WHERE faculty_id = (SELECT id FROM faculty WHERE employee_id='E101')`); n != 0 {
		t.Fatalf("deleted faculty still has challenge rows: %d", n)
	}
	ac.wantErr("DELETE", "/admin/api/faculty/NOPE", "", 404, "not_found")

	// the code appears in the response only: not in the trail, not in the audit
	for _, q := range []string{
		`SELECT COALESCE(group_concat(COALESCE(target,'')||'|'||COALESCE(detail,'')||'|'||outcome, ' '), '') FROM admin_actions`,
		`SELECT COALESCE(group_concat(path||'|'||COALESCE(faculty_id,'')||'|'||COALESCE(challenge_id,'')||'|'||COALESCE(policy,''), ' '), '') FROM audit_log`,
	} {
		var dump string
		if err := e.s.db.QueryRow(q).Scan(&dump); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(dump, code1) || strings.Contains(dump, code2) {
			t.Fatal("enrollment code leaked into a table")
		}
	}
	_, _, raw := ac.do("GET", "/admin/api/actions", "")
	if strings.Contains(string(raw), code1) || strings.Contains(string(raw), code2) {
		t.Fatal("enrollment code leaked through /actions")
	}

	// the trail: successes and failures, newest first
	adds := actionOutcomes(t, ac, "faculty_add")
	if len(adds) != 9 || adds[len(adds)-1]["outcome"] != "ok" || adds[len(adds)-1]["target"] != "E101" ||
		asMap(adds[len(adds)-1]["detail"])["name"] != "Dr. A Sharma" || adds[len(adds)-2]["outcome"] != "already_exists" {
		t.Fatalf("faculty_add trail: %v", adds)
	}
	codes := actionOutcomes(t, ac, "code_issue")
	if len(codes) != 3 || codes[0]["outcome"] != "not_found" || codes[0]["detail"] != nil || codes[1]["outcome"] != "ok" {
		t.Fatalf("code_issue trail: %v", codes)
	}
	if rv := actionOutcomes(t, ac, "faculty_revoke"); len(rv) != 2 {
		t.Fatalf("revoke trail: %v", rv)
	}
	ac.wantErr("GET", "/admin/api/actions?action=bogus", "", 400, "bad_request")
	_, out = ac.call("GET", "/admin/api/actions?limit=2", "")
	if len(auditItems(out)) != 2 || out["next_before_id"] == nil {
		t.Fatalf("paging: %v", out)
	}
}

func TestAdminManualMarkRoute(t *testing.T) {
	e, ac := newAdminEnv(t)
	ac.call("POST", "/admin/api/faculty", `{"employee_id":"E101","name":"Dr. A Sharma"}`)

	st, out := ac.call("POST", "/admin/api/attendance/manual", `{"employee_id":"E101","date":"2026-10-22","note":"Phone broken, verified by HOD"}`)
	if st != 201 || out["source"] != "manual" || out["date"] != "2026-10-22" {
		t.Fatalf("manual: %d %v", st, out)
	}
	_, out = ac.call("GET", "/admin/api/attendance/day?date=2026-10-22", "")
	f := asMap(asList(out["faculty"])[0])
	if f["marked"] != true || f["source"] != "manual" || f["note"] != "Phone broken, verified by HOD" {
		t.Fatalf("day view: %v", f)
	}
	// today is allowed, tomorrow is not
	if st, _ := ac.call("POST", "/admin/api/attendance/manual", `{"employee_id":"E101","date":"2026-10-23","note":"today"}`); st != 201 {
		t.Fatal("today must be allowed")
	}
	ac.wantErr("POST", "/admin/api/attendance/manual", `{"employee_id":"E101","date":"2026-10-22","note":"again"}`, 409, "already_marked")
	ac.wantErr("POST", "/admin/api/attendance/manual", `{"employee_id":"E101","date":"2026-10-24","note":"future"}`, 400, "future_date")
	ac.wantErr("POST", "/admin/api/attendance/manual", `{"employee_id":"E101","date":"2026-13-40","note":"x"}`, 400, "bad_date")
	ac.wantErr("POST", "/admin/api/attendance/manual", `{"employee_id":"E101","date":"2026-10-21","note":"  "}`, 422, "invalid_input")
	ac.wantErr("POST", "/admin/api/attendance/manual", `{"employee_id":"E101","note":"x"}`, 422, "invalid_input")
	ac.wantErr("POST", "/admin/api/attendance/manual", `{"employee_id":"NOPE","date":"2026-10-21","note":"x"}`, 404, "not_found")
	if n := e.dbCount(`SELECT count(*) FROM attendance`); n != 2 {
		t.Fatalf("only the two valid marks may exist: %d", n)
	}

	items := actionOutcomes(t, ac, "manual_mark")
	if len(items) != 8 { // 2 ok + already_marked + future_date + bad_date + 2 invalid_input + not_found
		t.Fatalf("trail: %d %v", len(items), items)
	}
	oks := 0
	for _, it := range items {
		if it["outcome"] == "ok" {
			oks++
			if d := asMap(it["detail"]); d["date"] == nil || d["note"] == nil {
				t.Fatalf("ok row needs date and note: %v", it)
			}
		}
	}
	if oks != 2 {
		t.Fatalf("ok rows: %d", oks)
	}
}

func TestConfigChangesLeaveATrail(t *testing.T) {
	e, ac := newAdminEnv(t)
	ac.call("PATCH", "/admin/api/config?dry_run=true", `{"geofence":{"radius_m":200}}`) // no trail
	ac.call("PATCH", "/admin/api/config", `{"geofence":{"radius_m":150}}`)              // no-op: no trail
	if n := e.dbCount(`SELECT count(*) FROM admin_actions`); n != 0 {
		t.Fatalf("dry runs and no-ops must not leave rows: %d", n)
	}
	ac.call("PATCH", "/admin/api/config", `{"geofence":{"radius_m":200}}`)
	ac.wantErr("PATCH", "/admin/api/config", `{"geofence":{"lat":95}}`, 422, "invalid_config")
	ac.call("DELETE", "/admin/api/config/overrides", "")
	ac.call("DELETE", "/admin/api/config/overrides", "") // nothing left: no trail

	_, out := ac.call("GET", "/admin/api/actions", "")
	items := auditItems(out)
	if len(items) != 3 {
		t.Fatalf("want 3 rows, got %d: %v", len(items), items)
	}
	if items[0]["action"] != "config_reset" || items[0]["target"] != "all" || items[0]["outcome"] != "ok" ||
		items[1]["action"] != "config_change" || items[1]["outcome"] != "invalid_config" ||
		asMap(asMap(items[1]["detail"])["fields"])["geofence.lat"] == nil ||
		items[2]["action"] != "config_change" || items[2]["target"] != "geofence.radius_m" ||
		asMap(asMap(items[2]["detail"])["geofence.radius_m"])["new"] != 200.0 {
		t.Fatalf("trail: %v", items)
	}
	if items[0]["remote_ip"] == nil {
		t.Fatal("remote_ip missing")
	}
}

// ---- G1: shutdown ----

func TestShutdownRoute(t *testing.T) {
	e, ac := newAdminEnv(t)
	called := make(chan string, 1)
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/server/shutdown" {
			t.Fatalf("wrong path/method: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		called <- string(body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"shutting_down","at_ist":"2026-10-23 10:00:00"}`))
	}))
	defer main.Close()
	oldURL := mainServerShutdownURL
	mainServerShutdownURL = main.URL
	defer func() { mainServerShutdownURL = oldURL }()

	ac.wantErr("POST", "/admin/api/server/shutdown", ``, 400, "confirm_required")
	ac.wantErr("POST", "/admin/api/server/shutdown", `{"confirm":"yes"}`, 400, "confirm_required")
	select {
	case <-e.s.shutdownReq:
		t.Fatal("admin server shutdown must not be requested")
	default:
	}
	st, out := ac.call("POST", "/admin/api/server/shutdown", `{"confirm":"shutdown"}`)
	if st != 202 || out["status"] != "shutting_down" || out["at_ist"] != "2026-10-23 10:00:00" {
		t.Fatalf("%d %v", st, out)
	}
	select {
	case body := <-called:
		if !strings.Contains(body, `"confirm":"shutdown"`) {
			t.Fatalf("missing confirm payload: %s", body)
		}
	default:
		t.Fatal("main server shutdown was not requested")
	}
	select {
	case <-e.s.shutdownReq:
		t.Fatal("admin panel process must remain running")
	default:
	}
	ac.call("POST", "/admin/api/server/shutdown", `{"confirm":"shutdown"}`) // repeated: must not panic
	items := actionOutcomes(t, ac, "shutdown")
	if len(items) != 4 || items[1]["outcome"] != "ok" || items[3]["outcome"] != "confirm_required" {
		t.Fatalf("trail: %v", items)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func waitDial(t *testing.T, addr string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			c.Close()
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("%s never started listening", addr)
}

// The admin panel must keep running after the shutdown button; it only forwards
// the stop request to the real main server.
func TestServeKeepsAdminPanelRunningAfterShutdownButton(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "attendance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/server/shutdown" {
			t.Fatalf("wrong path/method: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"shutting_down"}`))
	}))
	defer main.Close()
	oldURL := mainServerShutdownURL
	mainServerShutdownURL = main.URL
	defer func() { mainServerShutdownURL = oldURL }()

	cfg := Config{Listen: freeAddr(t), AdminListen: freeAddr(t), AdminTokenSHA256: sha256Hex(testAdminToken), DBFile: "attendance.db"}
	errc := make(chan error, 1)
	go func() { errc <- serve(cfg, db) }()
	waitDial(t, cfg.Listen)
	waitDial(t, cfg.AdminListen)

	req, _ := http.NewRequest("POST", "http://"+cfg.AdminListen+"/admin/api/server/shutdown", strings.NewReader(`{"confirm":"shutdown"}`))
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 202 {
		t.Fatalf("the 202 must reach the client: %v %v", err, resp)
	}
	resp.Body.Close()
	select {
	case err := <-errc:
		t.Fatalf("admin panel serve should keep running: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if c, err := net.DialTimeout("tcp", cfg.AdminListen, time.Second); err != nil {
		t.Fatalf("admin listener stopped: %v", err)
	} else {
		c.Close()
	}
	if c, err := net.DialTimeout("tcp", cfg.Listen, time.Second); err != nil {
		t.Fatalf("public listener stopped: %v", err)
	} else {
		c.Close()
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM admin_actions WHERE action='shutdown' AND outcome='ok'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("shutdown not recorded: %d %v", n, err)
	}
}

// ---- G4: the embedded UI ----

func TestAdminServesUIWithoutToken(t *testing.T) {
	e, ac := newAdminEnv(t)
	good := ac.token
	ac.token = ""

	st, hdr, body := ac.do("GET", "/", "")
	if st != 200 || !strings.Contains(string(body), "<html") ||
		!strings.Contains(hdr.Get("Content-Security-Policy"), "default-src 'self'") ||
		!strings.Contains(hdr.Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Referrer-Policy") != "no-referrer" ||
		hdr.Get("Cache-Control") != "no-cache" {
		t.Fatalf("index: %d %v", st, hdr)
	}
	if st, _, body := ac.do("GET", "/some/client/route", ""); st != 200 || !strings.Contains(string(body), "<html") {
		t.Fatalf("SPA fallback: %d", st)
	}
	if st, _, _ := ac.do("GET", "/assets/missing.js", ""); st != 404 {
		t.Fatalf("missing asset must be 404, got %d", st)
	}
	if st, _, _ := ac.do("POST", "/", ""); st != 404 {
		t.Fatalf("non-GET on a UI path: %d", st)
	}

	// the API stays behind the token, and static requests never count toward the throttle
	ac.wantErr("GET", "/admin/api/status", "", 401, "unauthorized")
	for i := 0; i < 30; i++ {
		ac.do("GET", "/", "")
	}
	ac.token = good
	st, hdr, _ = ac.do("GET", "/admin/api/status", "")
	if st != 200 || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("api: %d %v", st, hdr)
	}
	ac.token = "wrong"
	for i := 0; i < 9; i++ { // 1 failure above + 9 = 10
		ac.wantErr("GET", "/admin/api/status", "", 401, "unauthorized")
	}
	ac.wantErr("GET", "/admin/api/status", "", 429, "too_many_attempts")

	// none of it exists on the phone-facing port
	for _, p := range []string{"/", "/index.html", "/assets/x.js", "/admin/", "/admin/api/status"} {
		resp, err := http.Get(e.ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("public %s: %d", p, resp.StatusCode)
		}
	}
}
