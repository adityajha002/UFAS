package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type auditFaculty struct {
	ID         string  `json:"id"` // as claimed by the caller; may not exist
	EmployeeID *string `json:"employee_id"`
	Name       *string `json:"name"`
}

type auditItem struct {
	ID          int64           `json:"id"`
	TS          int64           `json:"ts"`
	AtIST       string          `json:"at_ist"`
	Endpoint    string          `json:"endpoint"`
	Outcome     string          `json:"outcome"`
	HTTPStatus  int             `json:"http_status"`
	Faculty     *auditFaculty   `json:"faculty"`
	ChallengeID *string         `json:"challenge_id"`
	Lat         *float64        `json:"lat"`
	Lon         *float64        `json:"lon"`
	AccuracyM   *float64        `json:"accuracy_m"`
	DistanceM   *int64          `json:"distance_m"`
	RemoteIP    *string         `json:"remote_ip"`
	UserAgent   *string         `json:"user_agent"`
	DurationMS  *int64          `json:"duration_ms"`
	Policy      json.RawMessage `json:"policy"`
}

const auditSelect = `SELECT a.id, a.ts, a.endpoint, a.outcome, a.status, a.faculty_id, f.employee_id, f.name,
	a.challenge_id, a.lat_micro, a.lon_micro, a.accuracy_cm, a.distance_m, a.remote_ip, a.user_agent,
	a.duration_ms, a.policy
	FROM audit_log a LEFT JOIN faculty f ON f.id = a.faculty_id`

func strPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func microPtr(n sql.NullInt64) *float64 {
	if !n.Valid {
		return nil
	}
	v := float64(n.Int64) / 1e6
	return &v
}

// queryAudit runs auditSelect with allow-listed conditions (all values bound),
// newest first, and closes the cursor before returning.
func (s *Server) queryAudit(ctx context.Context, where []string, args []any, limit int64) ([]auditItem, error) {
	q := auditSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY a.id DESC LIMIT ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []auditItem{}
	for rows.Next() {
		var it auditItem
		var facID, empID, name, chal, ip, ua, policy sql.NullString
		var lat, lon, acc, dist, dur sql.NullInt64
		if err := rows.Scan(&it.ID, &it.TS, &it.Endpoint, &it.Outcome, &it.HTTPStatus, &facID, &empID, &name,
			&chal, &lat, &lon, &acc, &dist, &ip, &ua, &dur, &policy); err != nil {
			return nil, err
		}
		it.AtIST = fmtIST(it.TS)
		if facID.Valid && facID.String != "" {
			it.Faculty = &auditFaculty{ID: facID.String, EmployeeID: strPtr(empID), Name: strPtr(name)}
		}
		it.ChallengeID = strPtr(chal)
		it.Lat, it.Lon = microPtr(lat), microPtr(lon)
		if acc.Valid {
			v := float64(acc.Int64) / 100
			it.AccuracyM = &v
		}
		if dist.Valid {
			v := dist.Int64
			it.DistanceM = &v
		}
		it.RemoteIP, it.UserAgent = strPtr(ip), strPtr(ua)
		if dur.Valid {
			v := dur.Int64
			it.DurationMS = &v
		}
		if policy.Valid && json.Valid([]byte(policy.String)) {
			it.Policy = json.RawMessage(policy.String)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

var auditEndpoints = map[string]bool{"register": true, "challenge": true, "attendance": true, "history": true}

func idParam(q url.Values, name string) (*int64, error) {
	v := q.Get(name)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return nil, apiErr{"bad_request"}
	}
	return &n, nil
}

// GET /admin/api/audit
func (s *Server) adminAuditList(w http.ResponseWriter, r *http.Request) (int, any, error) {
	q := r.URL.Query()
	today := todayIST(s.now())

	from, to := today.AddDate(0, 0, -6), today // default: last 7 days
	if v := q.Get("from"); v != "" {
		d, err := parseDay(v)
		if err != nil {
			return 0, nil, err
		}
		from = d
		if q.Get("to") == "" {
			to = today
		}
	}
	if v := q.Get("to"); v != "" {
		d, err := parseDay(v)
		if err != nil {
			return 0, nil, err
		}
		to = d
		if q.Get("from") == "" {
			from = to.AddDate(0, 0, -6)
		}
	}
	if err := checkSpan(from, to); err != nil {
		return 0, nil, err
	}
	limit, err := intParam(q, "limit", 50, 1, 200)
	if err != nil {
		return 0, nil, err
	}
	before, err := idParam(q, "before_id")
	if err != nil {
		return 0, nil, err
	}
	after, err := idParam(q, "after_id")
	if err != nil {
		return 0, nil, err
	}

	where := []string{"a.ts >= ?", "a.ts < ?"}
	args := []any{from.Unix(), to.AddDate(0, 0, 1).Unix()}
	if v := q.Get("employee_id"); v != "" {
		where, args = append(where, "f.employee_id = ?"), append(args, v)
	}
	if v := q.Get("faculty_id"); v != "" {
		where, args = append(where, "a.faculty_id = ?"), append(args, v)
	}
	if v := q.Get("endpoint"); v != "" {
		if !auditEndpoints[v] {
			return 0, nil, apiErr{"bad_request"}
		}
		where, args = append(where, "a.endpoint = ?"), append(args, v)
	}
	switch v := q.Get("outcome"); v {
	case "":
	case "failed":
		where = append(where, "a.outcome != 'ok'")
	default: // "ok" or one error code
		where, args = append(where, "a.outcome = ?"), append(args, v)
	}
	if before != nil {
		where, args = append(where, "a.id < ?"), append(args, *before)
	}
	if after != nil {
		where, args = append(where, "a.id > ?"), append(args, *after)
	}

	items, err := s.queryAudit(r.Context(), where, args, limit)
	if err != nil {
		return 0, nil, err
	}
	var next *int64
	if int64(len(items)) == limit {
		v := items[len(items)-1].ID
		next = &v
	}
	return 200, map[string]any{"items": items, "next_before_id": next}, nil
}

// GET /admin/api/audit/{id}
func (s *Server) adminAuditOne(w http.ResponseWriter, r *http.Request) (int, any, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, nil, adminErr{code: "not_found"}
	}
	items, err := s.queryAudit(r.Context(), []string{"a.id = ?"}, []any{id}, 1)
	if err != nil {
		return 0, nil, err
	}
	if len(items) == 0 {
		return 0, nil, adminErr{code: "not_found"}
	}
	return 200, items[0], nil
}

