package main

// Live-editable config. Effective value = stored override if present, else the
// YAML value (Server.base). Overrides live in the `settings` table so a change
// is transactional and never rewrites the hand-edited YAML file.

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
)

var editableKeys = []string{
	"geofence.lat", "geofence.lon", "geofence.radius_m", "geofence.max_accuracy_m",
	"window.start", "window.end",
}

func isEditableKey(k string) bool {
	for _, e := range editableKeys {
		if e == k {
			return true
		}
	}
	return false
}

// cfgGet returns the typed value of a key: float64, string or int.
func cfgGet(c Config, key string) any {
	switch key {
	case "geofence.lat":
		return c.GeoLat
	case "geofence.lon":
		return c.GeoLon
	case "geofence.radius_m":
		return c.RadiusM
	case "geofence.max_accuracy_m":
		return c.MaxAccM
	case "window.start":
		return fmtHM(c.WinStart)
	case "window.end":
		return fmtHM(c.WinEnd)
	case "server.listen":
		return c.Listen
	case "server.db_file":
		return c.DBFile
	case "admin.listen":
		return c.AdminListen
	case "audit.retention_days":
		return c.AuditRetentionDays
	case "audit.max_rows":
		return c.AuditMaxRows
	}
	return nil
}

// cfgStored is the text form kept in the settings table.
func cfgStored(c Config, key string) string {
	switch v := cfgGet(c, key).(type) {
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return v
	}
	return ""
}

func parseFinite(raw string) (float64, error) {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("not a finite number")
	}
	return f, nil
}

// cfgSet parses the stored text form into c. On error c is left untouched.
func cfgSet(c *Config, key, raw string) error {
	switch key {
	case "geofence.lat", "geofence.lon", "geofence.radius_m", "geofence.max_accuracy_m":
		f, err := parseFinite(raw)
		if err != nil {
			return err
		}
		switch key {
		case "geofence.lat":
			c.GeoLat = f
		case "geofence.lon":
			c.GeoLon = f
		case "geofence.radius_m":
			c.RadiusM = f
		default:
			c.MaxAccM = f
		}
	case "window.start", "window.end":
		m, err := parseWindowTime(raw)
		if err != nil {
			return err
		}
		if key == "window.start" {
			c.WinStart = m
		} else {
			c.WinEnd = m
		}
	default:
		return fmt.Errorf("unknown setting")
	}
	return nil
}

// diffConfig lists the editable keys whose value differs between a and b.
func diffConfig(a, b Config) map[string]change {
	out := map[string]change{}
	for _, k := range editableKeys {
		if av, bv := cfgGet(a, k), cfgGet(b, k); av != bv {
			out[k] = change{Old: av, New: bv}
		}
	}
	return out
}

func (s *Server) loadSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// applyStoredOverrides runs once at startup, before the listeners. An override
// that no longer validates (e.g. a hand-edited DB) is skipped, logged loudly
// and reported in /status config_warnings; the YAML value stays in force.
func (s *Server) applyStoredOverrides() {
	if s.db == nil {
		return
	}
	stored, err := s.loadSettings(context.Background())
	if err != nil {
		s.addWarning("settings", "could not read stored overrides: "+err.Error())
		return
	}
	keys := make([]string, 0, len(stored))
	for k := range stored {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	cand := s.base
	applied := map[string]bool{}
	for _, k := range keys {
		if !isEditableKey(k) {
			s.addWarning(k, "unknown stored setting ignored")
			continue
		}
		if err := cfgSet(&cand, k, stored[k]); err != nil {
			s.addWarning(k, fmt.Sprintf("stored override %q ignored: %v", stored[k], err))
			continue
		}
		applied[k] = true
	}
	for i := 0; i < len(editableKeys); i++ { // each pass drops at least one bad key
		bad := false
		for k, msg := range validateConfig(cand) {
			if applied[k] {
				s.addWarning(k, fmt.Sprintf("stored override %q ignored: %s", stored[k], msg))
				cfgSet(&cand, k, cfgStored(s.base, k))
				delete(applied, k)
				bad = true
			}
		}
		if !bad {
			break
		}
	}
	if len(validateConfig(cand)) > 0 { // should be impossible; never start on an invalid config
		s.addWarning("settings", "stored overrides are inconsistent; all ignored")
		return
	}
	if len(applied) > 0 {
		s.setConfig(cand)
		log.Printf("applied %d stored config override(s)", len(applied))
	}
}

func (s *Server) addWarning(key, msg string) {
	log.Printf("WARNING config %s: %s", key, msg)
	s.warnMu.Lock()
	defer s.warnMu.Unlock()
	if s.warnings == nil {
		s.warnings = map[string]string{}
	}
	s.warnings[key] = msg
}

func (s *Server) clearWarnings(keys ...string) {
	s.warnMu.Lock()
	defer s.warnMu.Unlock()
	for _, k := range keys {
		delete(s.warnings, k)
	}
}

func (s *Server) warningList() []string {
	s.warnMu.Lock()
	defer s.warnMu.Unlock()
	keys := make([]string, 0, len(s.warnings))
	for k := range s.warnings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+": "+s.warnings[k])
	}
	return out
}
