package client_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kmrinal19/margin/internal/client"
	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/server"
	"github.com/kmrinal19/margin/internal/store"
)

func TestClientRoundTrip(t *testing.T) {
	ctx := context.Background()
	docs := t.TempDir()
	data := t.TempDir()
	const md = "## Heading\n\nThe quick brown fox jumps over the lazy dog.\n"
	if err := os.WriteFile(filepath.Join(docs, "d.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	rnd, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := server.New(server.Config{Host: "127.0.0.1", DocsDir: docs, DataDir: data},
		slog.New(slog.NewTextHandler(io.Discard, nil)), rnd, st)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	c := client.New(ts.URL)

	// docs lists the file
	docList, err := c.Docs(ctx)
	if err != nil {
		t.Fatalf("Docs: %v", err)
	}
	if len(docList) != 1 || docList[0].Slug != "d" {
		t.Fatalf("Docs = %+v", docList)
	}

	// seed a thread (the CLI never creates threads; that's the browser's job)
	bid := ""
	for _, b := range rnd.Blocks([]byte(md)) {
		if strings.Contains(b.Text, "quick brown fox") {
			bid = b.ID
		}
	}
	if bid == "" {
		t.Fatal("could not find block id")
	}
	th, err := st.CreateThread(ctx, "d", store.Anchor{
		BlockID: bid, QuoteExact: "quick brown fox",
	}, "human", "what fox?")
	if err != nil {
		t.Fatal(err)
	}

	// Comments returns the open thread with re-resolved anchor
	got, err := c.Comments(ctx, "d", "open")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != 1 || got[0].Comments[0].Body != "what fox?" {
		t.Fatalf("Comments = %+v", got)
	}
	if got[0].Orphaned {
		t.Error("thread should resolve cleanly, not orphan")
	}

	// SetStatus resolves it
	rt, err := c.SetStatus(ctx, th.ID, store.StatusResolved, "ai", "it's a metaphor")
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if rt.Status != store.StatusResolved {
		t.Fatalf("status = %q", rt.Status)
	}
	open, _ := c.Comments(ctx, "d", "open")
	if len(open) != 0 {
		t.Errorf("want 0 open after resolve, got %d", len(open))
	}

	// unreachable server yields a clear error
	if _, err := client.New("http://127.0.0.1:1").Docs(ctx); err == nil {
		t.Error("expected error reaching a dead server")
	}
}
