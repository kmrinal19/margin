// Package render turns Markdown documents into margin's HTML, stamps stable
// block IDs for anchoring, and renders the page shell and the doc index.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kmrinal19/margin/web"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// TOCItem is one entry in a document's table of contents.
type TOCItem struct {
	ID    string
	Text  string
	Level int
}

// DocInfo summarizes a document for the index page and the docs API.
type DocInfo struct {
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	OpenCount     int    `json:"open_count"`
	ResolvedCount int    `json:"resolved_count,omitempty"`
	OrphanCount   int    `json:"orphan_count,omitempty"`
	Date          string `json:"date,omitempty"`
	Status        string `json:"status,omitempty"`
	Excerpt       string `json:"-"`
	ReadMins      int    `json:"-"`
}

// Meta is the lightweight metadata of a document (title + front matter + a short
// excerpt and reading length), extracted without a full HTML render.
type Meta struct {
	Title    string
	Date     string
	Status   string
	Excerpt  string
	Words    int
	ReadMins int
}

// Renderer renders the index and document pages from embedded templates.
// It is safe for concurrent use.
type Renderer struct {
	tmpl    *template.Template
	md      goldmark.Markdown
	bufPool sync.Pool
}

// New parses the embedded templates, builds the Markdown engine, and returns a
// ready Renderer.
func New() (*Renderer, error) {
	tmpl, err := template.ParseFS(web.Templates, "*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	md := goldmark.New(
		// GFM (tables/strikethrough/linkify/tasklist) + footnotes + definition lists —
		// all first-party goldmark extensions, no new external deps. RFCs use footnotes.
		goldmark.WithExtensions(extension.GFM, extension.Footnote, extension.DefinitionList, &calloutExtension{}),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(blockIDTransformer{}, 100)),
		),
		// Source docs are trusted (authored by the user/agent locally); the one
		// untrusted input — comment text — is escaped separately by the API/widget.
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			// Lower priority value wins in goldmark; 100 < the default 1000 so this
			// overrides code rendering to emit the block id on <pre>.
			renderer.WithNodeRenderers(util.Prioritized(codeRenderer{}, 100)),
		),
	)
	return &Renderer{
		tmpl:    tmpl,
		md:      md,
		bufPool: sync.Pool{New: func() any { return new(bytes.Buffer) }},
	}, nil
}

type indexData struct {
	Tree  *DocTree
	Empty bool
}

// DocTree is a folder node in the nested document index. The root node has an
// empty Name/Path; its Docs are the top-level docs and its Dirs the subfolders.
type DocTree struct {
	Name        string     // folder segment, hyphens turned to spaces for display ("" at root)
	Path        string     // full folder path, the localStorage collapse key ("" at root)
	Docs        []DocInfo  // docs directly in this folder
	Dirs        []*DocTree // subfolders
	OpenCount   int        // aggregate over this subtree (so a collapsed folder still signals work)
	OrphanCount int
}

// buildDocTree groups a flat, slug-sorted []DocInfo into a folder tree by their
// "/"-separated slugs, then sorts and rolls up descendant counts.
func buildDocTree(docs []DocInfo) *DocTree {
	root := &DocTree{}
	for _, d := range docs {
		segs := strings.Split(d.Slug, "/")
		node := root
		for i := 0; i < len(segs)-1; i++ {
			seg := segs[i]
			var child *DocTree
			for _, c := range node.Dirs {
				if c.Name == strings.ReplaceAll(seg, "-", " ") {
					child = c
					break
				}
			}
			if child == nil {
				p := seg
				if node.Path != "" {
					p = node.Path + "/" + seg
				}
				child = &DocTree{Name: strings.ReplaceAll(seg, "-", " "), Path: p}
				node.Dirs = append(node.Dirs, child)
			}
			node = child
		}
		node.Docs = append(node.Docs, d)
	}
	root.finalize()
	return root
}

// finalize sorts folders (then docs are already slug-sorted) and rolls up counts.
func (t *DocTree) finalize() {
	sort.Slice(t.Dirs, func(i, j int) bool { return t.Dirs[i].Name < t.Dirs[j].Name })
	for _, d := range t.Docs {
		t.OpenCount += d.OpenCount
		t.OrphanCount += d.OrphanCount
	}
	for _, c := range t.Dirs {
		c.finalize()
		t.OpenCount += c.OpenCount
		t.OrphanCount += c.OrphanCount
	}
}

// Index writes the document index page.
func (r *Renderer) Index(w io.Writer, docs []DocInfo) error {
	data := indexData{Tree: buildDocTree(docs), Empty: len(docs) == 0}
	if err := r.tmpl.ExecuteTemplate(w, "index.html.tmpl", data); err != nil {
		return fmt.Errorf("render index: %w", err)
	}
	return nil
}

type errorData struct {
	Code    int
	Heading string
	Message string
}

// RenderError writes a templated, design-system-styled error page (404/500).
func (r *Renderer) RenderError(w io.Writer, code int, heading, message string) error {
	data := errorData{Code: code, Heading: heading, Message: message}
	if err := r.tmpl.ExecuteTemplate(w, "error.html.tmpl", data); err != nil {
		return fmt.Errorf("render error page: %w", err)
	}
	return nil
}

type docData struct {
	Slug       string
	Title      string
	Date       string
	Status     string
	Lead       string
	TOC        []TOCItem
	Body       template.HTML
	ReadMins   int
	WordsHuman string
}

// Article is a rendered document's pieces, without any page chrome. Used by
// both the served page shell and the portable export.
type Article struct {
	Title    string
	Lead     string
	Date     string
	Status   string
	TOC      []TOCItem
	Body     template.HTML
	Words    int
	ReadMins int
}

// RenderArticle renders a document's Markdown source into its pieces.
func (r *Renderer) RenderArticle(src []byte) (Article, error) {
	fm, body := splitFrontMatter(src)

	doc := r.md.Parser().Parse(text.NewReader(body))

	title := fm.Title
	if title == "" {
		if h := firstHeading(doc, 1); h != nil {
			title = nodeText(h, body)
			if p := h.Parent(); p != nil { // avoid a duplicate H1 under the doc title
				p.RemoveChild(p, h)
			}
		}
	}

	toc := buildTOC(doc, body)
	words := countWords(body)

	buf := r.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := r.md.Renderer().Render(buf, body, doc); err != nil {
		r.bufPool.Put(buf)
		return Article{}, fmt.Errorf("render markdown: %w", err)
	}
	out := buf.String()
	r.bufPool.Put(buf)

	return Article{
		Title:    title,
		Lead:     fm.Lead,
		Date:     fm.Date,
		Status:   fm.Status,
		TOC:      toc,
		Body:     template.HTML(out), //nolint:gosec // trusted, generated from local source
		Words:    words,
		ReadMins: readingMinutes(words),
	}, nil
}

// countWords approximates the prose length from the Markdown source. Whitespace
// tokens slightly over-count (markdown punctuation), which is fine for a "~N min".
func countWords(b []byte) int { return len(bytes.Fields(b)) }

// readingMinutes converts a word count to minutes at ~238 wpm (min 1).
func readingMinutes(words int) int {
	if words <= 0 {
		return 0
	}
	m := (words + 237) / 238
	if m < 1 {
		m = 1
	}
	return m
}

// humanInt formats an int with thousands separators (e.g. 1840 -> "1,840").
func humanInt(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// RenderDoc renders a Markdown document's source into the full page shell.
func (r *Renderer) RenderDoc(w io.Writer, slug string, src []byte) error {
	art, err := r.RenderArticle(src)
	if err != nil {
		return err
	}
	if art.Title == "" {
		art.Title = slug
	}
	page := docData{
		Slug:       slug,
		Title:      art.Title,
		Date:       art.Date,
		Status:     art.Status,
		Lead:       art.Lead,
		TOC:        art.TOC,
		Body:       art.Body,
		ReadMins:   art.ReadMins,
		WordsHuman: humanInt(art.Words),
	}
	if err := r.tmpl.ExecuteTemplate(w, "shell.html.tmpl", page); err != nil {
		return fmt.Errorf("render shell: %w", err)
	}
	return nil
}

// ExportMessage is one comment in an exported review.
type ExportMessage struct {
	Author string
	Body   string
	When   string
}

// ExportThread is a thread rendered into the portable export.
type ExportThread struct {
	Quote        string
	CalloutClass string // info | good | warn
	StatusLabel  string
	Messages     []ExportMessage
}

type exportData struct {
	Title       string
	Date        string
	Status      string
	Lead        string
	CSS         template.CSS
	TOC         []TOCItem
	Body        template.HTML
	Threads     []ExportThread
	HasComments bool
}

// ExportDoc writes a self-contained, server-free HTML file: the rendered doc
// with inlined CSS plus a static, read-only snapshot of its review comments.
func (r *Renderer) ExportDoc(w io.Writer, art Article, css string, threads []ExportThread) error {
	data := exportData{
		Title:       art.Title,
		Date:        art.Date,
		Status:      art.Status,
		Lead:        art.Lead,
		CSS:         template.CSS(css), //nolint:gosec // our own embedded stylesheet
		TOC:         art.TOC,
		Body:        art.Body,
		Threads:     threads,
		HasComments: len(threads) > 0,
	}
	if err := r.tmpl.ExecuteTemplate(w, "export.html.tmpl", data); err != nil {
		return fmt.Errorf("render export: %w", err)
	}
	return nil
}

// DocMeta extracts a document's title/date/status without a full render.
func (r *Renderer) DocMeta(slug string, src []byte) Meta {
	fm, body := splitFrontMatter(src)
	doc := r.md.Parser().Parse(text.NewReader(body))
	title := fm.Title
	if title == "" && doc != nil {
		if h := firstHeading(doc, 1); h != nil {
			title = nodeText(h, body)
		}
	}
	if title == "" {
		title = slug
	}
	excerpt := fm.Lead
	if excerpt == "" && doc != nil {
		excerpt = firstParagraphText(doc, body)
	}
	words := countWords(body)
	return Meta{
		Title:    title,
		Date:     fm.Date,
		Status:   fm.Status,
		Excerpt:  truncateWords(excerpt, 26),
		Words:    words,
		ReadMins: readingMinutes(words),
	}
}

// firstParagraphText returns the text of the first paragraph in the document,
// skipping headings — a sensible excerpt for the index when there's no lead.
func firstParagraphText(doc ast.Node, src []byte) string {
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if n.Kind() == ast.KindParagraph {
			return nodeText(n, src)
		}
	}
	return ""
}

// truncateWords clamps s to at most n words, appending an ellipsis when cut.
func truncateWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) <= n {
		return strings.Join(f, " ")
	}
	return strings.Join(f[:n], " ") + "…"
}
