package anchor

import "testing"

// helper: build a Doc the way render.Blocks would (normalized text).
func doc(blocks ...[2]string) *Doc {
	bs := make([]Block, len(blocks))
	for i, b := range blocks {
		bs[i] = Block{ID: b[0], Text: Normalize(b[1])}
	}
	return NewDoc(bs)
}

func stored(blockID, prefix, exact, suffix string, start int) Stored {
	return Stored{BlockID: blockID, Prefix: prefix, Exact: exact, Suffix: suffix, Start: start, End: start + len(exact)}
}

func TestTier1ExactUnchanged(t *testing.T) {
	t.Parallel()
	d := doc([2]string{"b-1", "The quick brown fox jumps."}, [2]string{"b-2", "Lazy dog sleeps."})
	r := d.Resolve(stored("b-1", "The quick ", "brown fox", " jumps", 10))
	if !r.OK || r.Tier != 1 || r.BlockID != "b-1" {
		t.Fatalf("tier1 = %+v", r)
	}
	if r.Confidence != 1.0 {
		t.Errorf("confidence = %v", r.Confidence)
	}
}

func TestTier2FuzzyInBlock(t *testing.T) {
	t.Parallel()
	// "brown fox" -> "brown fix" (one-char edit); same block id still present.
	d := doc([2]string{"b-1", "The quick brown fix jumps."})
	r := d.Resolve(stored("b-1", "The quick ", "brown fox", " jumps", 10))
	if !r.OK || r.Tier != 2 {
		t.Fatalf("expected tier 2 fuzzy, got %+v", r)
	}
}

func TestTier3ExactMovedToNewBlock(t *testing.T) {
	t.Parallel()
	// The original block id is gone (content edited -> new hash). The exact quote
	// now lives in a different block; tier 3 should find and re-home it.
	d := doc(
		[2]string{"b-new", "Intro paragraph changed entirely."},
		[2]string{"b-2", "Later we mention the main semester fee in full."},
	)
	r := d.Resolve(stored("b-gone", "mention the ", "main semester fee", " in full", 0))
	if !r.OK || r.Tier != 3 || r.BlockID != "b-2" {
		t.Fatalf("expected tier 3 -> b-2, got %+v", r)
	}
}

func TestTier3ContextDisambiguation(t *testing.T) {
	t.Parallel()
	// "the fee" appears twice; prefix/suffix should pick the right one.
	d := doc(
		[2]string{"b-1", "Pay the fee at registration."},
		[2]string{"b-2", "Refund the fee on withdrawal."},
	)
	r := d.Resolve(stored("b-gone", "Refund ", "the fee", " on withdrawal", 0))
	if !r.OK || r.BlockID != "b-2" {
		t.Fatalf("context should pick b-2, got %+v", r)
	}
}

func TestOrphanWhenGone(t *testing.T) {
	t.Parallel()
	d := doc([2]string{"b-1", "Completely different content now."})
	r := d.Resolve(stored("b-gone", "paid the ", "main semester fee", " before", 0))
	if r.OK {
		t.Fatalf("expected orphan, got %+v", r)
	}
}

func TestPreferOrphanOverMisAnchor(t *testing.T) {
	t.Parallel()
	// A loosely-similar but semantically different sentence must NOT be matched.
	d := doc([2]string{"b-1", "The annual membership discount applies to students."})
	r := d.Resolve(stored("b-gone", "", "main semester fee", "", 0))
	if r.OK {
		t.Fatalf("should prefer orphan over mis-anchor, got %+v", r)
	}
}

func TestHealRecomputesContext(t *testing.T) {
	t.Parallel()
	d := doc([2]string{"b-1", "alpha beta gamma delta epsilon"})
	r := d.Resolve(stored("b-1", "alpha ", "beta gamma", " delta", 6))
	if !r.OK {
		t.Fatal("expected hit")
	}
	if r.Prefix != "alpha " {
		t.Errorf("prefix = %q", r.Prefix)
	}
	if r.Suffix != " delta epsilon" {
		t.Errorf("suffix = %q", r.Suffix)
	}
}

func TestHealSetsExactToTheActualSpan(t *testing.T) {
	t.Parallel()
	// Fuzzy heal in-block: the stored quote drifted; the healed Exact must be the
	// real current span so the anchor stays self-consistent.
	d := doc([2]string{"b-1", "The quick brown fix jumps."})
	r := d.Resolve(stored("b-1", "quick ", "brown fox", " jumps", 10))
	if !r.OK || r.Tier != 2 {
		t.Fatalf("expected tier 2, got %+v", r)
	}
	if r.Exact != "brown fix" {
		t.Errorf("healed Exact = %q, want the actual span %q", r.Exact, "brown fix")
	}
}

func TestExactHealIsExact(t *testing.T) {
	t.Parallel()
	d := doc([2]string{"b-1", "alpha beta gamma"})
	r := d.Resolve(stored("b-1", "alpha ", "beta", " gamma", 6))
	if !r.OK || r.Exact != "beta" {
		t.Errorf("tier-1 Exact = %q (ok=%v), want \"beta\"", r.Exact, r.OK)
	}
}

func TestTier4DoesNotMatchAcrossBlockSeparator(t *testing.T) {
	t.Parallel()
	// A quote that would only "match" by spanning the block boundary must orphan,
	// not attach a degenerate/cross-block span.
	d := doc(
		[2]string{"b-1", "aaaaaaaaaa"},
		[2]string{"b-2", "bbbbbbbbbb"},
	)
	r := d.Resolve(stored("b-gone", "", "aaaaaXbbbbb", "", 5))
	if r.OK {
		t.Fatalf("a cross-separator quote must orphan, got %+v", r)
	}
}

func TestNormalizationInsensitive(t *testing.T) {
	t.Parallel()
	// Block text has collapsed whitespace; stored quote has messy whitespace.
	d := doc([2]string{"b-1", "one two three four"})
	r := d.Resolve(stored("b-1", "one ", "two   three", " four", 4))
	if !r.OK || r.Tier != 1 {
		t.Fatalf("normalization should make this a tier-1 hit, got %+v", r)
	}
}
