package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
)

// csvSafe neutralises spreadsheet formula injection: a text cell starting with
// = + - @ (or tab/CR) is prefixed with an apostrophe. Numbers are not passed here.
func csvSafe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@' || s[0] == '\t' || s[0] == '\r') {
		return "'" + s
	}
	return s
}

type exportRow struct {
	emp, name, day, note     string
	markedAt                 int64
	lat, lon, acc, dist      sql.NullInt64
}

// GET /admin/api/export/attendance.csv?from=&to=[&include_location=1][&bom=0]
func (s *Server) adminExport(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	if q.Get("from") == "" || q.Get("to") == "" {
		return adminErr{code: "bad_range", msg: "from and to are both required (YYYY-MM-DD)"}
	}
	from, err := parseDay(q.Get("from"))
	if err != nil {
		return err
	}
	to, err := parseDay(q.Get("to"))
	if err != nil {
		return err
	}
	if err := checkSpan(from, to); err != nil {
		return err
	}
	withLoc := q.Get("include_location") == "1"
	bom := q.Get("bom") != "0"

	// D6: read everything and release the single DB connection BEFORE writing.
	data, err := s.queryExport(r, ymd(from), ymd(to))
	if err != nil {
		return err
	}

	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="attendance_%s_to_%s.csv"`, ymd(from), ymd(to)))
	if bom {
		w.Write([]byte{0xEF, 0xBB, 0xBF}) // so Excel reads UTF-8 names correctly
	}
	cw := csv.NewWriter(w)
	head := []string{"employee_id", "name", "day", "marked_at_ist", "source", "distance_m", "note"}
	if withLoc {
		head = append(head, "lat", "lon", "accuracy_m")
	}
	cw.Write(head)
	for _, x := range data {
		source, dist := "phone", ""
		if !x.lat.Valid {
			source = "manual"
		}
		if x.dist.Valid {
			dist = strconv.FormatInt(x.dist.Int64, 10)
		}
		rec := []string{csvSafe(x.emp), csvSafe(x.name), x.day, fmtIST(x.markedAt), source, dist, csvSafe(x.note)}
		if withLoc {
			lat, lon, acc := "", "", ""
			if x.lat.Valid && x.lon.Valid {
				lat = strconv.FormatFloat(float64(x.lat.Int64)/1e6, 'f', 6, 64)
				lon = strconv.FormatFloat(float64(x.lon.Int64)/1e6, 'f', 6, 64)
			}
			if x.acc.Valid {
				acc = strconv.FormatFloat(float64(x.acc.Int64)/100, 'f', 2, 64)
			}
			rec = append(rec, lat, lon, acc)
		}
		cw.Write(rec)
	}
	cw.Flush()
	return nil // headers are already sent; a write error here only means the client left
}

func (s *Server) queryExport(r *http.Request, from, to string) ([]exportRow, error) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT f.employee_id, f.name, a.day, a.marked_at,
			a.lat_micro, a.lon_micro, a.accuracy_cm, a.distance_m, a.note
		FROM attendance a JOIN faculty f ON f.id = a.faculty_id
		WHERE a.day >= ? AND a.day <= ? ORDER BY a.day, f.employee_id`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []exportRow
	for rows.Next() {
		var x exportRow
		if err := rows.Scan(&x.emp, &x.name, &x.day, &x.markedAt, &x.lat, &x.lon, &x.acc, &x.dist, &x.note); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
