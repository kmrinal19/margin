package anchor

import (
	"math"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Matching tuning. A tight threshold prefers an honest orphan over a confident
// mis-anchor (PRD G3). Patterns longer than this many runes skip fuzzy matching
// (the Bitap bitmask is bounded), falling back to exact tiers.
const (
	matchThreshold = 0.35
	matchDistance  = 1000
	maxFuzzyRunes  = 32
	contextRunes   = 32
)

// Block is a document block's stable id and its normalized plain text.
type Block struct {
	ID   string
	Text string
}

// Stored is a thread's persisted anchor, as the cascade input.
type Stored struct {
	BlockID    string
	Prefix     string
	Exact      string
	Suffix     string
	Start, End int // advisory hints
}

// Result is the outcome of re-resolving a Stored anchor against a Doc.
type Result struct {
	BlockID    string
	Exact      string // the actual healed span text; keeps the stored anchor self-consistent
	Prefix     string
	Suffix     string
	Start, End int
	Confidence float64
	Tier       int  // 1..4; 0 when orphaned
	OK         bool // false => orphan
}

// Doc is a parsed document prepared for re-anchoring: per-block normalized text
// plus a concatenation for whole-doc fallback searches.
type Doc struct {
	ids        []string
	texts      [][]rune
	byID       map[string]int
	whole      []rune
	wholeStr   string // string(whole), built once for the tier-4 fuzzy haystack
	blockStart []int  // rune offset of each block within whole
}

// NewDoc builds a Doc from ordered blocks (id + normalized text).
func NewDoc(blocks []Block) *Doc {
	d := &Doc{byID: make(map[string]int, len(blocks))}
	for _, b := range blocks {
		r := []rune(b.Text)
		if _, dup := d.byID[b.ID]; !dup {
			d.byID[b.ID] = len(d.ids)
		}
		d.ids = append(d.ids, b.ID)
		d.texts = append(d.texts, r)
		d.blockStart = append(d.blockStart, len(d.whole))
		d.whole = append(d.whole, r...)
		d.whole = append(d.whole, '\n') // separator; normalized text has no newlines
	}
	d.wholeStr = string(d.whole) // hoist out of the per-thread tier-4 fuzzy path
	return d
}

// Resolve re-resolves a stored anchor against the current document via the
// 4-tier cascade. First validated hit wins; otherwise the thread is an orphan.
func (d *Doc) Resolve(s Stored) Result {
	exact := []rune(Normalize(s.Exact))
	if len(exact) == 0 {
		return Result{}
	}
	prefix := []rune(Normalize(s.Prefix))
	suffix := []rune(Normalize(s.Suffix))

	// Tier 1 & 2 — same block still present.
	if bi, ok := d.byID[s.BlockID]; ok {
		t := d.texts[bi]
		if at := nearestExact(t, exact, s.Start); at >= 0 {
			return healInBlock(s.BlockID, t, at, exact, 1.0, 1) // tier 1: exact in block
		}
		// tier 2: fuzzy in block — require the matched span to fit inside the block
		// (a match running off the end is degenerate; orphan instead) AND the FULL
		// sliced span to resemble the full quote. fuzzyRunes only matches the leading
		// window of a long quote to find the start, so without this the unvalidated
		// tail could heal onto the wrong text — a mis-anchor (the cardinal sin).
		if at := fuzzyRunes(t, exact, s.Start); at >= 0 && at+len(exact) <= len(t) && similarEnough(t[at:at+len(exact)], exact) {
			return healInBlock(s.BlockID, t, at, exact, 0.8, 2)
		}
	}

	// Tier 3 — exact quote elsewhere in the doc, disambiguated by context.
	if at := d.findExactContext(prefix, exact, suffix); at >= 0 {
		return d.healGlobal(at, exact, 0.7, 3)
	}

	// Tier 4 — last-resort fuzzy search across the whole doc (uses the cached
	// whole-doc string so a corpus of drifted threads doesn't re-stringify it).
	if at := fuzzyAt(d.wholeStr, exact, d.globalHint(s)); at >= 0 {
		return d.healGlobal(at, exact, 0.5, 4)
	}

	return Result{OK: false}
}

func (d *Doc) globalHint(s Stored) int {
	if bi, ok := d.byID[s.BlockID]; ok {
		return d.blockStart[bi] + s.Start
	}
	return s.Start
}

func healInBlock(blockID string, text []rune, at int, exact []rune, conf float64, tier int) Result {
	start, end := at, at+len(exact)
	if end > len(text) {
		end = len(text)
	}
	return Result{
		BlockID:    blockID,
		Exact:      sliceRunes(text, start, end),
		Start:      start,
		End:        end,
		Prefix:     sliceRunes(text, start-contextRunes, start),
		Suffix:     sliceRunes(text, end, end+contextRunes),
		Confidence: conf,
		Tier:       tier,
		OK:         true,
	}
}

func (d *Doc) healGlobal(at int, exact []rune, conf float64, tier int) Result {
	bi := d.blockAt(at)
	if bi < 0 {
		return Result{OK: false}
	}
	rel := at - d.blockStart[bi]
	// Reject a match that crosses a block boundary / the '\n' separator (which
	// would attach to the wrong block, possibly as a degenerate span).
	if rel < 0 || rel+len(exact) > len(d.texts[bi]) {
		return Result{OK: false}
	}
	// Validate the FULL sliced span against the quote — tier 4 fuzzy only matched
	// the leading window, so the tail must be re-checked (prefer orphan over a
	// confident mis-anchor). Tier 3 spans are exact, so this passes trivially.
	if !similarEnough(d.texts[bi][rel:rel+len(exact)], exact) {
		return Result{OK: false}
	}
	return healInBlock(d.ids[bi], d.texts[bi], rel, exact, conf, tier)
}

// Before reports whether document position (aBlock, aOff) strictly precedes
// (bBlock, bOff). Used to reject an inverted multi-block span (head after tail).
// Unknown blocks are treated as not-before (caller should orphan).
func (d *Doc) Before(aBlock string, aOff int, bBlock string, bOff int) bool {
	ai, aok := d.byID[aBlock]
	bi, bok := d.byID[bBlock]
	if !aok || !bok {
		return false
	}
	return d.blockStart[ai]+aOff < d.blockStart[bi]+bOff
}

func (d *Doc) blockAt(at int) int {
	found := -1
	for i, start := range d.blockStart {
		if start <= at {
			found = i
		} else {
			break
		}
	}
	return found
}

// findExactContext finds the occurrence of exact in the whole doc whose
// surrounding text best matches the stored prefix/suffix.
func (d *Doc) findExactContext(prefix, exact, suffix []rune) int {
	idxs := allIndex(d.whole, exact)
	if len(idxs) == 0 {
		return -1
	}
	if len(idxs) == 1 {
		return idxs[0]
	}
	best, bestScore := idxs[0], -1
	for _, i := range idxs {
		score := matchBackward(d.whole, i, prefix) + matchForward(d.whole, i+len(exact), suffix)
		if score > bestScore {
			bestScore, best = score, i
		}
	}
	return best
}

// similarEnough reports whether a candidate span is close enough to the stored
// quote to heal onto (rather than orphan). It bounds the Levenshtein edit
// distance to matchThreshold of the quote length — the full-span analogue of the
// Bitap threshold, so the unvalidated tail of a long fuzzy match can't mis-anchor.
func similarEnough(span, exact []rune) bool {
	if len(exact) == 0 {
		return false
	}
	if runesEqual(span, exact) {
		return true // tier-1/3 exact spans, the common case — skip the diff
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(span), string(exact), false)
	return float64(dmp.DiffLevenshtein(diffs)) <= matchThreshold*float64(len(exact))
}

func fuzzyRunes(hay, needle []rune, hint int) int {
	return fuzzyAt(string(hay), needle, hint)
}

// fuzzyAt Bitap-matches needle's leading window within the haystack string. The
// bitmask bounds the pattern length, so for a long quote we match only its
// leading window to find the START; the caller derives the span length from the
// full quote (and re-validates it via similarEnough). hay is taken as a string
// so callers with a hot, reusable haystack (the whole doc) can hoist string().
func fuzzyAt(hay string, needle []rune, hint int) int {
	if len(needle) == 0 {
		return -1
	}
	pat := needle
	if len(pat) > maxFuzzyRunes {
		pat = pat[:maxFuzzyRunes]
	}
	if hint < 0 {
		hint = 0
	}
	dmp := diffmatchpatch.New()
	dmp.MatchThreshold = matchThreshold
	dmp.MatchDistance = matchDistance
	return dmp.MatchMain(hay, string(pat), hint)
}

// ── rune helpers ────────────────────────────────────────────────────────────

func nearestExact(hay, needle []rune, hint int) int {
	idxs := allIndex(hay, needle)
	if len(idxs) == 0 {
		return -1
	}
	best, bestDist := idxs[0], math.MaxInt
	for _, i := range idxs {
		dst := i - hint
		if dst < 0 {
			dst = -dst
		}
		if dst < bestDist {
			bestDist, best = dst, i
		}
	}
	return best
}

func allIndex(hay, needle []rune) []int {
	var out []int
	if len(needle) == 0 || len(needle) > len(hay) {
		return out
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		if runesEqual(hay[i:i+len(needle)], needle) {
			out = append(out, i)
		}
	}
	return out
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// matchBackward counts how many trailing runes of pat match the text ending at
// pos. A single boundary space is skipped because Normalize trims the stored
// prefix's trailing whitespace while the doc text keeps the inter-word space.
func matchBackward(text []rune, pos int, pat []rune) int {
	if pos > 0 && text[pos-1] == ' ' {
		pos--
	}
	n := 0
	for n < len(pat) && pos-1-n >= 0 && text[pos-1-n] == pat[len(pat)-1-n] {
		n++
	}
	return n
}

// matchForward counts how many leading runes of pat match the text starting at
// pos, skipping a single boundary space (see matchBackward).
func matchForward(text []rune, pos int, pat []rune) int {
	if pos < len(text) && text[pos] == ' ' {
		pos++
	}
	n := 0
	for n < len(pat) && pos+n < len(text) && text[pos+n] == pat[n] {
		n++
	}
	return n
}

func sliceRunes(r []rune, from, to int) string {
	if from < 0 {
		from = 0
	}
	if to > len(r) {
		to = len(r)
	}
	if from >= to {
		return ""
	}
	return string(r[from:to])
}
