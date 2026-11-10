package main

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

type dayItem struct {
	EmployeeID  string  `json:"employee_id"`
	Name        string  `json:"name"`
	Enrolled    bool    `json:"enrolled"`
	Marked      bool    `json:"marked"`
	MarkedAtIST *string `json:"marked_at_ist"`
	DistanceM   *int64  `json:"distance_m"`
	Source      *string `json:"source"` // "phone" | "manual" | null (not marked)
	Note        string  `json:"note"`

	// Additive: nil unless this faculty member has a pending early-exit request
	// for the day. Approved requests are never reported here.
	PendingExitRequest *pendingExit `json:"pending_exit_request"`
}

type pendingExit struct {
	ID             string `json:"id"`
	RequestedAtIST string `json:"requested_at_ist"`
	Status         string `json:"status"`
}

// GET /admin/api/attendance/day?date=YYYY-MM-DD   (default: today, IST)
// Returns every faculty member so "absent" is explicit.
func (s *Server) adminDay(w http.ResponseWriter, r *http.Request) (int, any, error) {
	today := todayIST(s.now())
	day := today
	if v := r.URL.Query().Get("date"); v != "" {
		d, err := parseDay(v)
		if err != nil {
			return 0, nil, err
		}
		day = d
	}
	items, err := s.queryDay(r.Context(), ymd(day))
	if err != nil {
		return 0, nil, err
	}
	var marked, notEnrolled int
	for _, it := range items {
		if it.Marked {
			marked++
		}
		if !it.Enrolled {
			notEnrolled++
		}
	}
	wd := day.Weekday()
	// Pending requests never expire: on the today view, also report ones left
	// over from earlier days so the admin can still act on them.
	stale := []staleExit{}
	if day.Equal(today) {
		if stale, err = s.queryStaleExit(r.Context(), ymd(day)); err != nil {
			return 0, nil, err
		}
	}
	return 200, map[string]any{
		"stale_exit_requests": stale,
		"date":                ymd(day),
		"is_today":            day.Equal(today),
		"is_weekend":          wd == time.Saturday || wd == time.Sunday,
		"is_future":           day.After(today),
		"summary": map[string]int{
			"total": len(items), "marked": marked,
			"not_marked": len(items) - marked, "not_enrolled": notEnrolled,
		},
		"faculty": items,
	}, nil
}

// queryDay scans everything and closes the cursor before returning (D6: the
// single DB connection must never be held while talking to a client).
func (s *Server) queryDay(ctx context.Context, day string) ([]dayItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.employee_id, f.name, f.public_key IS NOT NULL,
			a.faculty_id IS NOT NULL, a.marked_at, a.distance_m, a.lat_micro IS NULL, a.note
		FROM faculty f LEFT JOIN attendance a ON a.faculty_id = f.id AND a.day = ?
		ORDER BY f.employee_id`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []dayItem{}
	for rows.Next() {
		var it dayItem
		var markedAt, dist sql.NullInt64
		var manual bool
		var note sql.NullString
		if err := rows.Scan(&it.EmployeeID, &it.Name, &it.Enrolled, &it.Marked, &markedAt, &dist, &manual, &note); err != nil {
			return nil, err
		}
		if it.Marked {
			if markedAt.Valid {
				v := fmtIST(markedAt.Int64)
				it.MarkedAtIST = &v
			}
			src := "phone"
			if manual {
				src = "manual"
			}
			it.Source = &src
		}
		if dist.Valid {
			v := dist.Int64
			it.DistanceM = &v
		}
		it.Note = note.String
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // single connection: free it before the batched lookup below

	// One batched query for the whole day (no per-row lookups).
	pend, err := s.db.QueryContext(ctx, `SELECT f.employee_id, r.id, r.requested_at
		FROM exit_requests r JOIN faculty f ON f.id = r.faculty_id
		WHERE r.day = ? AND r.status = 'pending'`, day)
	if err != nil {
		return nil, err
	}
	defer pend.Close()
	byEmp := map[string]*pendingExit{}
	for pend.Next() {
		var emp, id string
		var at int64
		if err := pend.Scan(&emp, &id, &at); err != nil {
			return nil, err
		}
		byEmp[emp] = &pendingExit{ID: id, RequestedAtIST: fmtIST(at), Status: "pending"}
	}
	if err := pend.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].PendingExitRequest = byEmp[items[i].EmployeeID]
	}
	return items, nil
}

type staleExit struct {
	ID             string `json:"id"`
	EmployeeID     string `json:"employee_id"`
	Name           string `json:"name"`
	Day            string `json:"day"`
	RequestedAtIST string `json:"requested_at_ist"`
	Status         string `json:"status"`
}

// queryStaleExit lists pending requests from days before `today`.
func (s *Server) queryStaleExit(ctx context.Context, today string) ([]staleExit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, f.employee_id, f.name, r.day, r.requested_at
		FROM exit_requests r JOIN faculty f ON f.id = r.faculty_id
		WHERE r.status = 'pending' AND r.day < ? ORDER BY r.requested_at, r.id`, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []staleExit{}
	for rows.Next() {
		var it staleExit
		var at int64
		if err := rows.Scan(&it.ID, &it.EmployeeID, &it.Name, &it.Day, &at); err != nil {
			return nil, err
		}
		it.RequestedAtIST, it.Status = fmtIST(at), "pending"
		out = append(out, it)
	}
	return out, rows.Err()
}
