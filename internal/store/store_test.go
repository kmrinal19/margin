package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleAnchor() Anchor {
	return Anchor{
		BlockID:     "b-abc12345",
		QuoteExact:  "the main fee",
		QuotePrefix: "paid ",
		QuoteSuffix: " today",
		CharStart:   5,
		CharEnd:     17,
	}
}

func TestCreateListResolveReopen(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	th, err := s.CreateThread(ctx, "doc1", sampleAnchor(), "human", "is this right?")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if th.ID == 0 || th.Status != StatusOpen || len(th.Comments) != 1 {
		t.Fatalf("unexpected thread: %+v", th)
	}
	if th.Anchor.Confidence != 1.0 {
		t.Errorf("default confidence = %v, want 1.0", th.Anchor.Confidence)
	}
	if th.Anchor.QuoteExact != "the main fee" {
		t.Errorf("anchor not round-tripped: %+v", th.Anchor)
	}

	if _, err := s.AddReply(ctx, th.ID, "ai", "fixed it"); err != nil {
		t.Fatalf("AddReply: %v", err)
	}
	got, err := s.GetThread(ctx, th.ID)
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if len(got.Comments) != 2 {
		t.Fatalf("want 2 comments, got %d", len(got.Comments))
	}

	if err := s.SetStatus(ctx, th.ID, StatusResolved, "ai", "done; changed wording"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, _ = s.GetThread(ctx, th.ID)
	if got.Status != StatusResolved || got.ResolvedAt == nil || got.ResolvedBy != "ai" {
		t.Fatalf("not resolved: %+v", got)
	}
	if len(got.Comments) != 3 {
		t.Fatalf("resolution note should add a comment; got %d", len(got.Comments))
	}

	if openThreads, _ := s.ListThreads(ctx, "doc1", FilterOpen); len(openThreads) != 0 {
		t.Errorf("want 0 open, got %d", len(openThreads))
	}
	if resolved, _ := s.ListThreads(ctx, "doc1", FilterResolved); len(resolved) != 1 {
		t.Errorf("want 1 resolved, got %d", len(resolved))
	}

	if err := s.SetStatus(ctx, th.ID, StatusOpen, "human", ""); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, _ = s.GetThread(ctx, th.ID)
	if got.Status != StatusOpen || got.ResolvedAt != nil {
		t.Fatalf("not reopened: %+v", got)
	}

	if counts, _ := s.OpenCounts(ctx); counts["doc1"] != 1 {
		t.Errorf("open count = %d, want 1", counts["doc1"])
	}
}

func TestAddReplyMissingThread(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.AddReply(context.Background(), 999, "human", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	// foreign_keys must be ON per-connection: a comment for a missing thread fails.
	if _, err := s.writer.Exec(
		`INSERT INTO comment(thread_id, author, body, created_at) VALUES(123, 'x', 'y', 'z')`,
	); err == nil {
		t.Error("expected a foreign-key violation, got nil")
	}
}

func TestUpdateAnchorResolution(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	th, err := s.CreateThread(ctx, "doc1", sampleAnchor(), "human", "c")
	if err != nil {
		t.Fatal(err)
	}
	healed := th.Anchor
	healed.BlockID = "b-99999999"
	healed.CharStart, healed.CharEnd = 40, 52
	healed.Confidence = 0.7
	if err := s.UpdateAnchorResolution(ctx, th.ID, healed, false); err != nil {
		t.Fatalf("UpdateAnchorResolution: %v", err)
	}
	got, _ := s.GetThread(ctx, th.ID)
	if got.Anchor.BlockID != "b-99999999" || got.Anchor.CharStart != 40 || got.Anchor.Confidence != 0.7 {
		t.Errorf("anchor not healed: %+v", got.Anchor)
	}
	if got.Orphaned {
		t.Error("should not be orphaned")
	}
}

// TestConcurrentWrites is the core safety test: many simultaneous writers (the
// "multiple AI sessions + browser" case) must never hit "database is locked".
func TestConcurrentWrites(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	const writers = 60

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := sampleAnchor()
			a.BlockID = fmt.Sprintf("b-%08x", i)
			if _, err := s.CreateThread(ctx, "concurrent", a, "human", fmt.Sprintf("comment %d", i)); err != nil {
				errs <- err
			}
			// interleave reads to exercise the reader pool under write load
			if _, err := s.ListThreads(ctx, "concurrent", FilterAll); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent op failed: %v", err)
	}

	all, err := s.ListThreads(ctx, "concurrent", FilterAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != writers {
		t.Errorf("want %d threads, got %d", writers, len(all))
	}
}
