package store

import "time"

// Thread status values.
const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
)

// DocBlockID is the reserved block id for a document-level note — a thread with
// no text anchor. Real block ids are "b-<hash>", so "doc" can never collide.
// ScopeDoc is the matching create-request scope value.
const (
	DocBlockID = "doc"
	ScopeDoc   = "doc"
)

// Filter selects which threads ListThreads returns.
type Filter string

// Filter values for ListThreads.
const (
	FilterOpen     Filter = "open"
	FilterResolved Filter = "resolved"
	FilterAll      Filter = "all"
)

// Anchor locates a thread's commented span within a document (W3C-style,
// block-scoped). Offsets are block-relative, never document-global. A selection
// that spans blocks sets EndBlockID (and QuoteTail = the portion in the end
// block); QuoteExact is then the head portion in the start block.
type Anchor struct {
	BlockID     string  `json:"block_id"`
	EndBlockID  string  `json:"end_block_id,omitempty"`
	QuoteExact  string  `json:"quote_exact"`
	QuoteTail   string  `json:"quote_tail,omitempty"`
	QuotePrefix string  `json:"quote_prefix"`
	QuoteSuffix string  `json:"quote_suffix"`
	CharStart   int     `json:"char_start"`
	CharEnd     int     `json:"char_end"`
	Confidence  float64 `json:"confidence"`
}

// Multi reports whether the anchor spans more than one block.
func (a Anchor) Multi() bool { return a.EndBlockID != "" && a.EndBlockID != a.BlockID }

// IsDoc reports whether this is a document-level note (no text anchor).
func (a Anchor) IsDoc() bool { return a.BlockID == DocBlockID }

// Comment is one message within a thread.
type Comment struct {
	ID        int64      `json:"id"`
	Author    string     `json:"author"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	EditedAt  *time.Time `json:"edited_at,omitempty"` // set when the body was edited
}

// Thread is a comment thread anchored to a span of a document.
type Thread struct {
	ID         int64      `json:"id"`
	DocSlug    string     `json:"doc_slug"`
	Status     string     `json:"status"`
	Orphaned   bool       `json:"orphaned"`
	Anchor     Anchor     `json:"anchor"`
	Comments   []Comment  `json:"comments"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
}
