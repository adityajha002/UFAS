package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
)

type change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type cfgMeta struct {
	key, label, typ, unit string
	editable              bool
	min, max              *float64
}

func f64(v float64) *float64 { return &v }

var cfgFields = []cfgMeta{
	{"geofence.lat", "Geofence centre latitude", "float", "deg", true, f64(-90), f64(90)},
	{"geofence.lon", "Geofence centre longitude", "float", "deg", true, f64(-180), f64(180)},
	{"geofence.radius_m", "Geofence radius", "float", "m", true, f64(0), nil},
	{"geofence.max_accuracy_m", "Maximum GPS error accepted", "float", "m", true, f64(0), nil},
	{"attendance_windows.in_windows", "Entry (IN) timing windows", "windows", "", true, nil, nil},
	{"attendance_windows.out_windows", "Exit (OUT) timing windows", "windows", "", true, nil, nil},
	{"window.start", "Fallback window opens", "time", "HH:MM", true, nil, nil},
	{"window.end", "Fallback window closes", "time", "HH:MM", true, nil, nil},
	{"server.listen", "Phone-facing listen address", "string", "", false, nil, nil},
	{"server.db_file", "Database file", "string", "", false, nil, nil},
	{"admin.listen", "Admin listen address", "string", "", false, nil, nil},
	{"audit.retention_days", "Audit retention (0 = forever)", "int", "days", false, nil, nil},
	{"audit.max_rows", "Audit row cap (0 = unlimited)", "int", "rows", false, nil, nil},
}

type cfgFieldOut struct {
	Key             string   `json:"key"`
	Label           string   `json:"label"`
	Type            string   `json:"type"`
	Unit            string   `json:"unit,omitempty"`
	Value           any      `json:"value"`
	YAMLValue       any      `json:"yaml_value,omitempty"`
	Source          string   `json:"source,omitempty"` // "yaml" | "override"
	Editable        bool     `json:"editable"`
	RestartRequired bool     `json:"restart_required,omitempty"`
	Min             *float64 `json:"min,omitempty"`
	Max             *float64 `json:"max,omitempty"`
}

// configFields describes every key for the effective config cur. The admin
// token hash is never part of it.
func (s *Server) configFields(cur Config) []cfgFieldOut {
	out := make([]cfgFieldOut, 0, len(cfgFields))
	for _, m := range cfgFields {
		f := cfgFieldOut{Key: m.key, Label: m.label, Type: m.typ, Unit: m.unit, Value: cfgGet(cur, m.key),
			Editable: m.editable, RestartRequired: !m.editable, Min: m.min, Max: m.max}
		if m.editable {
			f.YAMLValue = cfgGet(s.base, m.key)
			f.Source = "yaml"
			if !reflect.DeepEqual(f.Value, f.YAMLValue) {
				f.Source = "override"
			}
		}
		out = append(out, f)
	}
	return out
}

type configBody struct {
	Fields  []cfgFieldOut `json:"fields"`
	Changed any           `json:"changed,omitempty"`
}

// GET /admin/api/config
func (s *Server) adminConfigGet(w http.ResponseWriter, r *http.Request) (int, any, error) {
	return 200, configBody{Fields: s.configFields(s.config())}, nil
}

type configPatch struct {
	Geofence *struct {
		Lat     *float64 `json:"lat"`
		Lon     *float64 `json:"lon"`
		RadiusM *float64 `json:"radius_m"`
		MaxAccM *float64 `json:"max_accuracy_m"`
	} `json:"geofence"`
	Window *struct {
		Start *string `json:"start"`
		End   *string `json:"end"`
	} `json:"window"`
	AttendanceWindows *struct {
		IN  *[]windowDTO `json:"in_windows"`
		OUT *[]windowDTO `json:"out_windows"`
	} `json:"attendance_windows"`
}

func (p configPatch) empty() bool {
	g, w, aw := p.Geofence, p.Window, p.AttendanceWindows
	return (g == nil || (g.Lat == nil && g.Lon == nil && g.RadiusM == nil && g.MaxAccM == nil)) &&
		(w == nil || (w.Start == nil && w.End == nil)) &&
		(aw == nil || (aw.IN == nil && aw.OUT == nil))
}

func editableErrs(c Config) map[string]string {
	out := map[string]string{}
	for k, msg := range validateConfig(c) {
		if isEditableKey(k) {
			out[k] = msg
		}
	}
	return out
}

// PATCH /admin/api/config[?dry_run=true]
func (s *Server) adminConfigPatch(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	var changed map[string]change
	skip := false // dry runs and no-op patches change nothing, so leave no trail
	defer func() {
		if skip {
			return
		}
		var detail any
		target := ""
		if err == nil {
			detail, target = changed, joinKeys(changed)
		} else {
			detail = errDetail(err)
		}
		s.recordAction(r, "config_change", target, detail, outcomeOf(err))
	}()
	var p configPatch
	if err := decode(w, r, &p); err != nil {
		return 0, nil, err
	}
	if p.empty() {
		return 0, nil, apiErr{"bad_request"}
	}
	dryRun := r.URL.Query().Get("dry_run") == "true" || r.URL.Query().Get("dry_run") == "1"

	s.cfgMu.Lock() // read-modify-write of the effective config
	defer s.cfgMu.Unlock()
	cur := s.config()
	cand := cur
	fields := map[string]string{}
	if g := p.Geofence; g != nil {
		if g.Lat != nil {
			cand.GeoLat = *g.Lat
		}
		if g.Lon != nil {
			cand.GeoLon = *g.Lon
		}
		if g.RadiusM != nil {
			cand.RadiusM = *g.RadiusM
		}
		if g.MaxAccM != nil {
			cand.MaxAccM = *g.MaxAccM
		}
	}
	if wv := p.Window; wv != nil {
		if wv.Start != nil {
			if m, err := parseWindowTime(*wv.Start); err != nil {
				fields["window.start"] = "must be HH:MM or 24:00"
			} else {
				cand.WinStart = m
			}
		}
		if wv.End != nil {
			if m, err := parseWindowTime(*wv.End); err != nil {
				fields["window.end"] = "must be HH:MM or 24:00"
			} else {
				cand.WinEnd = m
			}
		}
	}
	if aw := p.AttendanceWindows; aw != nil {
		if aw.IN != nil {
			ws, err := dtoToWindows(*aw.IN)
			if err != nil {
				fields["attendance_windows.in_windows"] = err.Error()
			} else {
				cand.AttendanceWindows.IN = ws
			}
		}
		if aw.OUT != nil {
			ws, err := dtoToWindows(*aw.OUT)
			if err != nil {
				fields["attendance_windows.out_windows"] = err.Error()
			} else {
				cand.AttendanceWindows.OUT = ws
			}
		}
	}
	for k, msg := range editableErrs(cand) { // the same rules as startup
		if _, dup := fields[k]; !dup {
			fields[k] = msg
		}
	}
	if len(fields) > 0 {
		return 0, nil, adminErr{code: "invalid_config", msg: fmt.Sprintf("%d field(s) invalid", len(fields)), fields: fields}
	}

	changed = diffConfig(cur, cand)
	if dryRun {
		skip = true
		return 200, map[string]any{"dry_run": true, "changed": changed}, nil
	}
	if len(changed) == 0 {
		skip = true
		return 200, configBody{Fields: s.configFields(cur), Changed: changed}, nil
	}
	if err := s.saveConfig(r.Context(), r, cand, changed, nil); err != nil {
		return 0, nil, err
	}
	return 200, configBody{Fields: s.configFields(cand), Changed: changed}, nil
}

// saveConfig persists a config change in ONE transaction (settings rows plus a
// history row), then swaps the live config. dropKeys are stored override rows
// to delete even when they changed nothing live (ignored overrides).
func (s *Server) saveConfig(ctx context.Context, r *http.Request, cand Config, changed map[string]change, dropKeys []string) error {
	nowU := s.now().Unix()
	hist, err := json.Marshal(changed)
	if err != nil {
		return err
	}
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		for k := range changed {
			if reflect.DeepEqual(cfgGet(cand, k), cfgGet(s.base, k)) {
				// back at the YAML value: drop the override instead of storing a duplicate
				if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, k); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
				k, cfgStored(cand, k), nowU); err != nil {
				return err
			}
		}
		for _, k := range dropKeys {
			if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, k); err != nil {
				return err
			}
		}
		if len(changed) == 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO config_history (ts, config_json, remote_ip) VALUES (?, ?, ?)`,
			nowU, string(hist), nullStr(remoteIP(r)))
		return err
	})
	if err != nil {
		return err
	}
	s.setConfig(cand)
	keys := append([]string{}, dropKeys...)
	for k := range changed {
		keys = append(keys, k)
	}
	s.clearWarnings(keys...)
	return nil
}

// DELETE /admin/api/config/overrides[?key=geofence.radius_m]  (no key = all)
func (s *Server) adminConfigReset(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	key := r.URL.Query().Get("key")
	var changed map[string]change
	skip := false
	defer func() {
		if skip {
			return
		}
		var detail any
		target := key
		if target == "" {
			target = "all"
		}
		if err == nil {
			detail = changed
		} else {
			detail = errDetail(err)
		}
		s.recordAction(r, "config_reset", target, detail, outcomeOf(err))
	}()
	if key != "" && !isEditableKey(key) {
		return 0, nil, adminErr{code: "invalid_config", msg: "unknown or not editable key",
			fields: map[string]string{"key": "unknown or not editable"}}
	}
	targets := editableKeys
	if key != "" {
		targets = []string{key}
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	cur := s.config()
	cand := cur
	for _, k := range targets {
		cfgSet(&cand, k, cfgStored(s.base, k))
	}
	if errs := editableErrs(cand); len(errs) > 0 {
		return 0, nil, adminErr{code: "invalid_config",
			msg:    "resetting this key alone would make the configuration invalid; reset window.start and window.end together",
			fields: errs}
	}
	stored, err := s.loadSettings(r.Context())
	if err != nil {
		return 0, nil, err
	}
	var drop []string // also clears stored rows that were ignored at startup
	for _, k := range targets {
		if _, ok := stored[k]; ok {
			drop = append(drop, k)
		}
	}
	changed = diffConfig(cur, cand)
	if len(changed) == 0 && len(drop) == 0 {
		skip = true
		return 200, configBody{Fields: s.configFields(cur), Changed: changed}, nil
	}
	if err := s.saveConfig(r.Context(), r, cand, changed, drop); err != nil {
		return 0, nil, err
	}
	return 200, configBody{Fields: s.configFields(cand), Changed: changed}, nil
}

type historyItem struct {
	ID       int64           `json:"id"`
	TS       int64           `json:"ts"`
	AtIST    string          `json:"at_ist"`
	RemoteIP *string         `json:"remote_ip"`
	Changes  json.RawMessage `json:"changes"`
}

// GET /admin/api/config/history?limit=50
func (s *Server) adminConfigHistory(w http.ResponseWriter, r *http.Request) (int, any, error) {
	limit, err := intParam(r.URL.Query(), "limit", 50, 1, 200)
	if err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, ts, remote_ip, config_json FROM config_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []historyItem{}
	for rows.Next() {
		var it historyItem
		var ip sql.NullString
		var raw string
		if err := rows.Scan(&it.ID, &it.TS, &ip, &raw); err != nil {
			return 0, nil, err
		}
		it.AtIST = fmtIST(it.TS)
		it.RemoteIP = strPtr(ip)
		if json.Valid([]byte(raw)) {
			it.Changes = json.RawMessage(raw)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	return 200, map[string]any{"items": items}, nil
}
