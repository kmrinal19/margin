package render

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// codeRenderer overrides goldmark's default code-block rendering so that the
// block id stamped by blockIDTransformer is emitted on the <pre> element
// (the default renderer drops node attributes on code blocks). Fenced code
// stays plain — no syntax highlighter — per the design goals.
type codeRenderer struct{}

func (codeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, renderFencedCode)
	reg.Register(ast.KindCodeBlock, renderIndentedCode)
}

func writeBlockID(w util.BufWriter, n ast.Node) {
	if v, ok := n.AttributeString("id"); ok {
		if b, ok := v.([]byte); ok {
			_, _ = w.WriteString(` id="`)
			_, _ = w.Write(b) // value is "b-<hex>", safe
			_ = w.WriteByte('"')
		}
	}
}

func writeCodeLines(w util.BufWriter, source []byte, n ast.Node) {
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		seg := l.At(i)
		_, _ = w.Write(util.EscapeHTML(seg.Value(source)))
	}
}

func renderFencedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.FencedCodeBlock)
	if entering {
		lang := n.Language(source)
		_, _ = w.WriteString("<pre")
		writeBlockID(w, n)
		if lang != nil {
			_, _ = w.WriteString(` data-lang="`)
			_, _ = w.Write(util.EscapeHTML(lang)) // shown as a label via CSS
			_ = w.WriteByte('"')
		}
		_, _ = w.WriteString("><code")
		if lang != nil {
			_, _ = w.WriteString(` class="language-`)
			_, _ = w.Write(util.EscapeHTML(lang))
			_ = w.WriteByte('"')
		}
		_ = w.WriteByte('>')
		writeCodeLines(w, source, n)
	} else {
		_, _ = w.WriteString("</code></pre>\n")
	}
	return ast.WalkContinue, nil
}

func renderIndentedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<pre")
		writeBlockID(w, node)
		_, _ = w.WriteString("><code>")
		writeCodeLines(w, source, node)
	} else {
		_, _ = w.WriteString("</code></pre>\n")
	}
	return ast.WalkContinue, nil
}
