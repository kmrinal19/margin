// Package store is the SQLite-backed comment store. It opens two pools to one
// database file — a single-connection writer (serializing writes, no SQLITE_BUSY)
// and a multi-connection reader (parallel under WAL) — which is what lets many
// concurrent AI sessions and the browser hit it at once safely.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

//go:embed schema.sql
var schemaSQL string

// ErrNotFound is returned when a thread (or doc) does not exist.
var ErrNotFound = errors.New("not found")

// Store holds the writer and reader connection pools.
type Store struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

// dsnPragmas are applied to every connection. foreign_keys(ON) is required
// per-connection or ON DELETE CASCADE silently no-ops.
const dsnPragmas = "_pragma=journal_mode(WAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=temp_store(MEMORY)"

// Open opens (creating if needed) the comment store under dataDir/margin.db.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "margin.db")
	dsn := "file:" + dbPath + "?" + dsnPragmas

	// Writer: exactly one connection; BEGIN IMMEDIATE via _txlock so a
	// read-then-write transaction can't hit an instant SQLITE_BUSY.
	writer, err := sql.Open("sqlite", dsn+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)
	writer.SetConnMaxIdleTime(0)

	// Reader: a small pool; WAL lets these run in parallel with the writer.
	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	n := min(max(runtime.NumCPU(), 4), 8)
	reader.SetMaxOpenConns(n)
	reader.SetMaxIdleConns(n)
	reader.SetConnMaxLifetime(0)
	reader.SetConnMaxIdleTime(0)

	s := &Store{writer: writer, reader: reader, path: dbPath}
	if err := s.migrate(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrate: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate (%.40s…): %w", strings.TrimSpace(stmt), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrate: %w", err)
	}
	return nil
}

// Close checkpoints the WAL and closes both pools.
func (s *Store) Close() error {
	if s.writer != nil {
		_, _ = s.writer.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	}
	var errs []error
	if s.writer != nil {
		errs = append(errs, s.writer.Close())
	}
	if s.reader != nil {
		errs = append(errs, s.reader.Close())
	}
	return errors.Join(errs...)
}

// splitStatements splits a schema file into individual statements. Comments are
// stripped FIRST (so a ';' inside a comment can't split a statement, and so
// modernc's parser doesn't reject leading/inline -- comments), then it splits on
// the remaining real semicolons.
func splitStatements(s string) []string {
	parts := strings.Split(stripSQLComments(s), ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if clean := strings.TrimSpace(p); clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func stripSQLComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func nowStr() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
