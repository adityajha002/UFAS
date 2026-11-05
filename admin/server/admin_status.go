package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"time"
)

// GET /admin/api/status
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) (int, any, error) {
	cfg := s.config()
	now := s.now()
	today := ymd(todayIST(now))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// public listener: really accepting connections?
	host, port, splitErr := net.SplitHostPort(cfg.Listen)
	reachable := false
	if splitErr == nil && port != "" {
		dialHost := host
		if isWildcardHost(host) {
			dialHost = "127.0.0.1"
		}
		if c, err := net.DialTimeout("tcp", net.JoinHostPort(dialHost, port), 500*time.Millisecond); err == nil {
			c.Close()
			reachable = true
		}
	}

	// addresses phones could use
	addrs := s.netAddrs()
	if addrs == nil {
		addrs = []netAddr{}
	}
	loopback := isLoopbackAddr(cfg.Listen)
	urls := []string{}
	warning := ""
	switch {
	case splitErr != nil || port == "":
		warning = "server.listen is not a host:port address"
	case loopback:
		warning = "public listener is bound to loopback; phones on the network cannot reach it"
	case isWildcardHost(host):
		for _, a := range addrs {
			urls = append(urls, "http://"+net.JoinHostPort(a.IP, port))
		}
		if len(urls) == 0 {
			warning = "no active network interface with an IPv4 address was found"
		}
	default:
		urls = append(urls, "http://"+net.JoinHostPort(host, port))
	}
	hostname, _ := os.Hostname()

	ws := s.windowState(now, cfg)
	var next any
	if !ws.NextChange.IsZero() {
		next = ws.NextChange.In(ist).Format(istLayout)
	}

	dbInfo := map[string]any{"file": cfg.DBFile, "size_bytes": nil, "ok": s.db != nil && s.db.PingContext(ctx) == nil}
	if fi, err := os.Stat(cfg.DBFile); err == nil {
		dbInfo["size_bytes"] = fi.Size()
	}

	var faculty, enrolled, markedToday, okN, failedN int64
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM faculty),
		(SELECT count(*) FROM faculty WHERE public_key IS NOT NULL),
		(SELECT count(*) FROM attendance WHERE day = ?),
		(SELECT COALESCE(SUM(outcome = 'ok'), 0)  FROM audit_log WHERE endpoint = 'attendance' AND ts >= ?),
		(SELECT COALESCE(SUM(outcome != 'ok'), 0) FROM audit_log WHERE endpoint = 'attendance' AND ts >= ?)`,
		today, now.Add(-24*time.Hour).Unix(), now.Add(-24*time.Hour).Unix()).
		Scan(&faculty, &enrolled, &markedToday, &okN, &failedN)
	if err != nil {
		return 0, nil, err
	}

	return 200, map[string]any{
		"version":         version,
		"running":         true, // the process answered; clients treat a network error as "down"
		"started_at":      s.startedAt.In(ist).Format(time.RFC3339),
		"uptime_s":        max(0, int64(now.Sub(s.startedAt).Seconds())),
		"server_time_ist": now.In(ist).Format(istLayout),
		"public_listener": map[string]any{
			"configured":    cfg.Listen,
			"reachable":     reachable,
			"loopback_only": loopback,
		},
		"admin_listener": map[string]any{"configured": cfg.AdminListen},
		"network": map[string]any{
			"hostname":   hostname,
			"addresses":  addrs,
			"phone_urls": urls,
			"warning":    warning,
		},
		"attendance_window": map[string]any{
			"open":               ws.Open,
			"reason":             ws.Reason,
			"start":              fmtHM(cfg.WinStart),
			"end":                fmtHM(cfg.WinEnd),
			"next_change_at_ist": next,
			"weekday":            now.In(ist).Weekday().String(),
		},
		"database": dbInfo,
		"counts": map[string]any{
			"faculty":      faculty,
			"enrolled":     enrolled,
			"marked_today": markedToday,
		},
		"attendance_attempts_24h": map[string]any{"ok": okN, "failed": failedN},
		"config_warnings":         s.warningList(),
	}, nil
}

func ymd(t time.Time) string { return t.Format(dayLayout) }
