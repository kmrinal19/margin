package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
)

// TestCmdExport exercises the portable export end to end: it must render the doc,
// embed the comments, and — crucially — compute statuses with the SAME logic the
// live server uses, so a document-level note is never wrongly orphaned.
func TestCmdExport(t *testing.T) {
	docs, data := t.TempDir(), t.TempDir()
	const md = "## Intro\n\nThe quick brown fox jumps over the lazy dog.\n"
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	rnd, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	bid := ""
	for _, b := range rnd.Blocks([]byte(md)) {
		if strings.Contains(b.Text, "quick brown fox") {
			bid = b.ID
		}
	}
	ctx := context.Background()
	// (1) an anchored comment that still resolves
	if _, err := st.CreateThread(ctx, "guide", store.Anchor{BlockID: bid, QuoteExact: "quick brown fox"}, "human", "real?"); err != nil {
		t.Fatal(err)
	}
	// (2) an anchored comment whose quote is absent → must export as Orphaned
	if _, err := st.CreateThread(ctx, "guide", store.Anchor{BlockID: bid, QuoteExact: "nonexistent phrase"}, "human", "gone"); err != nil {
		t.Fatal(err)
	}
	// (3) a document-level note → must NEVER export as Orphaned (the regression)
	if _, err := st.CreateThread(ctx, "guide", store.Anchor{BlockID: store.DocBlockID}, "human", "overall: solid"); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	out := filepath.Join(t.TempDir(), "guide.html")
	if err := cmdExport([]string{"guide", "--docs", docs, "--data", data, "--out", out}); err != nil {
		t.Fatalf("cmdExport: %v", err)
	}
	html, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)

	if !strings.Contains(s, "quick brown fox") {
		t.Error("export is missing the rendered doc / anchored quote")
	}
	if !strings.Contains(s, "overall: solid") {
		t.Error("export is missing the document-level note")
	}
	// the doc-level note's body is present but it must NOT carry an Orphaned label;
	// exactly one thread (the nonexistent-phrase one) should be orphaned.
	if got := strings.Count(s, "Orphaned"); got != 1 {
		t.Errorf("expected exactly 1 orphaned thread in the export, got %d", got)
	}
}

func TestExportRejectsBadSlug(t *testing.T) {
	for _, bad := range []string{"../etc", "a/../b", "A/b", ""} {
		if err := cmdExport([]string{bad, "--docs", t.TempDir(), "--data", t.TempDir()}); err == nil {
			t.Errorf("cmdExport(%q) should reject an invalid slug", bad)
		}
	}
}
