package render

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Callouts are authored with a fenced container:
//
//	:::warn Optional title
//	body markdown…
//	:::
//
// and render to <div class="callout warn"><div class="c-title">…</div> body </div>,
// matching the design system. A bare ::: closes the block.

var calloutFence = []byte(":::")

// kindCallout identifies the callout block node.
var kindCallout = ast.NewNodeKind("Callout")

type calloutNode struct {
	ast.BaseBlock
	Class string // info | good | warn | danger
	Title string
}

func (*calloutNode) Kind() ast.NodeKind { return kindCallout }

func (n *calloutNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Class": n.Class, "Title": n.Title}, nil)
}

// calloutSpec maps the text after ":::" to a CSS class and a default title,
// with an optional author-supplied title overriding the default.
func calloutSpec(spec string) (class, title string) {
	typ := spec
	rest := ""
	if i := strings.IndexAny(spec, " \t"); i >= 0 {
		typ, rest = spec[:i], strings.TrimSpace(spec[i+1:])
	}
	switch strings.ToLower(typ) {
	case "good", "tip", "success":
		class, title = "good", "Tip"
	case "warn", "warning":
		class, title = "warn", "Warning"
	case "danger", "caution", "error":
		class, title = "danger", "Caution"
	default: // info, note, or unknown
		class, title = "info", "Note"
	}
	if rest != "" {
		title = rest
	}
	return class, title
}

func isClosingFence(line []byte) bool {
	return bytes.Equal(bytes.TrimRight(line, " \t\r\n"), calloutFence)
}

type calloutParser struct{}

var _ parser.BlockParser = calloutParser{}

func (calloutParser) Trigger() []byte { return []byte{':'} }

func (calloutParser) Open(_ ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	if !bytes.HasPrefix(line, calloutFence) {
		return nil, parser.NoChildren
	}
	spec := strings.TrimSpace(string(line[len(calloutFence):]))
	if spec == "" {
		// A bare ::: is only ever a closer, never an opener.
		return nil, parser.NoChildren
	}
	class, title := calloutSpec(spec)
	reader.AdvanceLine()
	return &calloutNode{Class: class, Title: title}, parser.HasChildren
}

func (calloutParser) Continue(_ ast.Node, reader text.Reader, _ parser.Context) parser.State {
	line, _ := reader.PeekLine()
	if isClosingFence(line) {
		reader.AdvanceLine()
		return parser.Close
	}
	return parser.Continue | parser.HasChildren
}

func (calloutParser) Close(_ ast.Node, _ text.Reader, _ parser.Context) {}

func (calloutParser) CanInterruptParagraph() bool { return true }

func (calloutParser) CanAcceptIndentedLine() bool { return false }

type calloutRenderer struct{}

func (calloutRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindCallout, renderCallout)
}

func renderCallout(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		c := n.(*calloutNode)
		_, _ = w.WriteString(`<div class="callout `)
		_, _ = w.WriteString(c.Class)
		_, _ = w.WriteString(`"><div class="c-title">`)
		_, _ = w.Write(util.EscapeHTML([]byte(c.Title)))
		_, _ = w.WriteString(`</div>`)
	} else {
		_, _ = w.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}

// calloutExtension wires the parser and renderer into goldmark.
type calloutExtension struct{}

var _ goldmark.Extender = (*calloutExtension)(nil)

func (*calloutExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithBlockParsers(
		util.Prioritized(calloutParser{}, 100),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(calloutRenderer{}, 100),
	))
}
