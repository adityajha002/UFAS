package main

// Early-exit requests, admin side. Requests are created by the phone-facing
// server (POST /attendance/request-out); the admin server only lists them and
// approves them. Approval is one transaction: the request flips to 'approved',
// attendance_state goes OUT with the session added to today_worked_s, and the
// daily attendance row gets the OUT time. Nothing here ever deletes a request.

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

// outTimeFor is the instant stamped as OUT. For a request made today it is the
// approval time. A request from an earlier IST day (they never expire) is
// stamped at the end of that day, so an approval days later cannot credit days
// of phantom work time.
func outTimeFor(day string, approvedAt int64, now time.Time) int64 {
	if day >= ymd(todayIST(now)) {
		return approvedAt
	}
	d, err := time.ParseInLocation(dayLayout, day, ist)
	if err != nil {
		return approvedAt
	}
	end := d.Add(24*time.Hour - time.Second).Unix()
	if end > approvedAt {
		return approvedAt
	}
	return end
}

type exitRequestItem struct {
	ID             string  `json:"id"`
	EmployeeID     string  `json:"employee_id"`
	Name           string  `json:"name"`
	Day            string  `json:"day"`
	RequestedAt    int64   `json:"requested_at"`
	RequestedAtIST string  `json:"requested_at_ist"`
	LatMicro       int64   `json:"lat_micro"`
	LonMicro       int64   `json:"lon_micro"`
	AccuracyCM     int64   `json:"accuracy_cm"`
	DistanceM      int64   `json:"distance_m"`
	Status         string  `json:"status"`
	ApprovedAtIST  *string `json:"approved_at_ist"`
	ApprovedBy     *string `json:"approved_by"`
	IsIn           *bool   `json:"is_in,omitempty"` // detail only: current attendance state
}

const exitRequestCols = `r.id, f.employee_id, f.name, r.day, r.requested_at, r.lat_micro, r.lon_micro,
	r.accuracy_cm, r.distance_m, r.status, r.approved_at, r.approved_by`

func scanExitRequest(sc interface{ Scan(...any) error }) (exitRequestItem, error) {
	var it exitRequestItem
	var appAt sql.NullInt64
	var appBy sql.NullString
	if err := sc.Scan(&it.ID, &it.EmployeeID, &it.Name, &it.Day, &it.RequestedAt, &it.LatMicro,
		&it.LonMicro, &it.AccuracyCM, &it.DistanceM, &it.Status, &appAt, &appBy); err != nil {
		return it, err
	}
	it.RequestedAtIST = fmtIST(it.RequestedAt)
	if appAt.Valid {
		v := fmtIST(appAt.Int64)
		it.ApprovedAtIST = &v
	}
	it.ApprovedBy = strPtr(appBy)
	return it, nil
}

// GET /admin/api/exit-requests?date=YYYY-MM-DD&status=pending|approved|all
// status defaults to pending. date is optional: without it every day is listed,
// so a pending request from an earlier day is never hidden (they do not expire).
func (s *Server) adminExitRequestList(w http.ResponseWriter, r *http.Request) (int, any, error) {
	q := r.URL.Query()
	status := q.Get("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "all" {
		return 0, nil, apiErr{"bad_request"}
	}
	var where []string
	var args []any
	if v := q.Get("date"); v != "" {
		d, err := parseDay(v)
		if err != nil {
			return 0, nil, err
		}
		where, args = append(where, "r.day = ?"), append(args, ymd(d))
	}
	if status != "all" {
		where, args = append(where, "r.status = ?"), append(args, status)
	}
	query := `SELECT ` + exitRequestCols + ` FROM exit_requests r JOIN faculty f ON f.id = r.faculty_id`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY r.requested_at, r.id"
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []exitRequestItem{}
	for rows.Next() {
		it, err := scanExitRequest(rows)
		if err != nil {
			return 0, nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	return 200, map[string]any{"items": items}, nil
}

// GET /admin/api/exit-requests/{request_id}
func (s *Server) adminExitRequestOne(w http.ResponseWriter, r *http.Request) (int, any, error) {
	id := r.PathValue("request_id")
	it, err := scanExitRequest(s.db.QueryRowContext(r.Context(),
		`SELECT `+exitRequestCols+` FROM exit_requests r JOIN faculty f ON f.id = r.faculty_id WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, adminErr{code: "not_found", msg: "no such exit request"}
	}
	if err != nil {
		return 0, nil, err
	}
	var isIn sql.NullBool
	err = s.db.QueryRowContext(r.Context(),
		`SELECT s.is_in <> 0 FROM attendance_state s JOIN exit_requests r ON r.faculty_id = s.faculty_id AND r.day = s.day WHERE r.id = ?`,
		id).Scan(&isIn)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, nil, err
	}
	if isIn.Valid {
		v := isIn.Bool
		it.IsIn = &v
	}
	return 200, it, nil
}

// POST /admin/api/exit-requests/{request_id}/approve   (body ignored)
//
// The guarded UPDATE runs first so the transaction takes SQLite's write lock
// before it reads anything: two concurrent approvals (two admin sessions, or the
// phone server writing at the same moment) serialise, and exactly one of them
// finds the row still 'pending'. Any later failure returns an error, which
// withTx turns into a rollback, undoing that UPDATE too.
func (s *Server) adminExitRequestApprove(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	id := r.PathValue("request_id")
	target := id
	var detail any
	defer s.trackAction(r, "exit_request_approve", &target, &detail, &err)()

	now := s.now()
	approvedAt := now.Unix()
	approvedBy := "admin@" + remoteIP(r) // the panel uses one shared token; there is no per-admin identity
	var emp string
	var workedAdded, workedTotal int64
	var outAt int64
	var reqDay string

	terr := withTx(r.Context(), s.db, func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE exit_requests SET status = 'approved', approved_at = ?, approved_by = ?, attendance_day = day
			WHERE id = ? AND status = 'pending'`, approvedAt, approvedBy, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			var st string
			switch err := tx.QueryRow(`SELECT status FROM exit_requests WHERE id = ?`, id).Scan(&st); {
			case errors.Is(err, sql.ErrNoRows):
				return adminErr{code: "not_found", msg: "no such exit request"}
			case err != nil:
				return err
			}
			return adminErr{code: "already_approved"} // never adds the session again
		}

		var facultyID, day string
		var lat, lon, acc, dist int64
		if err := tx.QueryRow(`SELECT r.faculty_id, r.day, r.lat_micro, r.lon_micro, r.accuracy_cm, r.distance_m, f.employee_id
			FROM exit_requests r JOIN faculty f ON f.id = r.faculty_id WHERE r.id = ?`, id).
			Scan(&facultyID, &day, &lat, &lon, &acc, &dist, &emp); err != nil {
			return err
		}
		target = emp

		var isIn int64
		var startedAt sql.NullInt64
		var worked int64
		err = tx.QueryRow(`SELECT is_in, started_at, today_worked_s FROM attendance_state WHERE faculty_id = ? AND day = ?`, facultyID, day).
			Scan(&isIn, &startedAt, &worked)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (isIn == 0 || !startedAt.Valid || startedAt.Int64 <= 0)) {
			return adminErr{code: "not_in", msg: "faculty member is no longer IN; the request was not approved"}
		}
		if err != nil {
			return err
		}
		// OUT is stamped with the approval time, not the request time (or the
		// end of the request day when that day is already over).
		reqDay = day
		outAt = outTimeFor(day, approvedAt, now)
		dur := outAt - startedAt.Int64
		if dur < 0 {
			dur = 0
		}
		workedAdded, workedTotal = dur, worked+dur
		if _, err := tx.Exec(`UPDATE attendance_state SET is_in = 0, started_at = NULL, today_worked_s = ?, updated_at = ?
			WHERE faculty_id = ? AND day = ?`, workedTotal, outAt, facultyID, day); err != nil {
			return err
		}
		// Same upsert a normal phone OUT performs (handlers.go in the phone server):
		// the daily row becomes the latest mark, with the location that was verified.
		if _, err := tx.Exec(`INSERT INTO attendance (faculty_id, day, marked_at, lat_micro, lon_micro, accuracy_cm, distance_m)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(faculty_id, day) DO UPDATE SET marked_at = excluded.marked_at, lat_micro = excluded.lat_micro,
				lon_micro = excluded.lon_micro, accuracy_cm = excluded.accuracy_cm, distance_m = excluded.distance_m`,
			facultyID, day, outAt, lat, lon, acc, dist); err != nil {
			return err
		}
		return nil
	})
	if terr != nil {
		return 0, nil, terr
	}
	detail = map[string]any{"request_id": id, "employee_id": emp, "session_s": workedAdded,
		"request_day": reqDay, "out_at_ist": fmtIST(outAt)}
	return 200, map[string]any{
		"id":              id,
		"employee_id":     emp,
		"status":          "approved",
		"approved_at_ist": fmtIST(approvedAt),
		"out_at_ist":      fmtIST(outAt),
		"session_s":       workedAdded,
		"today_worked_s":  workedTotal,
		"is_in":           false,
	}, nil
}
