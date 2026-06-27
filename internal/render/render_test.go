package render

import (
	"bytes"
	"strings"
	"testing"
)

func newRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRenderDocBasics(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	src := []byte("---\n" +
		"title: Hello Doc\n" +
		"status: draft\n" +
		"---\n\n" +
		"## Section One\n\n" +
		"A paragraph here.\n\n" +
		":::warn Heads up\nbe careful\n:::\n\n" +
		"```go\nfmt.Println(\"hi\")\n```\n")

	var buf bytes.Buffer
	if err := r.RenderDoc(&buf, "hello", src); err != nil {
		t.Fatalf("RenderDoc: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"<title>Hello Doc · margin</title>",
		`<h1 class="doc-title">Hello Doc</h1>`,
		`<p id="b-`,   // paragraph block id
		`<pre id="b-`, // code block id
		`<div class="callout warn"><div class="c-title">Heads up</div>`,
		`href="#section-one"`, // toc link from auto heading id
		`data-slug="hello"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}

func TestRenderWave3Features(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	src := []byte("---\ntitle: Features\n---\n\n" +
		"Body text with a note.[^n] More words here to give the reader something to count.\n\n" +
		"Term\n: A definition of the term.\n\n" +
		"[^n]: the footnote text.\n\n" +
		"```sql\nSELECT 1;\n```\n")
	var buf bytes.Buffer
	if err := r.RenderDoc(&buf, "feat", src); err != nil {
		t.Fatalf("RenderDoc: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`class="footnotes"`,    // footnote endnotes section
		"<dl>", "<dt>", "<dd>", // definition list
		`data-lang="sql"`,     // language label hook on the code block
		"min read",            // reading time in the dateline
		`<div class="prose">`, // body wrapper that enables the drop cap
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}

func TestReadingHelpers(t *testing.T) {
	t.Parallel()
	if got := humanInt(1840); got != "1,840" {
		t.Errorf("humanInt(1840) = %q, want 1,840", got)
	}
	if got := humanInt(42); got != "42" {
		t.Errorf("humanInt(42) = %q, want 42", got)
	}
	if got := readingMinutes(0); got != 0 {
		t.Errorf("readingMinutes(0) = %d, want 0", got)
	}
	if got := readingMinutes(1); got != 1 {
		t.Errorf("readingMinutes(1) = %d, want 1", got)
	}
	if got := readingMinutes(476); got != 2 {
		t.Errorf("readingMinutes(476) = %d, want 2", got)
	}
}

func TestRenderDocNoFrontMatterUsesH1(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	var buf bytes.Buffer
	if err := r.RenderDoc(&buf, "doc", []byte("# Title From H1\n\nsome body text")); err != nil {
		t.Fatalf("RenderDoc: %v", err)
	}
	out := buf.String()
	// The body H1 must be removed so it isn't duplicated under the doc title.
	// Its auto heading id ("title-from-h1") is a precise marker of the body heading.
	if strings.Contains(out, `id="title-from-h1"`) {
		t.Error("leading body H1 should be removed (found its auto heading id in output)")
	}
	if !strings.Contains(out, `<h1 class="doc-title">Title From H1</h1>`) {
		t.Error("doc title should be derived from the H1")
	}
}

func TestRenderDocEscapesNothingInTrustedSource(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	var buf bytes.Buffer
	if err := r.RenderDoc(&buf, "tbl", []byte("| a | b |\n|---|---|\n| 1 | 2 |\n")); err != nil {
		t.Fatalf("RenderDoc: %v", err)
	}
	if !strings.Contains(buf.String(), "<td id=\"b-") {
		t.Error("table cells should carry block ids")
	}
}

func TestBlocksSoftWrapKeepsWhitespace(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	// A paragraph wrapped across two source lines renders as one line with a space
	// in the browser; block text must match (else anchors spanning the wrap orphan).
	blocks := r.Blocks([]byte("one two three\nfour five six\n"))
	if len(blocks) == 0 {
		t.Fatal("no blocks")
	}
	if !strings.Contains(blocks[0].Text, "three four") {
		t.Errorf("soft-wrap boundary lost its space: %q", blocks[0].Text)
	}
}

func TestSplitFrontMatter(t *testing.T) {
	t.Parallel()
	fm, body := splitFrontMatter([]byte("---\ntitle: T\ndate: 2026-01-01\n---\nbody here"))
	if fm.Title != "T" || fm.Date != "2026-01-01" {
		t.Errorf("front matter = %+v", fm)
	}
	if strings.TrimSpace(string(body)) != "body here" {
		t.Errorf("body = %q", body)
	}

	fm2, body2 := splitFrontMatter([]byte("no front matter here"))
	if fm2.Title != "" {
		t.Errorf("expected empty title, got %q", fm2.Title)
	}
	if string(body2) != "no front matter here" {
		t.Errorf("body2 = %q", body2)
	}
}

func TestCalloutSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, class, title string
	}{
		{"warn", "warn", "Warning"},
		{"warning Custom", "warn", "Custom"},
		{"danger", "danger", "Caution"},
		{"good", "good", "Tip"},
		{"info", "info", "Note"},
		{"note", "info", "Note"},
		{"unknown", "info", "Note"},
	}
	for _, c := range cases {
		class, title := calloutSpec(c.in)
		if class != c.class || title != c.title {
			t.Errorf("calloutSpec(%q) = (%q,%q), want (%q,%q)", c.in, class, title, c.class, c.title)
		}
	}
}
