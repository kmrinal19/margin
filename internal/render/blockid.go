package render

import (
	"fmt"
	"strings"

	"github.com/kmrinal19/margin/internal/anchor"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// blockIDTransformer stamps a stable content-hash id (id="b-…") onto block-level
// leaf nodes so the comment layer can anchor a selection to its containing block
// and re-resolve it after edits. Headings are skipped — they already carry an
// auto-generated slug id used for the TOC and :target navigation.
type blockIDTransformer struct{}

var _ parser.ASTTransformer = blockIDTransformer{}

func (blockIDTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	seen := make(map[string]int)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || !shouldStamp(n) {
			return ast.WalkContinue, nil
		}
		id := anchor.BlockID(blockText(n, source))
		seen[id]++
		if c := seen[id]; c > 1 { // disambiguate identical blocks
			id = fmt.Sprintf("%s-%d", id, c)
		}
		n.SetAttributeString("id", []byte(id))
		return ast.WalkContinue, nil
	})
}

// shouldStamp reports whether n is a block node that should receive a block id.
// These are the element-producing leaf blocks a text selection lands inside.
func shouldStamp(n ast.Node) bool {
	switch n.Kind() {
	case ast.KindParagraph,
		ast.KindCodeBlock,
		ast.KindFencedCodeBlock,
		ast.KindBlockquote,
		ast.KindListItem,
		east.KindTableCell:
		return true
	default:
		return false
	}
}

// blockText returns the plain-text content of a block for hashing.
func blockText(n ast.Node, source []byte) string {
	switch n.Kind() {
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		var b strings.Builder
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			b.Write(seg.Value(source))
		}
		return b.String()
	default:
		return nodeText(n, source)
	}
}

// nodeText concatenates the visible text of a node's inline descendants.
func nodeText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(source))
			// A soft/hard line break renders as whitespace in the browser, so emit
			// one here too — otherwise blockText diverges from the DOM's textContent
			// and comments spanning a wrapped line spuriously orphan. Normalize()
			// later collapses it to a single space.
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte('\n')
			}
		case *ast.String:
			b.Write(t.Value)
		case *ast.AutoLink:
			b.Write(t.URL(source))
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// firstHeading returns the first heading of the given level, or nil.
func firstHeading(doc ast.Node, level int) *ast.Heading {
	var found *ast.Heading
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found != nil {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok && h.Level == level {
			found = h
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return found
}

// Blocks returns every id-bearing block of a document (stamped blocks and
// headings) with its normalized plain text, in document order. It feeds the
// server-side re-anchoring cascade, so the text is normalized identically to
// how block IDs are hashed.
func (r *Renderer) Blocks(src []byte) []anchor.Block {
	_, body := splitFrontMatter(src)
	doc := r.md.Parser().Parse(text.NewReader(body))
	var blocks []anchor.Block
	seen := make(map[string]bool)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		v, ok := n.AttributeString("id")
		if !ok {
			return ast.WalkContinue, nil
		}
		id := attrString(v)
		if id == "" || seen[id] {
			return ast.WalkContinue, nil
		}
		seen[id] = true
		blocks = append(blocks, anchor.Block{ID: id, Text: anchor.Normalize(blockText(n, body))})
		return ast.WalkContinue, nil
	})
	return blocks
}

func attrString(v any) string {
	switch s := v.(type) {
	case []byte:
		return string(s)
	case string:
		return s
	default:
		return ""
	}
}

// buildTOC collects level-2 and level-3 headings with their auto-generated ids.
func buildTOC(doc ast.Node, source []byte) []TOCItem {
	var toc []TOCItem
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok || h.Level < 2 || h.Level > 3 {
			return ast.WalkContinue, nil
		}
		id := ""
		if v, ok := h.AttributeString("id"); ok {
			switch s := v.(type) {
			case []byte:
				id = string(s)
			case string:
				id = s
			}
		}
		toc = append(toc, TOCItem{ID: id, Text: nodeText(h, source), Level: h.Level})
		return ast.WalkSkipChildren, nil
	})
	return toc
}
