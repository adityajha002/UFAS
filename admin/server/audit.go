package main

import (
	"context"
	"database/sql"
	"log"
	"time"
)

const pruneChunk = 5000

// pruneLoop prunes at startup and then hourly until ctx is cancelled.
func (s *Server) pruneLoop(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		n, err := s.pruneAudit(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Printf("audit prune: %v", err)
		case n > 0:
			log.Printf("audit prune: removed %d rows", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// pruneAudit deletes rows older than retention_days, then the oldest overflow
// beyond max_rows. 0 disables the respective limit. Deletes run in chunks so
// phones are never stuck behind one long delete on the single connection.
func (s *Server) pruneAudit(ctx context.Context) (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	cfg := s.config()
	var total int64
	if cfg.AuditRetentionDays > 0 {
		cutoff := s.now().Add(-time.Duration(cfg.AuditRetentionDays) * 24 * time.Hour).Unix()
		n, err := deleteChunks(ctx, s.db,
			`DELETE FROM audit_log WHERE id IN (SELECT id FROM audit_log WHERE ts < ? LIMIT ?)`, cutoff)
		total += n
		if err != nil {
			return total, err
		}
	}
	if cfg.AuditMaxRows > 0 {
		// everything at or below the (max_rows+1)-th newest id is overflow
		n, err := deleteChunks(ctx, s.db,
			`DELETE FROM audit_log WHERE id IN (SELECT id FROM audit_log
				WHERE id <= (SELECT id FROM audit_log ORDER BY id DESC LIMIT 1 OFFSET ?) LIMIT ?)`,
			cfg.AuditMaxRows)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func deleteChunks(ctx context.Context, db *sql.DB, q string, arg any) (int64, error) {
	var total int64
	for {
		res, err := db.ExecContext(ctx, q, arg, pruneChunk)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < pruneChunk {
			return total, nil
		}
		select { // let phone requests use the connection between chunks
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
