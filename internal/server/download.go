package server

import (
	"bytes"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
	"github.com/kmrinal19/margin/web"
)

// handleDownload serves a document as a downloadable file: Markdown (the source,
// optionally with a review-comments appendix) or a self-contained HTML export
// (optionally with the review snapshot). PDF is intentionally NOT here — it's the
// browser's own "Save as PDF" over the print stylesheet, so margin stays a pure-Go
// single offline binary (a server PDF engine would break that).
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) || !s.docExists(slug) {
		s.httpError(w, http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	withComments := q.Get("comments") == "1"
	inline := q.Get("inline") == "1" // inline disposition → used by the print-to-PDF path
	base := path.Base(slug)          // last path segment → the download filename stem

	src, _, err := s.readDoc(slug)
	if err != nil {
		s.httpError(w, http.StatusInternalServerError)
		return
	}

	switch q.Get("format") {
	case "md", "markdown":
		out := src
		if withComments {
			out = append(append([]byte{}, src...), []byte(s.commentsMarkdown(r, slug))...)
		}
		s.serveDownload(w, base+".md", "text/markdown; charset=utf-8", inline, out)

	case "html":
		art, err := s.rnd.RenderArticle(src)
		if err != nil {
			s.httpError(w, http.StatusInternalServerError)
			return
		}
		var threads []render.ExportThread
		if withComments {
			resolved, _ := s.resolveThreads(r, slug, store.FilterAll)
			threads = exportThreads(resolved)
		}
		css, _ := web.Static.ReadFile("design-system.css")
		var buf bytes.Buffer
		if err := s.rnd.ExportDoc(&buf, art, string(css), threads); err != nil {
			s.log.Error("export html", "slug", slug, "err", err)
			s.httpError(w, http.StatusInternalServerError)
			return
		}
		s.serveDownload(w, base+".html", "text/html; charset=utf-8", inline, buf.Bytes())

	default:
		writeError(w, http.StatusBadRequest, "format must be md or html")
	}
}

func (s *Server) serveDownload(w http.ResponseWriter, filename, contentType string, inline bool, body []byte) {
	disp := "attachment"
	if inline {
		disp = "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename=%q`, disp, filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

// commentsMarkdown renders a doc's resolved threads as a Markdown appendix.
func (s *Server) commentsMarkdown(r *http.Request, slug string) string {
	threads, err := s.resolveThreads(r, slug, store.FilterAll)
	if err != nil || len(threads) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n---\n\n## Review comments\n")
	for _, t := range threads {
		status := t.Status
		if t.Orphaned {
			status = "orphaned"
		}
		head := t.Anchor.QuoteExact
		if t.Anchor.IsDoc() {
			head = "On the document"
		}
		fmt.Fprintf(&b, "\n### %s — _%s_\n\n", oneLine(head), status)
		for _, c := range t.Comments {
			fmt.Fprintf(&b, "- **%s:** %s\n", c.Author, oneLine(c.Body))
		}
	}
	return b.String()
}

// exportThreads maps stored threads to the render export shape (status callout +
// messages), mirroring the CLI export so the snapshot looks identical.
func exportThreads(threads []store.Thread) []render.ExportThread {
	out := make([]render.ExportThread, 0, len(threads))
	for _, t := range threads {
		class, label := "info", "Open"
		switch {
		case t.Orphaned:
			class, label = "warn", "Orphaned — could not be re-located in the current doc"
		case t.Status == store.StatusResolved:
			class, label = "good", "Resolved"
		}
		msgs := make([]render.ExportMessage, 0, len(t.Comments))
		for _, c := range t.Comments {
			msgs = append(msgs, render.ExportMessage{Author: c.Author, Body: c.Body, When: c.CreatedAt.Format("2006-01-02 15:04")})
		}
		q := t.Anchor.QuoteExact
		if t.Anchor.IsDoc() {
			q = "On this document"
		}
		out = append(out, render.ExportThread{Quote: q, CalloutClass: class, StatusLabel: label, Messages: msgs})
	}
	return out
}

// oneLine collapses whitespace so a comment body fits one Markdown list item.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
