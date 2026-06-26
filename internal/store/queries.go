package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CreateThread creates a doc (if needed), thread, anchor, and first comment in
// one write transaction, and returns the resulting thread.
func (s *Store) CreateThread(ctx context.Context, slug string, a Anchor, author, body string) (Thread, error) {
	if a.Confidence == 0 {
		a.Confidence = 1.0
	}
	ts := nowStr()

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return Thread{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	docID, err := ensureDocTx(ctx, tx, slug)
	if err != nil {
		return Thread{}, err
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO comment_thread(doc_id, status, orphaned, created_at) VALUES(?, ?, 0, ?)`,
		docID, StatusOpen, ts)
	if err != nil {
		return Thread{}, fmt.Errorf("insert thread: %w", err)
	}
	threadID, err := res.LastInsertId()
	if err != nil {
		return Thread{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO anchor(thread_id, block_id, end_block_id, quote_exact, quote_tail, quote_prefix, quote_suffix, char_start, char_end, last_confidence)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		threadID, a.BlockID, endBlockID(a), a.QuoteExact, a.QuoteTail, a.QuotePrefix, a.QuoteSuffix, a.CharStart, a.CharEnd, a.Confidence,
	); err != nil {
		return Thread{}, fmt.Errorf("insert anchor: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO comment(thread_id, author, body, created_at) VALUES(?, ?, ?, ?)`,
		threadID, author, body, ts,
	); err != nil {
		return Thread{}, fmt.Errorf("insert comment: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Thread{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetThread(ctx, threadID)
}

// endBlockID returns the anchor's end block, defaulting to the start block for a
// single-block selection.
func endBlockID(a Anchor) string {
	if a.EndBlockID == "" {
		return a.BlockID
	}
	return a.EndBlockID
}

func ensureDocTx(ctx context.Context, tx *sql.Tx, slug string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`INSERT INTO doc(slug) VALUES(?) ON CONFLICT(slug) DO UPDATE SET slug=excluded.slug RETURNING id`,
		slug).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure doc: %w", err)
	}
	return id, nil
}

// GetThread returns a single thread with its anchor and comments.
func (s *Store) GetThread(ctx context.Context, id int64) (Thread, error) {
	ts, err := s.queryThreads(ctx, "WHERE t.id = ?", id)
	if err != nil {
		return Thread{}, err
	}
	if len(ts) == 0 {
		return Thread{}, ErrNotFound
	}
	return ts[0], nil
}

// ListThreads returns the threads for a doc, filtered by status.
func (s *Store) ListThreads(ctx context.Context, slug string, filter Filter) ([]Thread, error) {
	where := "WHERE d.slug = ?"
	switch filter {
	case FilterOpen:
		where += " AND t.status = 'open'"
	case FilterResolved:
		where += " AND t.status = 'resolved'"
	}
	return s.queryThreads(ctx, where, slug)
}

func (s *Store) queryThreads(ctx context.Context, where string, args ...any) ([]Thread, error) {
	const cols = `t.id, d.slug, t.status, t.orphaned, t.created_at, t.resolved_at, t.resolved_by,
		a.block_id, a.end_block_id, a.quote_exact, a.quote_tail, a.quote_prefix, a.quote_suffix, a.char_start, a.char_end, a.last_confidence`
	q := `SELECT ` + cols + `
		FROM comment_thread t
		JOIN doc d ON d.id = t.doc_id
		JOIN anchor a ON a.thread_id = t.id ` + where + ` ORDER BY t.id`

	rows, err := s.reader.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query threads: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var threads []Thread
	var ids []int64
	for rows.Next() {
		var (
			t                      Thread
			orph                   int
			created                string
			resolvedAt, resolvedBy sql.NullString
			endBlock, tail         sql.NullString
			prefix, suffix         sql.NullString
			cstart, cend           sql.NullInt64
			conf                   sql.NullFloat64
		)
		if err := rows.Scan(&t.ID, &t.DocSlug, &t.Status, &orph, &created, &resolvedAt, &resolvedBy,
			&t.Anchor.BlockID, &endBlock, &t.Anchor.QuoteExact, &tail, &prefix, &suffix, &cstart, &cend, &conf); err != nil {
			return nil, fmt.Errorf("scan thread: %w", err)
		}
		t.Orphaned = orph != 0
		t.CreatedAt = parseTime(created)
		if resolvedAt.Valid {
			tm := parseTime(resolvedAt.String)
			t.ResolvedAt = &tm
		}
		t.ResolvedBy = resolvedBy.String
		// Pre-migration rows have NULL end_block_id → single-block (end = start).
		if endBlock.Valid && endBlock.String != "" {
			t.Anchor.EndBlockID = endBlock.String
		} else {
			t.Anchor.EndBlockID = t.Anchor.BlockID
		}
		t.Anchor.QuoteTail = tail.String
		t.Anchor.QuotePrefix = prefix.String
		t.Anchor.QuoteSuffix = suffix.String
		t.Anchor.CharStart = int(cstart.Int64)
		t.Anchor.CharEnd = int(cend.Int64)
		t.Anchor.Confidence = conf.Float64
		threads = append(threads, t)
		ids = append(ids, t.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate threads: %w", err)
	}

	byThread, err := s.loadComments(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range threads {
		threads[i].Comments = byThread[threads[i].ID]
	}
	return threads, nil
}

func (s *Store) loadComments(ctx context.Context, ids []int64) (map[int64][]Comment, error) {
	out := make(map[int64][]Comment, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	q := `SELECT thread_id, id, author, body, created_at FROM comment
		WHERE thread_id IN (` + strings.Join(ph, ",") + `) ORDER BY id`
	rows, err := s.reader.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query comments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			tid     int64
			c       Comment
			created string
		)
		if err := rows.Scan(&tid, &c.ID, &c.Author, &c.Body, &created); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		c.CreatedAt = parseTime(created)
		out[tid] = append(out[tid], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comments: %w", err)
	}
	return out, nil
}

// AddReply appends a comment to an existing thread.
func (s *Store) AddReply(ctx context.Context, threadID int64, author, body string) (Comment, error) {
	ts := nowStr()
	res, err := s.writer.ExecContext(ctx,
		`INSERT INTO comment(thread_id, author, body, created_at)
		 SELECT ?, ?, ?, ? WHERE EXISTS(SELECT 1 FROM comment_thread WHERE id = ?)`,
		threadID, author, body, ts, threadID)
	if err != nil {
		return Comment{}, fmt.Errorf("insert reply: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Comment{}, ErrNotFound
	}
	id, _ := res.LastInsertId()
	return Comment{ID: id, Author: author, Body: body, CreatedAt: parseTime(ts)}, nil
}

// SetStatus resolves or reopens a thread, optionally recording a note as a reply
// (used for AI resolution notes). by attributes the action.
func (s *Store) SetStatus(ctx context.Context, threadID int64, status, by, note string) error {
	if status != StatusOpen && status != StatusResolved {
		return fmt.Errorf("invalid status %q", status)
	}
	ts := nowStr()

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if strings.TrimSpace(note) != "" {
		author := by
		if author == "" {
			author = "ai"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO comment(thread_id, author, body, created_at)
			 SELECT ?, ?, ?, ? WHERE EXISTS(SELECT 1 FROM comment_thread WHERE id = ?)`,
			threadID, author, note, ts, threadID); err != nil {
			return fmt.Errorf("insert note: %w", err)
		}
	}

	var res sql.Result
	if status == StatusResolved {
		res, err = tx.ExecContext(ctx,
			`UPDATE comment_thread SET status='resolved', resolved_at=?, resolved_by=? WHERE id=?`,
			ts, by, threadID)
	} else {
		res, err = tx.ExecContext(ctx,
			`UPDATE comment_thread SET status='open', resolved_at=NULL, resolved_by=NULL WHERE id=?`,
			threadID)
	}
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// HealRow is one thread's re-resolved anchor plus its orphaned flag.
type HealRow struct {
	ThreadID int64
	Anchor   Anchor
	Orphaned bool
}

// UpdateAnchorResolution rewrites a single thread's stored anchor after the
// cascade healed it, and updates its orphaned flag.
func (s *Store) UpdateAnchorResolution(ctx context.Context, threadID int64, a Anchor, orphaned bool) error {
	return s.UpdateAnchorResolutions(ctx, []HealRow{{ThreadID: threadID, Anchor: a, Orphaned: orphaned}})
}

// UpdateAnchorResolutions persists a batch of healed anchors in ONE write
// transaction — so a GET that re-resolves many drifted threads occupies the
// single writer once, not N times.
func (s *Store) UpdateAnchorResolutions(ctx context.Context, rows []HealRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, r := range rows {
		if _, err := tx.ExecContext(ctx,
			`UPDATE anchor SET block_id=?, end_block_id=?, quote_exact=?, quote_tail=?, quote_prefix=?, quote_suffix=?, char_start=?, char_end=?, last_confidence=? WHERE thread_id=?`,
			r.Anchor.BlockID, endBlockID(r.Anchor), r.Anchor.QuoteExact, r.Anchor.QuoteTail, r.Anchor.QuotePrefix, r.Anchor.QuoteSuffix,
			r.Anchor.CharStart, r.Anchor.CharEnd, r.Anchor.Confidence, r.ThreadID); err != nil {
			return fmt.Errorf("update anchor: %w", err)
		}
		orphInt := 0
		if r.Orphaned {
			orphInt = 1
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE comment_thread SET orphaned=? WHERE id=?`, orphInt, r.ThreadID); err != nil {
			return fmt.Errorf("update orphaned: %w", err)
		}
	}
	return tx.Commit()
}

// OpenCounts returns the number of open threads per doc slug.
func (s *Store) OpenCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT d.slug, COUNT(*) FROM comment_thread t
		 JOIN doc d ON d.id = t.doc_id
		 WHERE t.status='open' GROUP BY d.slug`)
	if err != nil {
		return nil, fmt.Errorf("open counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var (
			slug string
			n    int
		)
		if err := rows.Scan(&slug, &n); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		out[slug] = n
	}
	return out, rows.Err()
}
