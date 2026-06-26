package store

import "time"

// Thread status values.
const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
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
// block-scoped). Offsets are block-relative, never document-global.
type Anchor struct {
	BlockID     string  `json:"block_id"`
	QuoteExact  string  `json:"quote_exact"`
	QuotePrefix string  `json:"quote_prefix"`
	QuoteSuffix string  `json:"quote_suffix"`
	CharStart   int     `json:"char_start"`
	CharEnd     int     `json:"char_end"`
	Confidence  float64 `json:"confidence"`
}

// Comment is one message within a thread.
type Comment struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
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
