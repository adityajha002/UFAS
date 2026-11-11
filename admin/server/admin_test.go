package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAdminToken = "test-admin-token"

type adminClient struct {
	t     *testing.T
	url   string
	token string
}

// newAdminEnv is newEnv plus the admin listener's handler on its own test server.
func newAdminEnv(t *testing.T) (*env, *adminClient) {
	t.Helper()
	e := newEnv(t)
	cfg := e.s.config()
	cfg.AdminListen = "127.0.0.1:0"
	cfg.AdminTokenSHA256 = sha256Hex(testAdminToken)
	e.s.setConfig(cfg)
	e.s.base = cfg
	e.s.netAddrs = func() []netAddr { return []netAddr{{Interface: "eth0", IP: "192.168.1.20"}} }
	as := httptest.NewServer(e.s.adminRoutes())
	t.Cleanup(as.Close) // runs before newEnv's cleanup closes the DB
	return e, &adminClient{t: t, url: as.URL, token: testAdminToken}
}

func (c *adminClient) do(method, path, body string) (int, http.Header, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.url+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func (c *adminClient) call(method, path, body string) (int, map[string]any) {
	c.t.Helper()
	st, _, b := c.do(method, path, body)
	var m map[string]any
	json.Unmarshal(b, &m)
	return st, m
}

func (c *adminClient) wantErr(method, path, body string, status int, code string) map[string]any {
	c.t.Helper()
	st, out := c.call(method, path, body)
	if st != status || out["error"] != code {
		c.t.Fatalf("%s %s: want %d %q, got %d %v", method, path, status, code, st, out)
	}
	return out
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }

func auditItems(out map[string]any) []map[string]any {
	var r []map[string]any
	for _, v := range asList(out["items"]) {
		r = append(r, asMap(v))
	}
	return r
}

func (e *env) dbCount(q string, args ...any) (n int) {
	e.t.Helper()
	if err := e.s.db.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// ---- auth ----

func TestAdminAuth(t *testing.T) {
	e, ac := newAdminEnv(t)

	// admin routes do not exist on the phone-facing mux
	resp, err := http.Get(e.ts.URL + "/admin/api/status")
	if err != nil || resp.StatusCode != 404 {
		t.Fatalf("public mux must 404 admin paths: %v %v", err, resp)
	}
	resp.Body.Close()

	good := ac.token
	ac.token = ""
	ac.wantErr("GET", "/admin/api/status", "", 401, "unauthorized")
	ac.wantErr("GET", "/admin/api/nope", "", 401, "unauthorized") // no probing without a token
	ac.token = "wrong"
	ac.wantErr("GET", "/admin/api/status", "", 401, "unauthorized")
	req, _ := http.NewRequest("GET", ac.url+"/admin/api/status", nil)
	req.Header.Set("Authorization", "Basic "+good)
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 401 {
		t.Fatalf("malformed scheme: %v %v", err, r)
	} else {
		r.Body.Close()
	}

	ac.token = good
	st, hdr, _ := ac.do("GET", "/admin/api/status", "")
	if st != 200 || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("good token: %d %v", st, hdr)
	}
	ac.wantErr("GET", "/admin/api/nope", "", 404, "not_found")
}

func TestAdminThrottle(t *testing.T) {
	_, ac := newAdminEnv(t)
	good := ac.token
	ac.token = "wrong"
	for i := 0; i < 10; i++ {
		ac.wantErr("GET", "/admin/api/status", "", 401, "unauthorized")
	}
	ac.wantErr("GET", "/admin/api/status", "", 429, "too_many_attempts")
	ac.token = good // blocked even with the right token until the minute is over
	ac.wantErr("GET", "/admin/api/status", "", 429, "too_many_attempts")
}

func TestAdminAuthDisabledWithoutHash(t *testing.T) {
	e, ac := newAdminEnv(t)
	cfg := e.s.config()
	cfg.AdminTokenSHA256 = ""
	e.s.setConfig(cfg)
	ac.token = ""
	if st, _, _ := ac.do("GET", "/admin/api/status", ""); st != 200 {
		t.Fatalf("empty hash disables auth, want 200 got %d", st)
	}

	// startup validation accepts a listener with no hash...
	cfg.AdminListen = "127.0.0.1:9090"
	if errs := validateConfig(cfg); errs["admin.token_sha256"] != "" {
		t.Fatalf("empty hash must be valid: %v", errs)
	}
	// ...but a non-empty hash must still be well-formed
	cfg.AdminTokenSHA256 = "abc"
	if errs := validateConfig(cfg); errs["admin.token_sha256"] == "" {
		t.Fatalf("malformed hash must be invalid: %v", errs)
	}
}

// ---- status ----

func TestAdminStatus(t *testing.T) {
	e, ac := newAdminEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatal("attend failed")
	}
	if st, _ := p.attend(farLatMicro, geoLonMicro, 1500); st != 422 {
		t.Fatal("expected outside_area")
	}

	st, out := ac.call("GET", "/admin/api/status", "")
	if st != 200 || out["running"] != true {
		t.Fatalf("%d %v", st, out)
	}
	counts := asMap(out["counts"])
	if counts["faculty"] != 1.0 || counts["enrolled"] != 1.0 || counts["marked_today"] != 1.0 {
		t.Fatalf("counts %v", counts)
	}
	att := asMap(out["attendance_attempts_24h"])
	if att["ok"] != 1.0 || att["failed"] != 1.0 {
		t.Fatalf("attempts %v", att)
	}
	if asMap(out["database"])["ok"] != true {
		t.Fatalf("db %v", out["database"])
	}

	for _, c := range []struct {
		at           time.Time
		open         bool
		reason, next string
	}{
		{friday10(), true, "open", "2026-10-23 18:00:00"},
		{time.Date(2026, 10, 23, 19, 0, 0, 0, ist), false, "after_end", "2026-10-26 09:00:00"},
		{time.Date(2026, 10, 23, 8, 0, 0, 0, ist), false, "before_start", "2026-10-23 09:00:00"},
		{time.Date(2026, 10, 24, 10, 0, 0, 0, ist), false, "weekend", "2026-10-26 09:00:00"},
	} {
		e.setClock(c.at)
		_, out := ac.call("GET", "/admin/api/status", "")
		w := asMap(out["attendance_window"])
		if w["open"] != c.open || w["reason"] != c.reason || w["next_change_at_ist"] != c.next {
			t.Fatalf("%v: %v", c.at, w)
		}
	}
	e.setClock(friday10())

	// loopback bind: warning and no phone URLs
	cfg := e.s.config()
	cfg.Listen = "127.0.0.1:8080"
	e.s.setConfig(cfg)
	_, out = ac.call("GET", "/admin/api/status", "")
	nw := asMap(out["network"])
	if len(asList(nw["phone_urls"])) != 0 || !strings.Contains(nw["warning"].(string), "loopback") ||
		asMap(out["public_listener"])["loopback_only"] != true {
		t.Fatalf("loopback: %v", out)
	}

	// wildcard bind: one URL per address
	cfg.Listen = "0.0.0.0:8080"
	e.s.setConfig(cfg)
	_, out = ac.call("GET", "/admin/api/status", "")
	urls := asList(asMap(out["network"])["phone_urls"])
	if len(urls) != 1 || urls[0] != "http://192.168.1.20:8080" {
		t.Fatalf("urls %v", urls)
	}

	// reachable against a live listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cfg.Listen = ln.Addr().String()
	e.s.setConfig(cfg)
	_, out = ac.call("GET", "/admin/api/status", "")
	if asMap(out["public_listener"])["reachable"] != true {
		t.Fatalf("reachable: %v", out["public_listener"])
	}
}

// ---- day view ----

func TestAdminDayView(t *testing.T) {
	e, ac := newAdminEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	e.enroll("F002", "Dr. Iyer")                                      // enrolled, absent
	if _, err := addFaculty(e.s.db, "F003", "Dr. Nair"); err != nil { // not enrolled
		t.Fatal(err)
	}
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatal("attend failed")
	}
	if err := markManual(e.s.db, "F003", "2026-10-23", "paper sheet", e.s.now()); err != nil {
		t.Fatal(err)
	}

	st, out := ac.call("GET", "/admin/api/attendance/day", "") // default: today IST
	if st != 200 || out["date"] != "2026-10-23" || out["is_today"] != true || out["is_weekend"] != false {
		t.Fatalf("%d %v", st, out)
	}
	sum := asMap(out["summary"])
	if sum["total"] != 3.0 || sum["marked"] != 2.0 || sum["not_marked"] != 1.0 || sum["not_enrolled"] != 1.0 {
		t.Fatalf("summary %v", sum)
	}
	by := map[string]map[string]any{}
	for _, v := range asList(out["faculty"]) {
		m := asMap(v)
		by[m["employee_id"].(string)] = m
	}
	if by["F001"]["source"] != "phone" || by["F001"]["distance_m"] == nil || by["F001"]["marked_at_ist"] != "2026-10-23 10:00:00" {
		t.Fatalf("F001 %v", by["F001"])
	}
	if by["F002"]["marked"] != false || by["F002"]["source"] != nil || by["F002"]["marked_at_ist"] != nil {
		t.Fatalf("F002 %v", by["F002"])
	}
	if by["F003"]["source"] != "manual" || by["F003"]["note"] != "paper sheet" || by["F003"]["enrolled"] != false {
		t.Fatalf("F003 %v", by["F003"])
	}

	ac.wantErr("GET", "/admin/api/attendance/day?date=2026-13-01", "", 400, "bad_date")
	ac.wantErr("GET", "/admin/api/attendance/day?date=yesterday", "", 400, "bad_date")
	_, out = ac.call("GET", "/admin/api/attendance/day?date=2026-10-25", "") // Sunday
	if out["is_weekend"] != true || out["is_future"] != true || asMap(out["summary"])["marked"] != 0.0 {
		t.Fatalf("sunday %v", out)
	}
}

// ---- audit ----

func TestAuditRecordsEveryRequest(t *testing.T) {
	e, ac := newAdminEnv(t)
	p := e.enroll("F001", "Dr. Rao")

	before := e.dbCount(`SELECT count(*) FROM audit_log`)
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 { // challenge + attendance
		t.Fatal("attend failed")
	}
	if n := e.dbCount(`SELECT count(*) FROM audit_log`); n != before+2 {
		t.Fatalf("one row per request: %d -> %d", before, n)
	}
	// a rejected attempt with a bad signature: its secrets must not be stored
	cid, nonce := p.challenge()
	body := p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)
	body["signature"] = p.sign("something else entirely")
	st, out := e.post("/attendance", body)
	e.wantErr(st, out, "bad_signature", 401)
	if st, _ := p.attend(farLatMicro, geoLonMicro, 1500); st != 422 {
		t.Fatal("expected outside_area")
	}
	if st, out := e.post("/challenge", map[string]any{"faculty_id": "nope"}); st != 403 {
		t.Fatalf("%d %v", st, out)
	}

	st, out = ac.call("GET", "/admin/api/audit?outcome=outside_area", "")
	items := auditItems(out)
	if st != 200 || len(items) != 1 {
		t.Fatalf("%d %v", st, out)
	}
	it := items[0]
	pol := asMap(it["policy"])
	if it["endpoint"] != "attendance" || it["http_status"] != 422.0 || it["distance_m"].(float64) < 9000 ||
		pol["radius_m"] != 150.0 || pol["window_start"] != "09:00" || pol["window_end"] != "18:00" ||
		asMap(it["faculty"])["employee_id"] != "F001" || it["lat"].(float64) < 28.69 || it["accuracy_m"] != 15.0 ||
		it["at_ist"] != "2026-10-23 10:00:00" {
		t.Fatalf("item %v", it)
	}

	_, out = ac.call("GET", "/admin/api/audit?outcome=failed", "")
	if n := len(auditItems(out)); n != 3 { // bad_signature, outside_area, not_enrolled
		t.Fatalf("failed: %d %v", n, out)
	}
	_, out = ac.call("GET", "/admin/api/audit?endpoint=attendance&employee_id=F001", "")
	if n := len(auditItems(out)); n != 3 {
		t.Fatalf("attendance rows: %d", n)
	}
	_, out = ac.call("GET", "/admin/api/audit?endpoint=register", "")
	if n := len(auditItems(out)); n != 1 {
		t.Fatalf("register rows: %d", n)
	}

	// unknown claimed faculty id: still listed, with null employee
	_, out = ac.call("GET", "/admin/api/audit?faculty_id=nope", "")
	items = auditItems(out)
	if len(items) != 1 || asMap(items[0]["faculty"])["id"] != "nope" || asMap(items[0]["faculty"])["employee_id"] != nil ||
		items[0]["lat"] != nil || items[0]["distance_m"] != nil {
		t.Fatalf("unknown faculty: %v", items)
	}

	// no secrets anywhere in the table
	var dump string
	if err := e.s.db.QueryRow(`SELECT COALESCE(group_concat(endpoint||'|'||method||'|'||path||'|'||outcome||'|'||
		COALESCE(faculty_id,'')||'|'||COALESCE(challenge_id,'')||'|'||COALESCE(remote_ip,'')||'|'||
		COALESCE(user_agent,'')||'|'||COALESCE(policy,''), ' '), '') FROM audit_log`).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{nonce, body["signature"].(string), p.pubB64()} {
		if strings.Contains(dump, secret) {
			t.Fatalf("audit_log leaks %q", secret)
		}
	}

	// detail
	id := int64(it["id"].(float64))
	st, out = ac.call("GET", "/admin/api/audit/"+itoa(id), "")
	if st != 200 || out["outcome"] != "outside_area" {
		t.Fatalf("detail %d %v", st, out)
	}
	ac.wantErr("GET", "/admin/api/audit/999999", "", 404, "not_found")
	ac.wantErr("GET", "/admin/api/audit/abc", "", 404, "not_found")
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestAuditFiltersAndPaging(t *testing.T) {
	e, ac := newAdminEnv(t)
	for i := 0; i < 5; i++ {
		e.post("/challenge", map[string]any{"faculty_id": "nope"})
	}
	_, out := ac.call("GET", "/admin/api/audit?limit=2", "")
	first := auditItems(out)
	if len(first) != 2 || out["next_before_id"] == nil {
		t.Fatalf("page 1: %v", out)
	}
	if first[0]["id"].(float64) < first[1]["id"].(float64) {
		t.Fatal("must be newest first")
	}
	next := itoa(int64(out["next_before_id"].(float64)))
	_, out = ac.call("GET", "/admin/api/audit?limit=2&before_id="+next, "")
	second := auditItems(out)
	if len(second) != 2 || second[0]["id"].(float64) >= first[1]["id"].(float64) {
		t.Fatalf("page 2 overlaps: %v", second)
	}
	// polling: only rows newer than the newest we have
	newest := itoa(int64(first[0]["id"].(float64)))
	_, out = ac.call("GET", "/admin/api/audit?after_id="+newest, "")
	if len(auditItems(out)) != 0 {
		t.Fatal("nothing newer yet")
	}
	e.post("/challenge", map[string]any{"faculty_id": "nope"})
	_, out = ac.call("GET", "/admin/api/audit?after_id="+newest, "")
	if len(auditItems(out)) != 1 {
		t.Fatal("expected exactly the new row")
	}

	// date range is inclusive and in IST
	_, out = ac.call("GET", "/admin/api/audit?from=2026-10-23&to=2026-10-23", "")
	if len(auditItems(out)) == 0 {
		t.Fatal("today's rows must be in 10-23..10-23")
	}
	_, out = ac.call("GET", "/admin/api/audit?from=2026-10-22&to=2026-10-22", "")
	if len(auditItems(out)) != 0 {
		t.Fatal("no rows on 10-22")
	}
	ac.wantErr("GET", "/admin/api/audit?from=2026-10-24&to=2026-10-23", "", 400, "bad_range")
	ac.wantErr("GET", "/admin/api/audit?from=2025-01-01&to=2026-10-23", "", 400, "range_too_large")
	ac.wantErr("GET", "/admin/api/audit?from=nope", "", 400, "bad_date")
	ac.wantErr("GET", "/admin/api/audit?endpoint=bogus", "", 400, "bad_request")
	ac.wantErr("GET", "/admin/api/audit?limit=1000", "", 400, "bad_request")
}

func TestAuditWriteFailureDoesNotChangeResponse(t *testing.T) {
	e, _ := newAdminEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	if _, err := e.s.db.Exec(`DROP TABLE audit_log`); err != nil {
		t.Fatal(err)
	}
	if st, out := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatalf("attendance must still succeed: %d %v", st, out)
	}
}

func TestAuditPrune(t *testing.T) {
	e, _ := newAdminEnv(t)
	cfg := e.s.config()
	cfg.AuditRetentionDays, cfg.AuditMaxRows = 10, 3
	e.s.setConfig(cfg)
	now := e.s.now().Unix()
	ins := func(ts int64) {
		if _, err := e.s.db.Exec(`INSERT INTO audit_log (ts, endpoint, method, path, status, outcome)
			VALUES (?, 'challenge', 'POST', '/challenge', 403, 'not_enrolled')`, ts); err != nil {
			t.Fatal(err)
		}
	}
	ins(now - 20*86400)
	ins(now - 15*86400)
	for i := 0; i < 5; i++ {
		ins(now - int64(i))
	}
	n, err := e.s.pruneAudit(context.Background())
	if err != nil || n != 4 { // 2 by age, then 2 oldest of the 5 left by the row cap
		t.Fatalf("pruned %d, err %v", n, err)
	}
	if left := e.dbCount(`SELECT count(*) FROM audit_log`); left != 3 {
		t.Fatalf("left %d", left)
	}
	if old := e.dbCount(`SELECT count(*) FROM audit_log WHERE ts < ?`, now-5); old != 0 {
		t.Fatal("old rows survived")
	}
}

// ---- config ----

func TestAdminConfigPatchAndPolicySnapshot(t *testing.T) {
	e, ac := newAdminEnv(t)
	p := e.enroll("F001", "Dr. Rao")

	st, out := ac.call("GET", "/admin/api/config", "")
	if st != 200 || strings.Contains(asJSON(out), sha256Hex(testAdminToken)) {
		t.Fatalf("config must never expose the token hash: %d", st)
	}
	field := func(out map[string]any, key string) map[string]any {
		for _, v := range asList(out["fields"]) {
			if m := asMap(v); m["key"] == key {
				return m
			}
		}
		t.Fatalf("no field %s", key)
		return nil
	}
	if f := field(out, "geofence.radius_m"); f["source"] != "yaml" || f["value"] != 150.0 || f["editable"] != true {
		t.Fatalf("%v", f)
	}
	if f := field(out, "server.listen"); f["editable"] != false || f["restart_required"] != true {
		t.Fatalf("%v", f)
	}

	// ~222 m away: outside the 150 m radius ...
	if st, out := p.attend(geoLatMicro+2000, geoLonMicro, 1500); st != 422 || out["error"] != "outside_area" {
		t.Fatalf("%d %v", st, out)
	}
	// dry run changes nothing
	st, out = ac.call("PATCH", "/admin/api/config?dry_run=true", `{"geofence":{"radius_m":500}}`)
	if st != 200 || out["dry_run"] != true || e.s.config().RadiusM != 150 {
		t.Fatalf("dry run: %d %v", st, out)
	}
	if e.dbCount(`SELECT count(*) FROM settings`) != 0 || e.dbCount(`SELECT count(*) FROM config_history`) != 0 {
		t.Fatal("dry run wrote something")
	}
	// ... and inside after the real change, applied to the very next submit
	st, out = ac.call("PATCH", "/admin/api/config", `{"geofence":{"radius_m":500}}`)
	ch := asMap(asMap(out["changed"])["geofence.radius_m"])
	if st != 200 || ch["old"] != 150.0 || ch["new"] != 500.0 {
		t.Fatalf("patch: %d %v", st, out)
	}
	if f := field(out, "geofence.radius_m"); f["source"] != "override" || f["yaml_value"] != 150.0 || f["value"] != 500.0 {
		t.Fatalf("%v", f)
	}
	if st, out := p.attend(geoLatMicro+2000, geoLonMicro, 1500); st != 201 {
		t.Fatalf("new radius must apply: %d %v", st, out)
	}

	// the audit rows carry the radius in force at the time
	_, out = ac.call("GET", "/admin/api/audit?endpoint=attendance", "")
	items := auditItems(out)
	if len(items) != 2 || asMap(items[0]["policy"])["radius_m"] != 500.0 || asMap(items[1]["policy"])["radius_m"] != 150.0 {
		t.Fatalf("policy snapshots: %v", items)
	}

	// patching back to the YAML value removes the override instead of storing a duplicate
	if st, _ := ac.call("PATCH", "/admin/api/config", `{"geofence":{"radius_m":150}}`); st != 200 {
		t.Fatal("patch back")
	}
	if e.dbCount(`SELECT count(*) FROM settings WHERE key='geofence.radius_m'`) != 0 {
		t.Fatal("override should be gone")
	}
	_, out = ac.call("GET", "/admin/api/config/history", "")
	hist := auditItems(out)
	if len(hist) != 2 || asMap(hist[0]["changes"])["geofence.radius_m"] == nil {
		t.Fatalf("history %v", out)
	}
}

func asJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestAdminConfigValidation(t *testing.T) {
	e, ac := newAdminEnv(t)
	for _, body := range []string{
		`{"window":{"start":"19:00"}}`, // start after the current end
		`{"window":{"end":"08:00"}}`,   // end before the current start
		`{"window":{"start":"9am"}}`,   // not a time
		`{"geofence":{"lat":95}}`,      // out of range
		`{"geofence":{"lon":-181}}`,
		`{"geofence":{"radius_m":0}}`,
		`{"geofence":{"max_accuracy_m":-5}}`,
	} {
		out := ac.wantErr("PATCH", "/admin/api/config", body, 422, "invalid_config")
		if len(asMap(out["fields"])) == 0 {
			t.Fatalf("%s: expected per-field messages: %v", body, out)
		}
	}
	for _, body := range []string{``, `{}`, `{"geofence":{}}`, `{"geofence":{"radious":1}}`, `{"server":{"listen":":1"}}`, `{"geofence":{"radius_m":1}} {}`} {
		ac.wantErr("PATCH", "/admin/api/config", body, 400, "bad_request")
	}
	if c := e.s.config(); c.RadiusM != 150 || c.GeoLat != 28.6139 || c.WinStart != 540 || c.WinEnd != 1080 {
		t.Fatalf("live config changed by rejected patches: %+v", c)
	}
	if e.dbCount(`SELECT count(*) FROM settings`) != 0 || e.dbCount(`SELECT count(*) FROM config_history`) != 0 {
		t.Fatal("rejected patches wrote something")
	}
}

func TestAdminConfigReset(t *testing.T) {
	e, ac := newAdminEnv(t)
	ac.call("PATCH", "/admin/api/config", `{"geofence":{"radius_m":250},"window":{"end":"19:00"}}`)
	if c := e.s.config(); c.RadiusM != 250 || c.WinEnd != 19*60 {
		t.Fatalf("%+v", c)
	}
	if st, _ := ac.call("DELETE", "/admin/api/config/overrides?key=geofence.radius_m", ""); st != 200 {
		t.Fatal("reset one")
	}
	if c := e.s.config(); c.RadiusM != 150 || c.WinEnd != 19*60 {
		t.Fatalf("only the named key may revert: %+v", c)
	}
	if st, _ := ac.call("DELETE", "/admin/api/config/overrides", ""); st != 200 {
		t.Fatal("reset all")
	}
	if c := e.s.config(); c.WinEnd != 18*60 || e.dbCount(`SELECT count(*) FROM settings`) != 0 {
		t.Fatalf("%+v", c)
	}
	ac.wantErr("DELETE", "/admin/api/config/overrides?key=server.listen", "", 422, "invalid_config")

	// resetting just one of a consistent pair would leave start >= end
	ac.call("PATCH", "/admin/api/config", `{"window":{"start":"07:00","end":"08:00"}}`)
	ac.wantErr("DELETE", "/admin/api/config/overrides?key=window.start", "", 422, "invalid_config")
	if c := e.s.config(); c.WinStart != 7*60 || c.WinEnd != 8*60 {
		t.Fatalf("rejected reset must change nothing: %+v", c)
	}
	if st, _ := ac.call("DELETE", "/admin/api/config/overrides", ""); st != 200 {
		t.Fatal("reset all")
	}
}

func TestStoredOverridesAtStartup(t *testing.T) {
	e, ac := newAdminEnv(t)
	for k, v := range map[string]string{
		"geofence.radius_m": "abc",   // unparsable
		"geofence.lat":      "95",    // fails validation
		"window.start":      "10:00", // fine
		"bogus.key":         "1",     // unknown
	} {
		if _, err := e.s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.s.applyStoredOverrides()
	c := e.s.config()
	if c.RadiusM != 150 || c.GeoLat != 28.6139 || c.WinStart != 10*60 {
		t.Fatalf("%+v", c)
	}
	_, out := ac.call("GET", "/admin/api/status", "")
	if w := asList(out["config_warnings"]); len(w) != 3 {
		t.Fatalf("warnings %v", w)
	}
	// resetting clears the junk rows and their warnings
	ac.call("DELETE", "/admin/api/config/overrides", "")
	_, out = ac.call("GET", "/admin/api/status", "")
	if e.dbCount(`SELECT count(*) FROM settings WHERE key IN ('geofence.radius_m','geofence.lat','window.start')`) != 0 ||
		len(asList(out["config_warnings"])) != 1 { // only the unknown key's warning remains
		t.Fatalf("after reset: %v", out["config_warnings"])
	}
}

func TestConfigPatchRacesWithAttendance(t *testing.T) {
	e, ac := newAdminEnv(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	probe := []byte(`{"challenge_id":"x","faculty_id":"y","lat_micro":1,"lon_micro":1,"accuracy_cm":1,"signature":"z"}`)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if resp, err := http.Post(e.ts.URL+"/attendance", "application/json", bytes.NewReader(probe)); err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}
		}()
	}
	for i := 0; i < 30; i++ {
		body := `{"geofence":{"radius_m":150}}`
		if i%2 == 1 {
			body = `{"geofence":{"radius_m":300}}`
		}
		if st, _ := ac.call("PATCH", "/admin/api/config", body); st != 200 {
			t.Fatalf("patch %d: %d", i, st)
		}
	}
	close(stop)
	wg.Wait()
	if e.s.config().RadiusM != 300 {
		t.Fatalf("last write must win: %v", e.s.config().RadiusM)
	}
}

// ---- export ----

func TestAdminExportCSV(t *testing.T) {
	e, ac := newAdminEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	if _, err := addFaculty(e.s.db, "F002", `=HYPERLINK("http://x")`); err != nil {
		t.Fatal(err)
	}
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 { // Fri 10-23
		t.Fatal("attend")
	}
	if err := markManual(e.s.db, "F002", "2026-10-24", "=cmd", e.s.now()); err != nil { // Sat 10-24
		t.Fatal(err)
	}
	e.setClock(time.Date(2026, 10, 26, 10, 0, 0, 0, ist)) // Mon 10-26
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatal("attend monday")
	}

	read := func(path string) (http.Header, [][]string, []byte) {
		st, hdr, b := ac.do("GET", path, "")
		if st != 200 {
			t.Fatalf("%s: %d %s", path, st, b)
		}
		body := bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
		rows, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return hdr, rows, b
	}

	hdr, rows, raw := read("/admin/api/export/attendance.csv?from=2026-10-23&to=2026-10-26")
	if !bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("BOM expected by default")
	}
	if hdr.Get("Content-Type") != "text/csv; charset=utf-8" ||
		hdr.Get("Content-Disposition") != `attachment; filename="attendance_2026-10-23_to_2026-10-26.csv"` {
		t.Fatalf("headers %v", hdr)
	}
	want := []string{"employee_id", "name", "day", "marked_at_ist", "source", "distance_m", "note"}
	if strings.Join(rows[0], ",") != strings.Join(want, ",") || len(rows) != 4 {
		t.Fatalf("%v", rows)
	}
	// sorted by day, then employee_id; inclusive on both ends
	if rows[1][0] != "F001" || rows[1][2] != "2026-10-23" || rows[1][3] != "2026-10-23 10:00:00" || rows[1][4] != "phone" ||
		rows[2][0] != "F002" || rows[2][2] != "2026-10-24" || rows[2][4] != "manual" ||
		rows[3][0] != "F001" || rows[3][2] != "2026-10-26" {
		t.Fatalf("%v", rows)
	}
	if rows[2][1] != `'=HYPERLINK("http://x")` || rows[2][6] != "'=cmd" { // formula injection neutralised
		t.Fatalf("injection guard: %q %q", rows[2][1], rows[2][6])
	}

	_, rows, raw = read("/admin/api/export/attendance.csv?from=2026-10-24&to=2026-10-24&bom=0&include_location=1")
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) || len(rows) != 2 || len(rows[0]) != 10 || rows[0][7] != "lat" {
		t.Fatalf("bom=0 / location: %v", rows)
	}
	_, rows, _ = read("/admin/api/export/attendance.csv?from=2026-10-23&to=2026-10-23&include_location=1")
	if rows[1][7] != "28.613900" || rows[1][9] != "15.00" {
		t.Fatalf("location columns: %v", rows[1])
	}

	ac.wantErr("GET", "/admin/api/export/attendance.csv?from=2026-10-26&to=2026-10-23", "", 400, "bad_range")
	ac.wantErr("GET", "/admin/api/export/attendance.csv?from=2026-10-26", "", 400, "bad_range")
	ac.wantErr("GET", "/admin/api/export/attendance.csv", "", 400, "bad_range")
	ac.wantErr("GET", "/admin/api/export/attendance.csv?from=2025-01-01&to=2026-10-26", "", 400, "range_too_large")
	ac.wantErr("GET", "/admin/api/export/attendance.csv?from=2026-02-30&to=2026-10-26", "", 400, "bad_date")
	// exactly 366 days is allowed
	if st, _, _ := ac.do("GET", "/admin/api/export/attendance.csv?from=2025-10-26&to=2026-10-26", ""); st != 200 {
		t.Fatalf("366 days: %d", st)
	}
}

// ---- faculty list and per-user totals ----

func TestAdminFacultyTotals(t *testing.T) {
	e, ac := newAdminEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	e.enroll("F002", "Dr. Iyer") // enrolled, never present
	if _, err := addFaculty(e.s.db, "F003", "Dr. Nair"); err != nil {
		t.Fatal(err)
	}
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 { // Fri 10-23
		t.Fatal("attend")
	}
	if err := markManual(e.s.db, "F003", "2026-10-24", "paper sheet", e.s.now()); err != nil {
		t.Fatal(err)
	}
	e.setClock(time.Date(2026, 10, 26, 10, 0, 0, 0, ist)) // Mon
	if st, _ := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatal("attend monday")
	}

	rowsOf := func(path string) map[string]map[string]any {
		st, out := ac.call("GET", path, "")
		if st != 200 {
			t.Fatalf("%s: %d %v", path, st, out)
		}
		m := map[string]map[string]any{}
		for _, v := range asList(out["faculty"]) {
			r := asMap(v)
			m[r["employee_id"].(string)] = r
		}
		return m
	}
	all := rowsOf("/admin/api/faculty")
	if len(all) != 3 || all["F001"]["days_present"] != 2.0 || all["F001"]["via_phone"] != 2.0 ||
		all["F001"]["last_marked_day"] != "2026-10-26" || all["F002"]["days_present"] != 0.0 ||
		all["F002"]["last_marked_day"] != nil || all["F002"]["enrolled"] != true ||
		all["F003"]["days_present"] != 1.0 || all["F003"]["manual"] != 1.0 || all["F003"]["enrolled"] != false {
		t.Fatalf("%v", all)
	}
	ranged := rowsOf("/admin/api/faculty?from=2026-10-23&to=2026-10-23")
	if len(ranged) != 3 || ranged["F001"]["days_present"] != 1.0 || ranged["F003"]["days_present"] != 0.0 {
		t.Fatalf("ranged: %v", ranged)
	}

	st, out := ac.call("GET", "/admin/api/faculty/F001/attendance", "")
	days := asList(out["days"])
	if st != 200 || out["days_present"] != 2.0 || out["first_marked_day"] != "2026-10-23" ||
		out["last_marked_day"] != "2026-10-26" || out["days_truncated"] != false || len(days) != 2 ||
		asMap(days[0])["day"] != "2026-10-26" || asMap(days[0])["source"] != "phone" ||
		asMap(asMap(out)["employee"])["name"] != "Dr. Rao" {
		t.Fatalf("%d %v", st, out)
	}
	_, out = ac.call("GET", "/admin/api/faculty/F003/attendance", "")
	if out["manual"] != 1.0 || asMap(asList(out["days"])[0])["source"] != "manual" ||
		asMap(asList(out["days"])[0])["note"] != "paper sheet" {
		t.Fatalf("F003: %v", out)
	}
	_, out = ac.call("GET", "/admin/api/faculty/F001/attendance?from=2026-10-24&to=2026-10-25", "")
	if out["days_present"] != 0.0 || out["first_marked_day"] != nil || len(asList(out["days"])) != 0 {
		t.Fatalf("empty range: %v", out)
	}

	ac.wantErr("GET", "/admin/api/faculty/NOPE/attendance", "", 404, "not_found")
	ac.wantErr("GET", "/admin/api/faculty/F001/attendance?from=2026-10-26&to=2026-10-23", "", 400, "bad_range")
	ac.wantErr("GET", "/admin/api/faculty?from=2026-10-23", "", 400, "bad_range")
	ac.wantErr("GET", "/admin/api/faculty?from=x&to=y", "", 400, "bad_date")
}

func TestAdminCORSAndPreflight(t *testing.T) {
	_, ac := newAdminEnv(t)
	good := ac.token
	ac.token = ""

	// 20 preflight OPTIONS requests without a token must all return 204 with CORS
	// and never trigger authThrottle.
	for i := 0; i < 20; i++ {
		st, hdr, _ := ac.do("OPTIONS", "/admin/api/status", "")
		if st != 204 || hdr.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("options %d: status=%d cors=%q", i, st, hdr.Get("Access-Control-Allow-Origin"))
		}
	}

	ac.token = good
	st, hdr, _ := ac.do("GET", "/admin/api/status", "")
	if st != 200 || hdr.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("get after options: status=%d cors=%q", st, hdr.Get("Access-Control-Allow-Origin"))
	}
}
