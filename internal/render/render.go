// Package render turns Markdown documents into margin's HTML, stamps stable
// block IDs for anchoring, and renders the page shell and the doc index.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"sync"

	"github.com/kmrinal19/margin/web"
	"github.com/yuin/goldmark"
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
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	OpenCount int    `json:"open_count"`
}

// Meta is the lightweight metadata of a document (title + front matter),
// extracted without a full render.
type Meta struct {
	Title  string
	Date   string
	Status string
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
		goldmark.WithExtensions(extension.GFM, &calloutExtension{}),
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
	Docs []DocInfo
}

// Index writes the document index page.
func (r *Renderer) Index(w io.Writer, docs []DocInfo) error {
	if err := r.tmpl.ExecuteTemplate(w, "index.html.tmpl", indexData{Docs: docs}); err != nil {
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
	Slug   string
	Title  string
	Date   string
	Status string
	Lead   string
	TOC    []TOCItem
	Body   template.HTML
}

// Article is a rendered document's pieces, without any page chrome. Used by
// both the served page shell and the portable export.
type Article struct {
	Title  string
	Lead   string
	Date   string
	Status string
	TOC    []TOCItem
	Body   template.HTML
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

	buf := r.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := r.md.Renderer().Render(buf, body, doc); err != nil {
		r.bufPool.Put(buf)
		return Article{}, fmt.Errorf("render markdown: %w", err)
	}
	out := buf.String()
	r.bufPool.Put(buf)

	return Article{
		Title:  title,
		Lead:   fm.Lead,
		Date:   fm.Date,
		Status: fm.Status,
		TOC:    toc,
		Body:   template.HTML(out), //nolint:gosec // trusted, generated from local source
	}, nil
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
		Slug:   slug,
		Title:  art.Title,
		Date:   art.Date,
		Status: art.Status,
		Lead:   art.Lead,
		TOC:    art.TOC,
		Body:   art.Body,
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
	title := fm.Title
	if title == "" {
		if doc := r.md.Parser().Parse(text.NewReader(body)); doc != nil {
			if h := firstHeading(doc, 1); h != nil {
				title = nodeText(h, body)
			}
		}
	}
	if title == "" {
		title = slug
	}
	return Meta{Title: title, Date: fm.Date, Status: fm.Status}
}
