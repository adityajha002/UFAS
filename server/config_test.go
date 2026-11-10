package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigDefaults(t *testing.T) {
	p := writeCfg(t, "geofence:\n  lat: 28.6\n  lon: 77.2\n")
	c, err := loadConfig(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.GeoLat != 28.6 || c.GeoLon != 77.2 || c.RadiusM != 150 || c.MaxAccM != 50 {
		t.Fatalf("%+v", c)
	}
	if len(c.AttendanceWindows.IN) != 0 || len(c.AttendanceWindows.OUT) != 0 {
		t.Fatalf("expected empty default schedule, got %#v", c.AttendanceWindows)
	}
	if want := filepath.Join(filepath.Dir(p), "attendance.db"); c.DBFile != want {
		t.Fatalf("db path %q, want %q", c.DBFile, want)
	}
}

func TestLoadConfigAcceptsAttendanceWindows(t *testing.T) {
	p := writeCfg(t, `geofence:
  lat: 28.6
  lon: 77.2
attendance_windows:
  in_windows:
    - id: "in-1"
      start: "09:00"
      end: "10:00"
    - id: "in-2"
      start: "13:00"
      end: "14:00"
  out_windows:
    - id: "out-1"
      start: "13:00"
      end: "14:00"
    - id: "out-2"
      start: "16:00"
      end: "17:00"
`)
	c, err := loadConfig(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AttendanceWindows.IN) != 2 || len(c.AttendanceWindows.OUT) != 2 {
		t.Fatalf("windows mismatch: %#v", c.AttendanceWindows)
	}
	if c.AttendanceWindows.IN[0].ID != "in-1" || c.AttendanceWindows.IN[0].Start != 9*60 || c.AttendanceWindows.IN[0].End != 10*60 {
		t.Fatalf("bad in window %#v", c.AttendanceWindows.IN[0])
	}
	if c.AttendanceWindows.OUT[1].ID != "out-2" || c.AttendanceWindows.OUT[1].Start != 16*60 || c.AttendanceWindows.OUT[1].End != 17*60 {
		t.Fatalf("bad out window %#v", c.AttendanceWindows.OUT[1])
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"missing lat/lon":  "server:\n  listen: \":8080\"\n",
		"unknown key":      "geofence:\n  lat: 1\n  lon: 1\n  radius: 5\n",
		"bad time":         "geofence:\n  lat: 1\n  lon: 1\nattendance_windows:\n  in_windows:\n    - id: \"in-1\"\n      start: \"9am\"\n      end: \"10:00\"\n",
		"start after end":  "geofence:\n  lat: 1\n  lon: 1\nattendance_windows:\n  in_windows:\n    - id: \"in-1\"\n      start: \"18:00\"\n      end: \"09:00\"\n",
		"overlap":          "geofence:\n  lat: 1\n  lon: 1\nattendance_windows:\n  in_windows:\n    - id: \"in-1\"\n      start: \"09:00\"\n      end: \"10:00\"\n    - id: \"in-2\"\n      start: \"09:30\"\n      end: \"11:00\"\n",
		"duplicate id":     "geofence:\n  lat: 1\n  lon: 1\nattendance_windows:\n  in_windows:\n    - id: \"x\"\n      start: \"09:00\"\n      end: \"10:00\"\n  out_windows:\n    - id: \"x\"\n      start: \"11:00\"\n      end: \"12:00\"\n",
		"lat out of range": "geofence:\n  lat: 95\n  lon: 1\n",
		"zero radius":      "geofence:\n  lat: 1\n  lon: 1\n  radius_m: 0\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig(writeCfg(t, body), true); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestActionWindowsOpenAt(t *testing.T) {
	cfg := Config{AttendanceWindows: AttendanceWindows{
		IN:  []AttendanceWindow{{ID: "in-1", Start: 9 * 60, End: 10 * 60}, {ID: "in-2", Start: 13 * 60, End: 14 * 60}},
		OUT: []AttendanceWindow{{ID: "out-1", Start: 13 * 60, End: 14 * 60}, {ID: "out-2", Start: 16 * 60, End: 17 * 60}},
	}}
	if !actionWindowOpenAt(time.Date(2026, 10, 23, 9, 30, 0, 0, ist), cfg, "in") {
		t.Fatal("morning in-window should be open")
	}
	if !actionWindowOpenAt(time.Date(2026, 10, 23, 16, 30, 0, 0, ist), cfg, "out") {
		t.Fatal("evening out-window should be open")
	}
	if actionWindowOpenAt(time.Date(2026, 10, 23, 11, 0, 0, 0, ist), cfg, "in") {
		t.Fatal("in should be closed outside configured windows")
	}
	if actionWindowOpenAt(time.Date(2026, 10, 23, 12, 0, 0, 0, ist), cfg, "out") {
		t.Fatal("out should be closed outside configured windows")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.yaml")
	if _, err := loadConfig(p, false); err != nil {
		t.Fatalf("admin commands must work without a file: %v", err)
	}
	if _, err := loadConfig(p, true); err == nil {
		t.Fatal("serve must require the file")
	}
}
