package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
)

const maxDayList = 366

// parseOptRange reads optional from/to (inclusive, IST). Both empty means
// all time; giving only one is bad_range. No span cap: these are aggregates.
func parseOptRange(q url.Values) (from, to string, err error) {
	f, t := q.Get("from"), q.Get("to")
	if f == "" && t == "" {
		return "", "", nil
	}
	if f == "" || t == "" {
		return "", "", adminErr{code: "bad_range", msg: "give both from and to, or neither"}
	}
	fd, err := parseDay(f)
	if err != nil {
		return "", "", err
	}
	td, err := parseDay(t)
	if err != nil {
		return "", "", err
	}
	if fd.After(td) {
		return "", "", adminErr{code: "bad_range", msg: "from must not be after to"}
	}
	return ymd(fd), ymd(td), nil
}

func rangeBounds(from, to string) (string, string) {
	if from == "" {
		return "0000-01-01", "9999-12-31"
	}
	return from, to
}

func rangeJSON(from, to string) map[string]any {
	if from == "" {
		return map[string]any{"from": nil, "to": nil}
	}
	return map[string]any{"from": from, "to": to}
}

type facultyRow struct {
	EmployeeID    string  `json:"employee_id"`
	Name          string  `json:"name"`
	Enrolled      bool    `json:"enrolled"`
	DaysPresent   int64   `json:"days_present"`
	ViaPhone      int64   `json:"via_phone"`
	Manual        int64   `json:"manual"`
	LastMarkedDay *string `json:"last_marked_day"`
}

// GET /admin/api/faculty[?from=&to=]  every user with their attendance total.
func (s *Server) adminFacultyList(w http.ResponseWriter, r *http.Request) (int, any, error) {
	from, to, err := parseOptRange(r.URL.Query())
	if err != nil {
		return 0, nil, err
	}
	lo, hi := rangeBounds(from, to)
	items, err := s.queryFacultyTotals(r.Context(), lo, hi)
	if err != nil {
		return 0, nil, err
	}
	return 200, map[string]any{"range": rangeJSON(from, to), "total": len(items), "faculty": items}, nil
}

func (s *Server) queryFacultyTotals(ctx context.Context, lo, hi string) ([]facultyRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.employee_id, f.name, f.public_key IS NOT NULL,
			COUNT(a.day),
			COALESCE(SUM(CASE WHEN a.day IS NOT NULL AND a.lat_micro IS NULL THEN 1 ELSE 0 END), 0),
			MAX(a.day)
		FROM faculty f LEFT JOIN attendance a ON a.faculty_id = f.id AND a.day >= ? AND a.day <= ?
		GROUP BY f.id ORDER BY f.employee_id`, lo, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []facultyRow{}
	for rows.Next() {
		var it facultyRow
		var last sql.NullString
		if err := rows.Scan(&it.EmployeeID, &it.Name, &it.Enrolled, &it.DaysPresent, &it.Manual, &last); err != nil {
			return nil, err
		}
		it.ViaPhone = it.DaysPresent - it.Manual
		it.LastMarkedDay = strPtr(last)
		items = append(items, it)
	}
	return items, rows.Err()
}

type facultyDay struct {
	Day         string  `json:"day"`
	MarkedAtIST string  `json:"marked_at_ist"`
	Source      string  `json:"source"`
	DistanceM   *int64  `json:"distance_m"`
	Note        string  `json:"note"`
}

// GET /admin/api/faculty/{employee_id}/attendance[?from=&to=]
// Total attendance of one user, plus their marked days (newest first, capped).
func (s *Server) adminFacultyAttendance(w http.ResponseWriter, r *http.Request) (int, any, error) {
	emp := r.PathValue("employee_id")
	from, to, err := parseOptRange(r.URL.Query())
	if err != nil {
		return 0, nil, err
	}
	lo, hi := rangeBounds(from, to)
	ctx := r.Context()

	var id, name string
	var enrolled bool
	err = s.db.QueryRowContext(ctx, `SELECT id, name, public_key IS NOT NULL FROM faculty WHERE employee_id = ?`, emp).
		Scan(&id, &name, &enrolled)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, adminErr{code: "not_found", msg: "no such employee"}
	}
	if err != nil {
		return 0, nil, err
	}

	var present, manual int64
	var first, last sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN lat_micro IS NULL THEN 1 ELSE 0 END), 0), MIN(day), MAX(day)
		FROM attendance WHERE faculty_id = ? AND day >= ? AND day <= ?`, id, lo, hi).
		Scan(&present, &manual, &first, &last); err != nil {
		return 0, nil, err
	}
	days, err := s.queryFacultyDays(ctx, id, lo, hi)
	if err != nil {
		return 0, nil, err
	}
	truncated := len(days) > maxDayList
	if truncated {
		days = days[:maxDayList]
	}
	return 200, map[string]any{
		"employee":         map[string]any{"employee_id": emp, "name": name, "enrolled": enrolled},
		"range":            rangeJSON(from, to),
		"days_present":     present,
		"via_phone":        present - manual,
		"manual":           manual,
		"first_marked_day": strPtr(first),
		"last_marked_day":  strPtr(last),
		"days":             days,
		"days_truncated":   truncated,
	}, nil
}

func (s *Server) queryFacultyDays(ctx context.Context, id, lo, hi string) ([]facultyDay, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, marked_at, lat_micro IS NULL, distance_m, note
		FROM attendance WHERE faculty_id = ? AND day >= ? AND day <= ? ORDER BY day DESC LIMIT ?`,
		id, lo, hi, maxDayList+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := []facultyDay{}
	for rows.Next() {
		var d facultyDay
		var markedAt int64
		var manual bool
		var dist sql.NullInt64
		if err := rows.Scan(&d.Day, &markedAt, &manual, &dist, &d.Note); err != nil {
			return nil, err
		}
		d.MarkedAtIST = fmtIST(markedAt)
		d.Source = "phone"
		if manual {
			d.Source = "manual"
		}
		if dist.Valid {
			v := dist.Int64
			d.DistanceM = &v
		}
		days = append(days, d)
	}
	return days, rows.Err()
}
