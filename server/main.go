package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const usage = `usage:
  server [--config <file>] serve            start the server
  server [--config <file>] admin-token      print one-time admin token and sha256
  server [--config <file>] add <employee_id> "<name>"  create a faculty member
  server [--config <file>] code <employee_id>          print a one-time enrollment code (15 min)
  server [--config <file>] revoke <employee_id>        clear the phone key (lost phone / leaver)
  server [--config <file>] list            employee id, name, enrolled yes/no
  server [--config <file>] mark <employee_id> <date> "<note>"  manual entry
  server [--config <file>] report [from] [to]  CSV of everyone's attendance (YYYY-MM-DD)`

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}

func fmtHM(minutes int) string {
	if minutes < 0 {
		minutes = 0
	}
	if minutes >= 24*60 {
		return "24:00"
	}
	h := minutes / 60
	m := minutes % 60
	return fmt.Sprintf("%02d:%02d", h, m)
}

func parseWindowTime(s string) (int, error) {
	if s == "24:00" {
		return 24 * 60, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("must be HH:MM or 24:00: %w", err)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func parseHM(key, def string) (int, error) {
	v := getenv(key, def)
	m, err := parseWindowTime(v)
	if err != nil {
		return 0, fmt.Errorf("%s %w", key, err)
	}
	return m, nil
}

func (w *AttendanceWindow) UnmarshalYAML(value *yaml.Node) error {
	type raw struct {
		ID    string `yaml:"id"`
		Start string `yaml:"start"`
		End   string `yaml:"end"`
	}
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	if r.ID == "" {
		return errors.New("window id is required")
	}
	if r.Start == "24:00" {
		return fmt.Errorf("window %q: start may not be 24:00", r.ID)
	}
	start, err := parseWindowTime(r.Start)
	if err != nil {
		return fmt.Errorf("window %q start: %w", r.ID, err)
	}
	end, err := parseWindowTime(r.End)
	if err != nil {
		return fmt.Errorf("window %q end: %w", r.ID, err)
	}
	if start >= end {
		return fmt.Errorf("window %q: start must be before end", r.ID)
	}
	w.ID = r.ID
	w.Start = start
	w.End = end
	return nil
}

type yamlConfig struct {
	Server struct {
		Listen string `yaml:"listen"`
		DBFile string `yaml:"db_file"`
	} `yaml:"server"`
	Geofence struct {
		Lat     *float64 `yaml:"lat"`
		Lon     *float64 `yaml:"lon"`
		RadiusM *float64 `yaml:"radius_m"`
		MaxAccM *float64 `yaml:"max_accuracy_m"`
	} `yaml:"geofence"`
	AttendanceWindows AttendanceWindows `yaml:"attendance_windows"`
	Window            struct {
		Start string `yaml:"start"`
		End   string `yaml:"end"`
	} `yaml:"window"`
	Admin struct {
		Listen      string `yaml:"listen"`
		TokenSHA256 string `yaml:"token_sha256"`
	} `yaml:"admin"`
	Audit struct {
		RetentionDays *int `yaml:"retention_days"`
		MaxRows       *int `yaml:"max_rows"`
	} `yaml:"audit"`
}

func validateConfig(c Config) map[string]string {
	errMap := map[string]string{}
	if c.GeoLat < -90 || c.GeoLat > 90 {
		errMap["geofence.lat"] = "must be in [-90, 90]"
	}
	if c.GeoLon < -180 || c.GeoLon > 180 {
		errMap["geofence.lon"] = "must be in [-180, 180]"
	}
	if c.RadiusM <= 0 {
		errMap["geofence.radius_m"] = "must be > 0"
	}
	if c.MaxAccM <= 0 {
		errMap["geofence.max_accuracy_m"] = "must be > 0"
	}
	if c.WinStart > 0 || c.WinEnd > 0 {
		if c.WinStart >= c.WinEnd {
			errMap["window.start"] = "must be before window.end"
			errMap["window.end"] = "must be after window.start"
		}
	}
	seen := map[string]bool{}
	for _, section := range []struct {
		name    string
		windows []AttendanceWindow
	}{
		{name: "in_windows", windows: c.AttendanceWindows.IN},
		{name: "out_windows", windows: c.AttendanceWindows.OUT},
	} {
		for i, w := range section.windows {
			if w.ID == "" {
				errMap["attendance_windows."+section.name+"["+strconv.Itoa(i)+"].id"] = "id is required"
				continue
			}
			if seen[w.ID] {
				errMap["attendance_windows."+section.name+"["+strconv.Itoa(i)+"].id"] = "duplicate window id: " + w.ID
				continue
			}
			seen[w.ID] = true
			if w.Start == 24*60 {
				errMap["attendance_windows."+section.name+"["+strconv.Itoa(i)+"].start"] = "start may not be 24:00"
			}
			if w.Start >= w.End || w.End > 24*60 {
				errMap["attendance_windows."+section.name+"["+strconv.Itoa(i)+"].start"] = "start must be before end"
			}
		}
		for i := 0; i < len(section.windows); i++ {
			for j := i + 1; j < len(section.windows); j++ {
				a, b := section.windows[i], section.windows[j]
				if a.Start < b.End && b.Start < a.End {
					errMap["attendance_windows."+section.name] = "windows overlap: " + a.ID + " and " + b.ID
				}
			}
		}
	}
	if c.AdminListen != "" {
		if len(c.AdminTokenSHA256) != 64 {
			errMap["admin.token_sha256"] = "must be exactly 64 hex chars"
		} else if _, err := hex.DecodeString(c.AdminTokenSHA256); err != nil {
			errMap["admin.token_sha256"] = "must be valid hex"
		}
		if c.AdminListen == c.Listen {
			errMap["admin.listen"] = "must differ from server.listen"
		}
	}
	if c.AuditRetentionDays < 0 {
		errMap["audit.retention_days"] = "must be >= 0"
	}
	if c.AuditMaxRows < 0 {
		errMap["audit.max_rows"] = "must be >= 0"
	}
	return errMap
}

func loadConfig(path string, needGeo bool) (Config, error) {
	c := Config{DBFile: "attendance.db", Listen: "127.0.0.1:8080", RadiusM: 150, MaxAccM: 50, AuditRetentionDays: 180, AuditMaxRows: 500000}
	if path == "" {
		path = "config.yaml"
	}
	hasLat, hasLon := false, false
	if raw, err := os.ReadFile(path); err == nil {
		if len(bytes.TrimSpace(raw)) == 0 {
			return c, nil
		}
		var y yamlConfig
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&y); err != nil && !errors.Is(err, io.EOF) {
			return c, err
		}
		if y.Server.Listen != "" {
			c.Listen = y.Server.Listen
		}
		if y.Server.DBFile != "" {
			c.DBFile = y.Server.DBFile
		}
		if y.Geofence.Lat != nil {
			hasLat = true
			c.GeoLat = *y.Geofence.Lat
		}
		if y.Geofence.Lon != nil {
			hasLon = true
			c.GeoLon = *y.Geofence.Lon
		}
		if y.Geofence.RadiusM != nil {
			c.RadiusM = *y.Geofence.RadiusM
		}
		if y.Geofence.MaxAccM != nil {
			c.MaxAccM = *y.Geofence.MaxAccM
		}
		if len(y.AttendanceWindows.IN) > 0 || len(y.AttendanceWindows.OUT) > 0 {
			c.AttendanceWindows = y.AttendanceWindows
		} else if y.Window.Start != "" || y.Window.End != "" {
			c.WinStart, err = parseWindowTime(y.Window.Start)
			if err != nil {
				return c, fmt.Errorf("window.start must be HH:MM or 24:00: %w", err)
			}
			c.WinEnd, err = parseWindowTime(y.Window.End)
			if err != nil {
				return c, fmt.Errorf("window.end must be HH:MM or 24:00: %w", err)
			}
		}
		if y.Admin.Listen != "" {
			c.AdminListen = y.Admin.Listen
		}
		if y.Admin.TokenSHA256 != "" {
			c.AdminTokenSHA256 = y.Admin.TokenSHA256
		}
		if y.Audit.RetentionDays != nil {
			c.AuditRetentionDays = *y.Audit.RetentionDays
		}
		if y.Audit.MaxRows != nil {
			c.AuditMaxRows = *y.Audit.MaxRows
		}
		if !filepath.IsAbs(c.DBFile) {
			c.DBFile = filepath.Join(filepath.Dir(path), c.DBFile)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if needGeo {
			return c, fmt.Errorf("config file %q not found", path)
		}
	} else {
		return c, err
	}
	if needGeo {
		if !hasLat || !hasLon {
			return c, errors.New("geofence.lat and geofence.lon must be set")
		}
	}
	if errs := validateConfig(c); len(errs) > 0 {
		keys := make([]string, 0, len(errs))
		for k := range errs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return c, fmt.Errorf("%s: %s", keys[0], errs[keys[0]])
	}
	return c, nil
}

func parseArgs(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New(usage)
	}
	cfgPath := "config.yaml"
	if args[0] == "--config" {
		if len(args) < 2 {
			return "", nil, errors.New("missing value for --config\n" + usage)
		}
		cfgPath = args[1]
		args = args[2:]
	}
	if len(args) == 0 {
		return "", nil, errors.New(usage)
	}
	return cfgPath, args, nil
}

func main() {
	cfgPath, args, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := runWithConfig(args[0], args[1:], cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func need(args []string, n int) error {
	if len(args) != n {
		return errors.New("wrong number of arguments\n" + usage)
	}
	return nil
}

func runWithConfig(cmd string, args []string, cfgPath string) error {
	cfg, err := loadConfig(cfgPath, cmd == "serve")
	if err != nil {
		return err
	}
	db, err := openDB(cfg.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now()

	switch cmd {
	case "serve":
		return serve(cfg, db)
	case "admin-token":
		return adminToken()
	case "add":
		if err := need(args, 2); err != nil {
			return err
		}
		id, err := addFaculty(db, args[0], args[1])
		if err != nil {
			return err
		}
		fmt.Printf("added %s (%s), faculty id %s\n", args[0], args[1], id)
	case "code":
		if err := need(args, 1); err != nil {
			return err
		}
		code, err := issueCode(db, args[0], now)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n(valid 15 minutes, single use; show it as a QR)\n", code)
	case "revoke":
		if err := need(args, 1); err != nil {
			return err
		}
		if err := revokeFaculty(db, args[0]); err != nil {
			return err
		}
		fmt.Println("revoked", args[0])
	case "delete":
		if err := need(args, 1); err != nil {
			return err
		}
		if err := deleteFaculty(db, args[0]); err != nil {
			return err
		}
		fmt.Println("deleted", args[0])
	case "list":
		return listFaculty(db)
	case "mark":
		if err := need(args, 3); err != nil {
			return err
		}
		if err := markManual(db, args[0], args[1], args[2], now); err != nil {
			return err
		}
		fmt.Printf("marked %s on %s\n", args[0], args[1])
	case "report":
		if len(args) > 2 {
			return errors.New(usage)
		}
		from, to := "0000-01-01", "9999-12-31"
		if len(args) > 0 {
			from = args[0]
		}
		if len(args) > 1 {
			to = args[1]
		}
		return report(db, from, to)
	default:
		return errors.New("unknown command\n" + usage)
	}
	return nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func adminToken() error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	token := hex.EncodeToString(b)
	fmt.Printf("token: %s\n", token)
	fmt.Printf("token_sha256: %s\n", sha256Hex(token))
	return nil
}

func run(cmd string, args []string) error {
	return runWithConfig(cmd, args, "config.yaml")
}

func listFaculty(db *sql.DB) error {
	rows, err := db.Query(`SELECT employee_id, name, public_key IS NOT NULL FROM faculty ORDER BY employee_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var emp, name string
		var enrolled bool
		if err := rows.Scan(&emp, &name, &enrolled); err != nil {
			return err
		}
		yn := "no"
		if enrolled {
			yn = "yes"
		}
		fmt.Printf("%s\t%s\tenrolled: %s\n", emp, name, yn)
	}
	return rows.Err()
}

func report(db *sql.DB, from, to string) error {
	rows, err := db.Query(`SELECT employee_id, name, day, marked_at_ist, distance_m, note
		FROM attendance_report WHERE day >= ? AND day <= ? ORDER BY day, employee_id`, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{"employee_id", "name", "day", "marked_at_ist", "distance_m", "note"})
	for rows.Next() {
		var emp, name, day, at, note string
		var dist sql.NullInt64
		if err := rows.Scan(&emp, &name, &day, &at, &dist, &note); err != nil {
			return err
		}
		d := ""
		if dist.Valid {
			d = strconv.FormatInt(dist.Int64, 10)
		}
		w.Write([]string{emp, name, day, at, d, note})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func serve(cfg Config, db *sql.DB) error {
	s := newServer(db, cfg, time.Now)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	var adminSrv *http.Server
	if cfg.AdminListen != "" {
		adminSrv = &http.Server{
			Addr:              cfg.AdminListen,
			Handler:           s.adminRoutes(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    8 << 10,
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	var stopOnce sync.Once
	shutdown := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("public shutdown: %v", err)
			}
			if adminSrv != nil {
				if err := adminSrv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Printf("admin shutdown: %v", err)
				}
			}
		})
	}
	go func() {
		select {
		case <-stop:
		case <-s.shutdownReq:
		}
		shutdown()
	}()
	log.Printf("listening on %s", cfg.Listen)
	if cfg.AdminListen != "" {
		log.Printf("admin listening on %s", cfg.AdminListen)
	}
	publicErr := make(chan error, 1)
	go func() { publicErr <- srv.ListenAndServe() }()
	if adminSrv != nil {
		adminErrCh := make(chan error, 1)
		go func() { adminErrCh <- adminSrv.ListenAndServe() }()
		select {
		case err := <-publicErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				shutdown()
				if aErr := <-adminErrCh; aErr != nil && !errors.Is(aErr, http.ErrServerClosed) {
					return aErr
				}
				return err
			}
		case aErr := <-adminErrCh:
			if aErr != nil && !errors.Is(aErr, http.ErrServerClosed) {
				shutdown()
				if err := <-publicErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return aErr
			}
		}
		return nil
	}
	if err := <-publicErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
