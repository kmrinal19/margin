// Package reanchor re-resolves a stored thread anchor against a freshly parsed
// document. It is the single source of truth shared by the server (per-GET
// healing) and the CLI export, so the two can never diverge on how a comment's
// position — or its orphaned state — is computed.
package reanchor

import (
	"github.com/kmrinal19/margin/internal/anchor"
	"github.com/kmrinal19/margin/internal/store"
)

// Resolve re-resolves a (possibly multi-block, possibly document-level) anchor
// against doc, returning the healed anchor and whether it is now orphaned.
//
//   - A document-level note (no text span) is never orphaned and never rewritten.
//   - A single-block anchor heals to its freshly-found span, or orphans.
//   - A multi-block anchor resolves its head and tail independently and orphans
//     if EITHER endpoint can no longer be located; its confidence is the min.
func Resolve(doc *anchor.Doc, a store.Anchor) (store.Anchor, bool) {
	if a.IsDoc() {
		return a, false // document-level note: nothing to locate, never orphans
	}

	if !a.Multi() {
		res := doc.Resolve(anchor.Stored{
			BlockID: a.BlockID, Prefix: a.QuotePrefix, Exact: a.QuoteExact,
			Suffix: a.QuoteSuffix, Start: a.CharStart, End: a.CharEnd,
		})
		if !res.OK {
			return a, true
		}
		n := a
		n.BlockID, n.EndBlockID = res.BlockID, res.BlockID
		n.QuoteExact, n.QuoteTail = res.Exact, ""
		n.QuotePrefix, n.QuoteSuffix = res.Prefix, res.Suffix
		n.CharStart, n.CharEnd = res.Start, res.End
		n.Confidence = res.Confidence
		return n, false
	}

	head := doc.Resolve(anchor.Stored{
		BlockID: a.BlockID, Prefix: a.QuotePrefix, Exact: a.QuoteExact, Start: a.CharStart,
	})
	tail := doc.Resolve(anchor.Stored{
		BlockID: a.EndBlockID, Exact: a.QuoteTail, Suffix: a.QuoteSuffix, Start: a.CharEnd,
	})
	if !head.OK || !tail.OK {
		return a, true
	}
	// The head must precede the tail in document order; an inverted or
	// cross-relocated span is degenerate, so orphan rather than mis-anchor.
	if !doc.Before(head.BlockID, head.Start, tail.BlockID, tail.End) {
		return a, true
	}
	n := a
	n.BlockID, n.QuoteExact, n.QuotePrefix, n.CharStart = head.BlockID, head.Exact, head.Prefix, head.Start
	n.EndBlockID, n.QuoteTail, n.QuoteSuffix, n.CharEnd = tail.BlockID, tail.Exact, tail.Suffix, tail.End
	n.Confidence = min(head.Confidence, tail.Confidence)
	return n, false
}
