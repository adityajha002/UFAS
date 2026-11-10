package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Building centre in micro-degrees, matching the test config below.
const (
	geoLatMicro = 28_613_900
	geoLonMicro = 77_209_000
	farLatMicro = 28_700_000 // roughly 9 km north
)

type env struct {
	t     *testing.T
	s     *Server
	ts    *httptest.Server
	clock atomic.Int64
}

func (e *env) setClock(tm time.Time) { e.clock.Store(tm.Unix()) }

// Friday 2026-10-23, 10:00 campus time, inside the window.
func friday10() time.Time { return time.Date(2026, 10, 23, 10, 0, 0, 0, ist) }

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t}
	e.s = newServer(db, Config{GeoLat: 28.6139, GeoLon: 77.2090, RadiusM: 150, MaxAccM: 50,
		WinStart: 9 * 60, WinEnd: 18 * 60}, func() time.Time { return time.Unix(e.clock.Load(), 0) })
	e.setClock(friday10())
	e.ts = httptest.NewServer(e.s.routes())
	t.Cleanup(func() { e.ts.Close(); db.Close() })
	return e
}

func (e *env) post(path string, body any) (int, map[string]any) {
	e.t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(e.ts.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *env) wantErr(status int, out map[string]any, code string, wantStatus int) {
	e.t.Helper()
	if status != wantStatus || out["error"] != code {
		e.t.Fatalf("want %d %q, got %d %v", wantStatus, code, status, out)
	}
}

// phone is the fake handset: a real P-256 key that signs exactly like the Kotlin module.
type phone struct {
	e         *env
	key       *ecdsa.PrivateKey
	facultyID string
	employee  string
}

func (p *phone) pubB64() string {
	der, _ := x509.MarshalPKIXPublicKey(&p.key.PublicKey)
	return base64.StdEncoding.EncodeToString(der)
}

func (p *phone) sign(msg string) string {
	h := sha256.Sum256([]byte(msg))
	sig, err := ecdsa.SignASN1(rand.Reader, p.key, h[:])
	if err != nil {
		p.e.t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// enroll creates the faculty row, issues a code and registers a new phone.
func (e *env) enroll(employeeID, name string) *phone {
	e.t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p := &phone{e: e, key: key, employee: employeeID}
	if _, err := addFaculty(e.s.db, employeeID, name); err != nil {
		e.t.Fatal(err)
	}
	code, err := issueCode(e.s.db, employeeID, e.s.now())
	if err != nil {
		e.t.Fatal(err)
	}
	st, out := e.post("/register", map[string]any{"code": code, "public_key": p.pubB64()})
	if st != 201 {
		e.t.Fatalf("register: %d %v", st, out)
	}
	p.facultyID = out["faculty_id"].(string)
	return p
}

func (p *phone) challenge() (id, nonce string) {
	p.e.t.Helper()
	st, out := p.e.post("/challenge", map[string]any{"faculty_id": p.facultyID})
	if st != 201 {
		p.e.t.Fatalf("challenge: %d %v", st, out)
	}
	return out["challenge_id"].(string), out["nonce"].(string)
}

func (p *phone) attendBody(cid, nonce string, lat, lon, acc int64) map[string]any {
	return map[string]any{
		"challenge_id": cid, "faculty_id": p.facultyID,
		"lat_micro": lat, "lon_micro": lon, "accuracy_cm": acc,
		"signature": p.sign(attendMsg(cid, nonce, p.facultyID, lat, lon, acc)),
	}
}

func (p *phone) attendV2Body(cid, nonce, action string, lat, lon, acc int64) map[string]any {
	return map[string]any{
		"challenge_id": cid, "faculty_id": p.facultyID,
		"lat_micro": lat, "lon_micro": lon, "accuracy_cm": acc,
		"action":    action,
		"signature": p.sign(attendMsgV2(action, cid, nonce, p.facultyID, lat, lon, acc)),
	}
}

// attend does the whole flow: challenge, sign, submit.
func (p *phone) attend(lat, lon, acc int64) (int, map[string]any) {
	cid, nonce := p.challenge()
	return p.e.post("/attendance", p.attendBody(cid, nonce, lat, lon, acc))
}

func (p *phone) historyBody(cid, nonce string) map[string]any {
	return map[string]any{
		"challenge_id": cid, "faculty_id": p.facultyID,
		"signature": p.sign(historyMsg(cid, nonce, p.facultyID)),
	}
}

func (p *phone) history() (int, map[string]any) {
	cid, nonce := p.challenge()
	return p.e.post("/history", p.historyBody(cid, nonce))
}

func (e *env) countAttendance() (n int) {
	e.t.Helper()
	if err := e.s.db.QueryRow(`SELECT count(*) FROM attendance`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return
}

func asStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// ---- tests ----

func TestHappyPath(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")

	st, out := p.attend(geoLatMicro, geoLonMicro, 1500)
	if st != 201 || out["day"] != "2026-10-23" || out["distance_m"].(float64) > 1 {
		t.Fatalf("attendance: %d %v", st, out)
	}
	st, out = p.history()
	if st != 200 || out["name"] != "Dr. Rao" || out["today"] != "2026-10-23" ||
		out["marked_today"] != true || out["window_open"] != true {
		t.Fatalf("history: %d %v", st, out)
	}
	if d := asStrings(out["days"]); len(d) != 1 || d[0] != "2026-10-23" {
		t.Fatalf("days: %v", d)
	}
}

func TestV2AttendanceFlow(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")

	cid, nonce := p.challenge()
	st, out := e.post("/attendance", p.attendV2Body(cid, nonce, "in", geoLatMicro, geoLonMicro, 1500))
	if st != 201 || out["action"] != "in" || out["is_in"] != true {
		t.Fatalf("in: %d %v", st, out)
	}

	st, out = p.history()
	if st != 200 || out["is_in"] != true {
		t.Fatalf("history after in: %d %v", st, out)
	}

	cid, nonce = p.challenge()
	st, out = e.post("/attendance", p.attendV2Body(cid, nonce, "out", geoLatMicro, geoLonMicro, 1500))
	if st != 201 || out["action"] != "out" || out["is_in"] != false {
		t.Fatalf("out: %d %v", st, out)
	}

	st, out = p.history()
	if st != 200 || out["is_in"] != false {
		t.Fatalf("history after out: %d %v", st, out)
	}
}

func TestShutdownRouteRequestsServerStop(t *testing.T) {
	e := newEnv(t)
	st, out := e.post("/server/shutdown", map[string]any{"confirm": "shutdown"})
	if st != 202 {
		t.Fatalf("shutdown route: %d %v", st, out)
	}
	if out["status"] != "shutting_down" {
		t.Fatalf("shutdown response: %v", out)
	}
	select {
	case <-e.s.shutdownReq:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("shutdown request was not sent")
	}
}

func TestReplayRejected(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	cid, nonce := p.challenge()
	body := p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)
	if st, out := e.post("/attendance", body); st != 201 {
		t.Fatalf("first: %d %v", st, out)
	}
	st, out := e.post("/attendance", body)
	e.wantErr(st, out, "challenge_used", 409)
}

func TestSignatureCoversFields(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	cid, nonce := p.challenge()

	// Changed coordinates break the signature.
	body := p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)
	body["lat_micro"] = int64(geoLatMicro + 1)
	st, out := e.post("/attendance", body)
	e.wantErr(st, out, "bad_signature", 401)

	// A history signature cannot be used as an attendance signature.
	body = p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)
	body["signature"] = p.sign(historyMsg(cid, nonce, p.facultyID))
	st, out = e.post("/attendance", body)
	e.wantErr(st, out, "bad_signature", 401)

	// A different key cannot sign for this person.
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	imposter := &phone{e: e, key: other, facultyID: p.facultyID}
	st, out = e.post("/attendance", imposter.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500))
	e.wantErr(st, out, "bad_signature", 401)

	// None of those consumed the challenge, so the real phone can still use it.
	if st, out := e.post("/attendance", p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)); st != 201 {
		t.Fatalf("retry: %d %v", st, out)
	}
}

func TestChallengeProblems(t *testing.T) {
	e := newEnv(t)
	a := e.enroll("F001", "Dr. Rao")
	b := e.enroll("F002", "Dr. Iyer")

	// expired
	cid, nonce := a.challenge()
	e.setClock(friday10().Add(61 * time.Second))
	st, out := e.post("/attendance", a.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500))
	e.wantErr(st, out, "challenge_expired", 410)

	// wrong owner: b signs and submits a's challenge under b's id
	cid, nonce = a.challenge()
	body := b.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)
	st, out = e.post("/attendance", body)
	e.wantErr(st, out, "challenge_invalid", 400)

	// unknown challenge
	st, out = e.post("/attendance", a.attendBody("deadbeef", "x", geoLatMicro, geoLonMicro, 1500))
	e.wantErr(st, out, "challenge_invalid", 400)

	// unknown faculty
	st, out = e.post("/challenge", map[string]any{"faculty_id": "nobody"})
	e.wantErr(st, out, "not_enrolled", 403)
}

func TestTooManyChallenges(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	for i := 0; i < 5; i++ {
		p.challenge()
	}
	st, out := e.post("/challenge", map[string]any{"faculty_id": p.facultyID})
	e.wantErr(st, out, "too_many_challenges", 429)
	e.setClock(friday10().Add(61 * time.Second)) // old ones expire
	p.challenge()
}

func TestAlreadyMarkedKeepsChallengeUnused(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	if st, out := p.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatalf("first: %d %v", st, out)
	}
	cid, nonce := p.challenge()
	st, out := e.post("/attendance", p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500))
	e.wantErr(st, out, "already_marked", 409)

	var used int
	if err := e.s.db.QueryRow(`SELECT used FROM challenges WHERE id=?`, cid).Scan(&used); err != nil || used != 0 {
		t.Fatalf("challenge should stay unused, used=%d err=%v", used, err)
	}
	if e.countAttendance() != 1 {
		t.Fatal("expected exactly one record")
	}
}

func TestPolicy(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")

	// outside the area, then retry on the same challenge after "moving into the building"
	cid, nonce := p.challenge()
	st, out := e.post("/attendance", p.attendBody(cid, nonce, farLatMicro, geoLonMicro, 1500))
	e.wantErr(st, out, "outside_area", 422)
	if st, out := e.post("/attendance", p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)); st != 201 {
		t.Fatalf("retry inside: %d %v", st, out)
	}

	q := e.enroll("F002", "Dr. Iyer")
	st, out = q.attend(geoLatMicro, geoLonMicro, 6000) // 60 m > 50 m limit
	e.wantErr(st, out, "poor_accuracy", 422)

	e.setClock(time.Date(2026, 10, 23, 8, 59, 0, 0, ist)) // before the window
	st, out = q.attend(geoLatMicro, geoLonMicro, 1500)
	e.wantErr(st, out, "closed", 409)
	e.setClock(time.Date(2026, 10, 23, 18, 0, 0, 0, ist)) // end is exclusive
	st, out = q.attend(geoLatMicro, geoLonMicro, 1500)
	e.wantErr(st, out, "closed", 409)
	e.setClock(time.Date(2026, 10, 24, 10, 0, 0, 0, ist)) // Saturday
	st, out = q.attend(geoLatMicro, geoLonMicro, 1500)
	e.wantErr(st, out, "closed", 409)
	e.setClock(time.Date(2026, 10, 26, 10, 0, 0, 0, ist)) // Monday works
	if st, out = q.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatalf("monday: %d %v", st, out)
	}
}

func TestBadFieldRanges(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	for _, c := range [][3]int64{{91_000_000, 0, 0}, {0, 181_000_000, 0}, {0, 0, -1}} {
		cid, nonce := p.challenge()
		st, out := e.post("/attendance", p.attendBody(cid, nonce, c[0], c[1], c[2]))
		e.wantErr(st, out, "bad_request", 400)
	}
	// unknown fields and trailing junk are rejected
	st, out := e.post("/challenge", map[string]any{"faculty_id": p.facultyID, "extra": 1})
	e.wantErr(st, out, "bad_request", 400)
	resp, err := http.Post(e.ts.URL+"/challenge", "application/json", bytes.NewReader([]byte(`{"faculty_id":"x"} {}`)))
	if err != nil || resp.StatusCode != 400 {
		t.Fatalf("trailing data: %v %v", err, resp)
	}
}

func TestConcurrentSubmitsOneRecord(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	cid, nonce := p.challenge()
	body := p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500)

	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := json.Marshal(body)
			resp, err := http.Post(e.ts.URL+"/attendance", "application/json", bytes.NewReader(b))
			if err != nil {
				return
			}
			resp.Body.Close()
			if resp.StatusCode == 201 {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || e.countAttendance() != 1 {
		t.Fatalf("successes=%d records=%d, want 1 and 1", ok.Load(), e.countAttendance())
	}
}

func TestEnrollment(t *testing.T) {
	e := newEnv(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p := &phone{e: e, key: key}
	if _, err := addFaculty(e.s.db, "F001", "Dr. Rao"); err != nil {
		t.Fatal(err)
	}
	code, _ := issueCode(e.s.db, "F001", e.s.now())

	// non-P-256 and garbage keys are rejected without burning the code
	k384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k384.PublicKey)
	st, out := e.post("/register", map[string]any{"code": code, "public_key": base64.StdEncoding.EncodeToString(der)})
	e.wantErr(st, out, "bad_request", 400)
	st, out = e.post("/register", map[string]any{"code": code, "public_key": "not-a-key"})
	e.wantErr(st, out, "bad_request", 400)

	// unknown code
	st, out = e.post("/register", map[string]any{"code": "wrong", "public_key": p.pubB64()})
	e.wantErr(st, out, "code_invalid", 403)

	// works once
	if st, out := e.post("/register", map[string]any{"code": code, "public_key": p.pubB64()}); st != 201 || out["name"] != "Dr. Rao" {
		t.Fatalf("register: %d %v", st, out)
	}
	st, out = e.post("/register", map[string]any{"code": code, "public_key": p.pubB64()})
	e.wantErr(st, out, "code_invalid", 403)

	// expired code
	code2, _ := issueCode(e.s.db, "F001", e.s.now())
	e.setClock(friday10().Add(16 * time.Minute))
	st, out = e.post("/register", map[string]any{"code": code2, "public_key": p.pubB64()})
	e.wantErr(st, out, "code_invalid", 403)
}

func TestEnrollmentAcceptsUppercaseCode(t *testing.T) {
	e := newEnv(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p := &phone{e: e, key: key}
	if _, err := addFaculty(e.s.db, "F001", "Dr. Rao"); err != nil {
		t.Fatal(err)
	}
	code, _ := issueCode(e.s.db, "F001", e.s.now())
	if st, out := e.post("/register", map[string]any{"code": strings.ToUpper(code), "public_key": p.pubB64()}); st != 201 || out["name"] != "Dr. Rao" {
		t.Fatalf("register uppercase: %d %v", st, out)
	}
}

func TestReEnrollRetiresOldPhone(t *testing.T) {
	e := newEnv(t)
	old := e.enroll("F001", "Dr. Rao")

	newKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	fresh := &phone{e: e, key: newKey, facultyID: old.facultyID}
	code, _ := issueCode(e.s.db, "F001", e.s.now())
	if st, out := e.post("/register", map[string]any{"code": code, "public_key": fresh.pubB64()}); st != 201 {
		t.Fatalf("re-register: %d %v", st, out)
	}
	st, out := old.attend(geoLatMicro, geoLonMicro, 1500)
	e.wantErr(st, out, "bad_signature", 401)
	if st, out := fresh.attend(geoLatMicro, geoLonMicro, 1500); st != 201 {
		t.Fatalf("new phone: %d %v", st, out)
	}
}

func TestRevokedPhoneRejected(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	cid, nonce := p.challenge()
	hcid, hnonce := p.challenge()

	if err := revokeFaculty(e.s.db, "F001"); err != nil {
		t.Fatal(err)
	}
	st, out := e.post("/challenge", map[string]any{"faculty_id": p.facultyID})
	e.wantErr(st, out, "not_enrolled", 403)
	st, out = e.post("/attendance", p.attendBody(cid, nonce, geoLatMicro, geoLonMicro, 1500))
	e.wantErr(st, out, "not_enrolled", 403)
	st, out = e.post("/history", p.historyBody(hcid, hnonce))
	e.wantErr(st, out, "not_enrolled", 403)
}

func TestHistoryOwnDaysAndTwoMonths(t *testing.T) {
	e := newEnv(t)
	a := e.enroll("F001", "Dr. Rao")
	e.enroll("F002", "Dr. Iyer")
	now := e.s.now()
	for _, d := range []string{"2026-10-20", "2026-08-23", "2026-08-22", "2026-07-01"} {
		if err := markManual(e.s.db, "F001", d, "manual", now); err != nil {
			t.Fatal(err)
		}
	}
	if err := markManual(e.s.db, "F002", "2026-10-21", "", now); err != nil {
		t.Fatal(err)
	}
	st, out := a.history()
	if st != 200 || out["marked_today"] != false {
		t.Fatalf("history: %d %v", st, out)
	}
	got := asStrings(out["days"])
	want := []string{"2026-10-20", "2026-08-23"} // 2026-08-23 is exactly two months back, inclusive
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("days = %v, want %v", got, want)
	}
}

func TestHistoryChallengeSingleUse(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	cid, nonce := p.challenge()
	body := p.historyBody(cid, nonce)
	if st, out := e.post("/history", body); st != 200 {
		t.Fatalf("first: %d %v", st, out)
	}
	st, out := e.post("/history", body)
	e.wantErr(st, out, "challenge_used", 409)
}

func TestHistoryWindowFlag(t *testing.T) {
	e := newEnv(t)
	p := e.enroll("F001", "Dr. Rao")
	e.setClock(time.Date(2026, 10, 24, 10, 0, 0, 0, ist)) // Saturday
	st, out := p.history()
	if st != 200 || out["window_open"] != false {
		t.Fatalf("history: %d %v", st, out)
	}
}

func TestTwoMonthsBack(t *testing.T) {
	cases := map[string]string{
		"2026-10-23": "2026-08-23",
		"2026-04-30": "2026-02-28",
		"2026-01-15": "2025-11-15",
		"2028-04-30": "2028-02-29",
	}
	for in, want := range cases {
		tm, _ := time.ParseInLocation("2006-01-02", in, ist)
		if got := twoMonthsBack(tm).Format("2006-01-02"); got != want {
			t.Errorf("twoMonthsBack(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestHaversine(t *testing.T) {
	// One degree of latitude is about 111.19 km.
	if d := haversineM(0, 0, 1, 0); d < 111_000 || d > 111_400 {
		t.Fatalf("got %f", d)
	}
}
