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
	if c.WinStart != 9*60 || c.WinEnd != 18*60 {
		t.Fatalf("%+v", c)
	}
	if want := filepath.Join(filepath.Dir(p), "attendance.db"); c.DBFile != want {
		t.Fatalf("db path %q, want %q", c.DBFile, want)
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"missing lat/lon":  "server:\n  listen: \":8080\"\n",
		"unknown key":      "geofence:\n  lat: 1\n  lon: 1\n  radius: 5\n",
		"bad time":         "geofence:\n  lat: 1\n  lon: 1\nwindow:\n  start: \"9am\"\n",
		"start after end":  "geofence:\n  lat: 1\n  lon: 1\nwindow:\n  start: \"18:00\"\n  end: \"09:00\"\n",
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

func TestLoadConfigAcceptsTwentyFourHourWindow(t *testing.T) {
	p := writeCfg(t, "geofence:\n  lat: 1\n  lon: 1\nwindow:\n  start: \"00:00\"\n  end: \"24:00\"\n")
	c, err := loadConfig(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.WinStart != 0 || c.WinEnd != 24*60 {
		t.Fatalf("win window = %#v, want start=0 end=1440", c)
	}
}

func TestWindowOpenTwentyFourSevenIncludesWeekend(t *testing.T) {
	s := newServer(nil, Config{WinStart: 0, WinEnd: 24 * 60}, time.Now)
	if !s.windowOpen(time.Date(2026, 10, 31, 9, 0, 0, 0, ist)) {
		t.Fatal("full-day attendance window should be open on Saturday")
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
