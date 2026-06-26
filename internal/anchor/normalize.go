// Package anchor implements stable block identity and the re-anchoring cascade
// that lets comments survive document edits.
//
// normalize() and BlockID() are shared by internal/render (which stamps block
// IDs at render time) and the cascade here (which re-resolves them). They MUST
// stay byte-identical across both call sites, or every comment silently orphans.
package anchor

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Normalize canonicalizes block text for hashing and comparison: NFC Unicode
// normalization, then collapse every run of whitespace to a single space and trim.
func Normalize(s string) string {
	s = norm.NFC.String(s)
	return strings.Join(strings.Fields(s), " ")
}

// BlockID returns the stable content-hash identifier for a block of text:
// "b-" followed by the first 8 hex chars of SHA-256(Normalize(text)).
// Editing a block changes its text and therefore its ID; unchanged blocks keep it.
func BlockID(text string) string {
	sum := sha256.Sum256([]byte(Normalize(text)))
	return "b-" + hex.EncodeToString(sum[:])[:8]
}
