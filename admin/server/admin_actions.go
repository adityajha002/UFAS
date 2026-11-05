package main

// Trail of what admins DID (as opposed to audit_log, which records what phones
// did). Rows never hold enrollment codes, tokens or token hashes.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
)

var actionNames = map[string]bool{
	"faculty_add": true, "code_issue": true, "faculty_revoke": true, "manual_mark": true,
	"config_change": true, "config_reset": true, "shutdown": true,
}

// outcomeOf is "ok" or the error code a client would see.
func outcomeOf(err error) string {
	if err == nil {
		return "ok"
	}
	var ae adminErr
	var pe apiErr
	switch {
	case errors.As(err, &ae):
		return ae.code
	case errors.As(err, &pe):
		return pe.code
	}
	return "internal"
}

// errDetail keeps per-field validation messages for the trail of a failed action.
func errDetail(err error) any {
	var ae adminErr
	if errors.As(err, &ae) && ae.fields != nil {
		return map[string]any{"fields": ae.fields}
	}
	return nil
}

func joinKeys(m map[string]change) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// recordAction writes one admin_actions row. A failure is logged and never
// changes the response the admin gets.
func (s *Server) recordAction(r *http.Request, action, target string, detail any, outcome string) {
	if s.db == nil {
		return
	}
	var d any
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			d = string(b)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO admin_actions (ts, action, target, detail, outcome, remote_ip)
		VALUES (?, ?, ?, ?, ?, ?)`,
		s.now().Unix(), action, nullStr(target), d, outcome, nullStr(remoteIP(r))); err != nil {
		log.Printf("admin action %s not recorded: %v", action, err)
	}
}

// trackAction is used as `defer s.trackAction(r, "x", &target, &detail, &err)()`
// in handlers with a named error result: the row is written on the way out,
// for success and failure alike, before the response is sent.
func (s *Server) trackAction(r *http.Request, action string, target *string, detail *any, errp *error) func() {
	return func() {
		d := *detail
		if *errp != nil && d == nil {
			d = errDetail(*errp)
		}
		s.recordAction(r, action, *target, d, outcomeOf(*errp))
	}
}

type actionItem struct {
	ID       int64           `json:"id"`
	TS       int64           `json:"ts"`
	AtIST    string          `json:"at_ist"`
	Action   string          `json:"action"`
	Target   *string         `json:"target"`
	Detail   json.RawMessage `json:"detail"`
	Outcome  string          `json:"outcome"`
	RemoteIP *string         `json:"remote_ip"`
}

// GET /admin/api/actions?limit=&before_id=&after_id=&action=
func (s *Server) adminActionsList(w http.ResponseWriter, r *http.Request) (int, any, error) {
	q := r.URL.Query()
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
	var where []string
	var args []any
	if v := q.Get("action"); v != "" {
		if !actionNames[v] {
			return 0, nil, apiErr{"bad_request"}
		}
		where, args = append(where, "action = ?"), append(args, v)
	}
	if before != nil {
		where, args = append(where, "id < ?"), append(args, *before)
	}
	if after != nil {
		where, args = append(where, "id > ?"), append(args, *after)
	}
	query := `SELECT id, ts, action, target, detail, outcome, remote_ip FROM admin_actions`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ?"
	items, err := s.queryActions(r, query, append(args, limit))
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

func (s *Server) queryActions(r *http.Request, query string, args []any) ([]actionItem, error) {
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []actionItem{}
	for rows.Next() {
		var it actionItem
		var target, detail, ip sql.NullString
		if err := rows.Scan(&it.ID, &it.TS, &it.Action, &target, &detail, &it.Outcome, &ip); err != nil {
			return nil, err
		}
		it.AtIST = fmtIST(it.TS)
		it.Target, it.RemoteIP = strPtr(target), strPtr(ip)
		if detail.Valid && json.Valid([]byte(detail.String)) {
			it.Detail = json.RawMessage(detail.String)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
