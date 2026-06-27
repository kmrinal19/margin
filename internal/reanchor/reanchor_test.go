package reanchor

import (
	"testing"

	"github.com/kmrinal19/margin/internal/anchor"
	"github.com/kmrinal19/margin/internal/store"
)

func doc(blocks ...[2]string) *anchor.Doc {
	bs := make([]anchor.Block, len(blocks))
	for i, b := range blocks {
		bs[i] = anchor.Block{ID: b[0], Text: anchor.Normalize(b[1])}
	}
	return anchor.NewDoc(bs)
}

func TestDocLevelNeverOrphans(t *testing.T) {
	t.Parallel()
	d := doc([2]string{"b-1", "anything at all"})
	a := store.Anchor{BlockID: store.DocBlockID} // a document-level note
	got, orphaned := Resolve(d, a)
	if orphaned {
		t.Fatal("a document-level note must never orphan")
	}
	if got != a {
		t.Errorf("a doc-level note must be returned unchanged, got %+v", got)
	}
}

func TestSingleBlockHealAndOrphan(t *testing.T) {
	t.Parallel()
	d := doc([2]string{"b-1", "The quick brown fox jumps over."})

	// heals to the fresh span when present
	healed, orphaned := Resolve(d, store.Anchor{
		BlockID: "b-1", QuoteExact: "brown fox", QuotePrefix: "quick ", QuoteSuffix: " jumps", CharStart: 10, CharEnd: 19,
	})
	if orphaned || healed.QuoteExact != "brown fox" {
		t.Fatalf("expected clean heal, got orphaned=%v %+v", orphaned, healed)
	}

	// orphans when the quote is gone
	if _, orphaned := Resolve(d, store.Anchor{BlockID: "b-gone", QuoteExact: "nonexistent phrase here"}); !orphaned {
		t.Error("missing quote should orphan")
	}
}

func TestMultiBlockHealBothEndpoints(t *testing.T) {
	t.Parallel()
	// a selection spanning b-1 (head) .. b-2 (tail)
	d := doc(
		[2]string{"b-1", "Alpha section discusses the first concern in detail."},
		[2]string{"b-2", "Beta section covers the second concern thoroughly."},
	)
	a := store.Anchor{
		BlockID:    "b-1",
		EndBlockID: "b-2",
		QuoteExact: "first concern in detail.", // head, in b-1
		QuoteTail:  "Beta section covers the",  // tail, in b-2
		CharStart:  27,
		CharEnd:    23,
	}
	healed, orphaned := Resolve(d, a)
	if orphaned {
		t.Fatalf("both endpoints present should heal, got orphan")
	}
	if healed.BlockID != "b-1" || healed.EndBlockID != "b-2" {
		t.Errorf("multi-block endpoints = %s..%s, want b-1..b-2", healed.BlockID, healed.EndBlockID)
	}
}

func TestMultiBlockOrphansWhenTailGone(t *testing.T) {
	t.Parallel()
	// head still present, tail block's text replaced entirely
	d := doc(
		[2]string{"b-1", "Alpha section discusses the first concern in detail."},
		[2]string{"b-2new", "Totally unrelated replacement paragraph now."},
	)
	a := store.Anchor{
		BlockID: "b-1", EndBlockID: "b-2", QuoteExact: "first concern in detail.",
		QuoteTail: "Beta section covers the", CharStart: 27, CharEnd: 23,
	}
	if _, orphaned := Resolve(d, a); !orphaned {
		t.Fatal("multi-block must orphan if EITHER endpoint is gone (tail here)")
	}
}

func TestMultiBlockOrphansWhenInverted(t *testing.T) {
	t.Parallel()
	// head text physically appears AFTER the tail text in the document, so a naive
	// independent resolve would produce an inverted span — must orphan.
	d := doc(
		[2]string{"b-x", "zzz tailphrase here zzz"}, // tail resolves here (earlier)
		[2]string{"b-y", "yyy headphrase here yyy"}, // head resolves here (later)
	)
	a := store.Anchor{
		BlockID: "b-y", EndBlockID: "b-x",
		QuoteExact: "headphrase", QuoteTail: "tailphrase",
		CharStart: 4, CharEnd: 4,
	}
	if _, orphaned := Resolve(d, a); !orphaned {
		t.Fatal("an inverted multi-block span (head after tail) must orphan, not mis-anchor")
	}
}
