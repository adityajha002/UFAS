package main

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// seedExitRequest creates a faculty row that is IN since startedAt (10:00 - inFor
// seconds) with 100 s already worked, and a pending request for today.
func seedExitRequest(t *testing.T, e *env, emp string, inFor int64) (facultyID, reqID string) {
	t.Helper()
	facultyID, err := addFaculty(e.s.db, emp, "Name "+emp)
	if err != nil {
		t.Fatal(err)
	}
	now := e.s.now().Unix()
	day := ymd(todayIST(e.s.now()))
	reqID = "req-" + emp
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO attendance_state (faculty_id, day, is_in, started_at, today_worked_s, updated_at) VALUES (?, ?, 1, ?, 100, ?)`,
			[]any{facultyID, day, now - inFor, now - inFor}},
		{`INSERT INTO attendance (faculty_id, day, marked_at, lat_micro, lon_micro, accuracy_cm, distance_m) VALUES (?, ?, ?, 1, 2, 3, 4)`,
			[]any{facultyID, day, now - inFor}},
		{`INSERT INTO exit_requests (id, faculty_id, day, requested_at, lat_micro, lon_micro, accuracy_cm, distance_m, challenge_id)
			VALUES (?, ?, ?, ?, 28613900, 77209000, 1200, 25, 'ch-1')`,
			[]any{reqID, facultyID, day, now - 60}},
	} {
		if _, err := e.s.db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	return facultyID, reqID
}

func (e *env) stateOf(facultyID string) (isIn int, started *int64, worked int64) {
	e.t.Helper()
	var st *int64
	if err := e.s.db.QueryRow(`SELECT is_in, started_at, today_worked_s FROM attendance_state WHERE faculty_id=? AND day=?`, facultyID, ymd(todayIST(e.s.now()))).
		Scan(&isIn, &st, &worked); err != nil {
		e.t.Fatal(err)
	}
	return isIn, st, worked
}

func TestExitRequestRequiresAdminToken(t *testing.T) {
	e, ac := newAdminEnv(t)
	_, id := seedExitRequest(t, e, "E1", 7200)
	ac.token = ""
	ac.wantErr("GET", "/admin/api/exit-requests", "", 401, "unauthorized")
	ac.wantErr("GET", "/admin/api/exit-requests/"+id, "", 401, "unauthorized")
	ac.wantErr("POST", "/admin/api/exit-requests/"+id+"/approve", "", 401, "unauthorized")
	if n := e.dbCount(`SELECT COUNT(*) FROM exit_requests WHERE status='pending'`); n != 1 {
		t.Fatalf("request must stay pending, got %d pending", n)
	}
}

func TestExitRequestListAndDetail(t *testing.T) {
	e, ac := newAdminEnv(t)
	_, id := seedExitRequest(t, e, "E1", 7200)

	st, out := ac.call("GET", "/admin/api/exit-requests?status=pending&date="+ymd(todayIST(e.s.now())), "")
	items := asList(out["items"])
	if st != 200 || len(items) != 1 || asMap(items[0])["id"] != id || asMap(items[0])["employee_id"] != "E1" {
		t.Fatalf("list: %d %v", st, out)
	}
	if asMap(items[0])["requested_at_ist"] == "" || asMap(items[0])["distance_m"].(float64) != 25 {
		t.Fatalf("list lacks request evidence: %v", items[0])
	}
	ac.wantErr("GET", "/admin/api/exit-requests?status=bogus", "", 400, "bad_request")
	ac.wantErr("GET", "/admin/api/exit-requests?date=2026-13-40", "", 400, "bad_date")

	st, out = ac.call("GET", "/admin/api/exit-requests/"+id, "")
	if st != 200 || out["status"] != "pending" || out["is_in"] != true {
		t.Fatalf("detail: %d %v", st, out)
	}
	ac.wantErr("GET", "/admin/api/exit-requests/nope", "", 404, "not_found")
}

func TestDayAPIPendingExitRequest(t *testing.T) {
	e, ac := newAdminEnv(t)
	_, id := seedExitRequest(t, e, "E1", 7200)
	addFaculty(e.s.db, "E2", "Plain")

	find := func(out map[string]any, emp string) map[string]any {
		for _, v := range asList(out["faculty"]) {
			if asMap(v)["employee_id"] == emp {
				return asMap(v)
			}
		}
		t.Fatalf("%s missing in %v", emp, out)
		return nil
	}
	st, out := ac.call("GET", "/admin/api/attendance/day", "")
	if st != 200 {
		t.Fatalf("day: %d %v", st, out)
	}
	p := asMap(find(out, "E1")["pending_exit_request"])
	if p["id"] != id || p["status"] != "pending" || p["requested_at_ist"] == "" {
		t.Fatalf("pending flag missing: %v", p)
	}
	if find(out, "E2")["pending_exit_request"] != nil {
		t.Fatal("row without a request must have null pending_exit_request")
	}

	if st, out := ac.call("POST", "/admin/api/exit-requests/"+id+"/approve", ""); st != 200 {
		t.Fatalf("approve: %d %v", st, out)
	}
	_, out = ac.call("GET", "/admin/api/attendance/day", "")
	if find(out, "E1")["pending_exit_request"] != nil {
		t.Fatal("approved request must no longer be pending in the day API")
	}
}

func TestApproveExitRequest(t *testing.T) {
	e, ac := newAdminEnv(t)
	fid, id := seedExitRequest(t, e, "E1", 7200) // IN for 2 h, 100 s already worked

	st, out := ac.call("POST", "/admin/api/exit-requests/"+id+"/approve", "")
	if st != 200 || out["status"] != "approved" || out["session_s"].(float64) != 7200 || out["today_worked_s"].(float64) != 7300 {
		t.Fatalf("approve: %d %v", st, out)
	}
	isIn, started, worked := e.stateOf(fid)
	if isIn != 0 || started != nil || worked != 7300 {
		t.Fatalf("state after approve: in=%d started=%v worked=%d", isIn, started, worked)
	}
	// OUT is stamped with the approval time, not the request time.
	if n := e.dbCount(`SELECT COUNT(*) FROM attendance WHERE faculty_id=? AND marked_at=?`, fid, e.s.now().Unix()); n != 1 {
		t.Fatal("attendance row must carry the approval time")
	}
	if n := e.dbCount(`SELECT COUNT(*) FROM exit_requests WHERE id=? AND status='approved' AND approved_at=? AND approved_by IS NOT NULL`,
		id, e.s.now().Unix()); n != 1 {
		t.Fatal("request must be retained as approved with approval metadata")
	}
	if n := e.dbCount(`SELECT COUNT(*) FROM admin_actions WHERE action='exit_request_approve' AND outcome='ok' AND target='E1'`); n != 1 {
		t.Fatalf("approval must be in the admin action trail, got %d", n)
	}

	// Second approval: conflict, nothing added again.
	ac.wantErr("POST", "/admin/api/exit-requests/"+id+"/approve", "", http.StatusConflict, "already_approved")
	if _, _, w := e.stateOf(fid); w != 7300 {
		t.Fatalf("second approval changed worked time to %d", w)
	}
	if n := e.dbCount(`SELECT COUNT(*) FROM admin_actions WHERE action='exit_request_approve' AND outcome='already_approved'`); n != 1 {
		t.Fatal("failed approval must be tracked too")
	}

	// Approved requests stay queryable but are not in the pending list.
	_, out = ac.call("GET", "/admin/api/exit-requests", "")
	if len(asList(out["items"])) != 0 {
		t.Fatalf("approved request listed as pending: %v", out)
	}
	_, out = ac.call("GET", "/admin/api/exit-requests?status=approved", "")
	if len(asList(out["items"])) != 1 {
		t.Fatalf("approved request not queryable: %v", out)
	}
	ac.wantErr("POST", "/admin/api/exit-requests/nope/approve", "", 404, "not_found")
}

func TestApproveRollsBackWhenNotIn(t *testing.T) {
	e, ac := newAdminEnv(t)
	fid, id := seedExitRequest(t, e, "E1", 7200)
	if _, err := e.s.db.Exec(`UPDATE attendance_state SET is_in=0, started_at=NULL WHERE faculty_id=?`, fid); err != nil {
		t.Fatal(err)
	}
	ac.wantErr("POST", "/admin/api/exit-requests/"+id+"/approve", "", http.StatusConflict, "not_in")
	if n := e.dbCount(`SELECT COUNT(*) FROM exit_requests WHERE id=? AND status='pending' AND approved_at IS NULL`, id); n != 1 {
		t.Fatal("request must stay pending when the faculty member is not IN (rollback)")
	}
	if _, _, w := e.stateOf(fid); w != 100 {
		t.Fatalf("worked time changed to %d", w)
	}
}

func TestConcurrentApprovalsApplyOnce(t *testing.T) {
	e, ac := newAdminEnv(t)
	fid, id := seedExitRequest(t, e, "E1", 7200)

	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, _ := http.NewRequest("POST", ac.url+"/admin/api/exit-requests/"+id+"/approve", nil)
			req.Header.Set("Authorization", "Bearer "+ac.token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				codes[i] = -1
				return
			}
			resp.Body.Close()
			codes[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 200 {
			ok++
		} else if c != 409 {
			t.Fatalf("unexpected status in %v", codes)
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one approval must win, got %d (%s)", ok, fmt.Sprint(codes))
	}
	if _, _, w := e.stateOf(fid); w != 7300 {
		t.Fatalf("session added more than once: worked=%d", w)
	}
}

func TestApproveStaleDayStampsEndOfRequestDay(t *testing.T) {
	e, ac := newAdminEnv(t)
	fid, id := seedExitRequest(t, e, "E1", 3600) // IN for 1 h, 100 s already worked
	reqDay := ymd(todayIST(e.s.now()))
	started := e.s.now().Unix() - 3600
	e.setClock(e.s.now().Add(48 * time.Hour)) // two days later, request still pending

	// Today's view lists the leftover request, other days' rows do not carry it.
	_, out := ac.call("GET", "/admin/api/attendance/day", "")
	stale := asList(out["stale_exit_requests"])
	if len(stale) != 1 || asMap(stale[0])["id"] != id || asMap(stale[0])["day"] != reqDay {
		t.Fatalf("stale request not reported: %v", out["stale_exit_requests"])
	}

	st, out := ac.call("POST", "/admin/api/exit-requests/"+id+"/approve", "")
	if st != 200 {
		t.Fatalf("approve: %d %v", st, out)
	}
	d, _ := time.ParseInLocation(dayLayout, reqDay, ist)
	end := d.Add(24*time.Hour - time.Second).Unix()
	if got := int64(out["session_s"].(float64)); got != end-started {
		t.Fatalf("session_s=%d want %d (end of request day, not approval time)", got, end-started)
	}
	if n := e.dbCount(`SELECT COUNT(*) FROM attendance WHERE faculty_id=? AND day=? AND marked_at=?`, fid, reqDay, end); n != 1 {
		t.Fatal("attendance row must carry the end-of-day OUT time")
	}
	if n := e.dbCount(`SELECT COUNT(*) FROM exit_requests WHERE id=? AND status='approved' AND approved_at=?`, id, e.s.now().Unix()); n != 1 {
		t.Fatal("approved_at must still be the real approval time")
	}
	_, out = ac.call("GET", "/admin/api/attendance/day", "")
	if len(asList(out["stale_exit_requests"])) != 0 {
		t.Fatal("approved request must leave the stale list")
	}
}
