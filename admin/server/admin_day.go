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
	return 200, map[string]any{
		"date":       ymd(day),
		"is_today":   day.Equal(today),
		"is_weekend": wd == time.Saturday || wd == time.Sunday,
		"is_future":  day.After(today),
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
	return items, rows.Err()
}
