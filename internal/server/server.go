// Package server implements margin's HTTP layer: routes, handlers, middleware,
// render-on-read document serving, the REST API, and the re-anchoring seam.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kmrinal19/margin/internal/anchor"
	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
	"github.com/kmrinal19/margin/web"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validSlug reports whether s is a safe document slug (maps to docs/<slug>.md).
func validSlug(s string) bool { return slugRe.MatchString(s) }

// IsLoopbackHost reports whether host is a loopback bind address. margin is
// offline-only, so the CLI refuses non-loopback hosts unless explicitly overridden.
func IsLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Server serves the doc index, rendered docs, embedded assets, and the REST API.
type Server struct {
	cfg      Config
	log      *slog.Logger
	rnd      *render.Renderer
	st       *store.Store
	handler  http.Handler
	etagSeed int64 // per-process; makes ETags refresh across restarts/rebuilds
}

var _ http.Handler = (*Server)(nil)

// New constructs a Server and wires its routes. The renderer and store must be
// non-nil.
func New(cfg Config, log *slog.Logger, rnd *render.Renderer, st *store.Store) *Server {
	s := &Server{cfg: cfg, log: log, rnd: rnd, st: st, etagSeed: time.Now().UnixNano()}
	s.handler = s.buildHandler()
	return s
}

// ServeHTTP makes Server itself the root http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /doc/{slug}", s.handleDoc)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(web.Static)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	// Catch-all for unmatched paths → a templated 404 (more specific patterns win).
	mux.HandleFunc("/", s.handleNotFound)

	// REST API (§10) — shared by the browser widget and the CLI client.
	mux.HandleFunc("GET /api/docs", s.handleListDocs)
	mux.HandleFunc("GET /api/docs/{slug}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/docs/{slug}/comments", s.handleCreateComment)
	mux.HandleFunc("POST /api/threads/{id}/replies", s.handleReply)
	mux.HandleFunc("PATCH /api/threads/{id}", s.handlePatchThread)

	// Outermost first: recover → log → bound execution → routes.
	var h http.Handler = mux
	h = http.TimeoutHandler(h, 10*time.Second, "request timed out")
	h = logMW(s.log)(h)
	h = recoverMW(s.log)(h)
	return h
}

// Run starts the server and blocks until ctx is cancelled (SIGINT/SIGTERM) or
// the listener fails, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s (already running? pick another --port): %w", addr, err)
	}

	srv := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	s.log.Info("margin serving", "url", "http://"+addr, "docs", s.cfg.DocsDir, "data", s.cfg.DataDir)

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		s.log.Info("shutting down")
		// A fresh context on purpose: the incoming ctx is already cancelled (that
		// is why we are shutting down), so Shutdown needs its own drain deadline.
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil { //nolint:contextcheck // intentional fresh drain deadline
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	docs, err := s.listDocs()
	if err != nil {
		s.log.Error("list docs", "err", err)
		s.httpError(w, http.StatusInternalServerError)
		return
	}
	if counts, err := s.st.OpenCounts(r.Context()); err != nil {
		s.log.Warn("open counts", "err", err)
	} else {
		for i := range docs {
			docs[i].OpenCount = counts[docs[i].Slug]
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.rnd.Index(w, docs); err != nil {
		s.log.Error("render index", "err", err)
	}
}

// handleListDocs returns the doc index as JSON (slug, title, open-comment count).
func (s *Server) handleListDocs(w http.ResponseWriter, r *http.Request) {
	docs, err := s.listDocs()
	if err != nil {
		s.log.Error("list docs", "err", err)
		writeError(w, http.StatusInternalServerError, "could not list docs")
		return
	}
	if counts, err := s.st.OpenCounts(r.Context()); err == nil {
		for i := range docs {
			docs[i].OpenCount = counts[docs[i].Slug]
		}
	}
	if docs == nil {
		docs = []render.DocInfo{}
	}
	writeJSON(w, http.StatusOK, docs)
}

// resolveThreads loads a doc's threads and re-resolves each anchor against the
// CURRENT source via the 4-tier cascade, healing stored anchors that drifted and
// flagging unresolvable ones as orphaned — so the widget and the AI always see
// freshly-resolved positions. The status filter is applied after re-resolution.
func (s *Server) resolveThreads(r *http.Request, slug string, filter store.Filter) ([]store.Thread, error) {
	ctx := r.Context()
	threads, err := s.st.ListThreads(ctx, slug, store.FilterAll)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return threads, nil
	}

	src, _, err := s.readDoc(slug)
	if err != nil {
		// Source gone/unreadable: every anchor is effectively orphaned, but we
		// don't persist that (the doc may reappear). Return stored, filtered.
		return filterByStatus(threads, filter), nil
	}
	doc := anchor.NewDoc(s.rnd.Blocks(src))

	var heals []store.HealRow
	for i := range threads {
		a := threads[i].Anchor
		newA, orphaned := reanchor(doc, a)

		// Persist only on a genuine change — a no-op resolution never touches the
		// single-writer pool, even on the browser's frequent polling GETs.
		if newA != a || orphaned != threads[i].Orphaned {
			heals = append(heals, store.HealRow{ThreadID: threads[i].ID, Anchor: newA, Orphaned: orphaned})
		}
		threads[i].Anchor = newA
		threads[i].Orphaned = orphaned
	}

	// One short write transaction for all drifted threads, not N.
	if len(heals) > 0 {
		if err := s.st.UpdateAnchorResolutions(ctx, heals); err != nil {
			s.log.Warn("heal anchors", "count", len(heals), "err", err)
		}
	}

	return filterByStatus(threads, filter), nil
}

// reanchor re-resolves an anchor against the current document, rewriting it to
// the freshly-found span (so the next resolve hits tier 1 and a stable doc stops
// generating writes). A multi-block anchor resolves its head and tail endpoints
// independently and orphans if EITHER endpoint can no longer be located.
func reanchor(doc *anchor.Doc, a store.Anchor) (store.Anchor, bool) {
	if !a.Multi() {
		res := doc.Resolve(anchor.Stored{
			BlockID: a.BlockID, Prefix: a.QuotePrefix, Exact: a.QuoteExact,
			Suffix: a.QuoteSuffix, Start: a.CharStart, End: a.CharEnd,
		})
		if !res.OK {
			return a, true
		}
		n := a
		n.BlockID, n.EndBlockID = res.BlockID, res.BlockID
		n.QuoteExact, n.QuoteTail = res.Exact, ""
		n.QuotePrefix, n.QuoteSuffix = res.Prefix, res.Suffix
		n.CharStart, n.CharEnd = res.Start, res.End
		n.Confidence = res.Confidence
		return n, false
	}

	head := doc.Resolve(anchor.Stored{
		BlockID: a.BlockID, Prefix: a.QuotePrefix, Exact: a.QuoteExact, Start: a.CharStart,
	})
	tail := doc.Resolve(anchor.Stored{
		BlockID: a.EndBlockID, Exact: a.QuoteTail, Suffix: a.QuoteSuffix, Start: a.CharEnd,
	})
	if !head.OK || !tail.OK {
		return a, true
	}
	n := a
	n.BlockID, n.QuoteExact, n.QuotePrefix, n.CharStart = head.BlockID, head.Exact, head.Prefix, head.Start
	n.EndBlockID, n.QuoteTail, n.QuoteSuffix, n.CharEnd = tail.BlockID, tail.Exact, tail.Suffix, tail.End
	n.Confidence = min(head.Confidence, tail.Confidence)
	return n, false
}

func filterByStatus(threads []store.Thread, filter store.Filter) []store.Thread {
	if filter == store.FilterAll || filter == "" {
		return threads
	}
	out := make([]store.Thread, 0, len(threads))
	for _, t := range threads {
		if (filter == store.FilterOpen && t.Status == store.StatusOpen) ||
			(filter == store.FilterResolved && t.Status == store.StatusResolved) {
			out = append(out, t)
		}
	}
	return out
}

// listDocs scans the docs directory for renderable Markdown files.
func (s *Server) listDocs() ([]render.DocInfo, error) {
	entries, err := os.ReadDir(s.cfg.DocsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var docs []render.DocInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		if !validSlug(slug) {
			continue
		}
		title := slug
		if src, err := os.ReadFile(filepath.Join(s.cfg.DocsDir, e.Name())); err == nil {
			title = s.rnd.DocMeta(slug, src).Title
		}
		docs = append(docs, render.DocInfo{Slug: slug, Title: title})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Slug < docs[j].Slug })
	return docs, nil
}

func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		s.httpError(w, http.StatusNotFound)
		return
	}
	src, fi, err := s.readDoc(slug)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.httpError(w, http.StatusNotFound)
			return
		}
		s.log.Error("read doc", "slug", slug, "err", err)
		s.httpError(w, http.StatusInternalServerError)
		return
	}

	// The rendered HTML depends only on the .md file (comments are fetched
	// client-side), so a conditional GET can skip re-render + transfer.
	etag := s.docETag(fi)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache") // must revalidate, but 304 is cheap
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.rnd.RenderDoc(w, slug, src); err != nil {
		s.log.Error("render doc", "slug", slug, "err", err)
	}
}

// handleNotFound serves a templated 404 for any unmatched path.
func (s *Server) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	s.httpError(w, http.StatusNotFound)
}

// httpError renders a design-system-styled HTML error page with the given status.
func (s *Server) httpError(w http.ResponseWriter, status int) {
	heading, message := errorCopy(status)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.rnd.RenderError(w, status, heading, message); err != nil {
		s.log.Error("render error page", "status", status, "err", err)
	}
}

// errorCopy returns the on-brand heading + message for an HTTP status.
func errorCopy(status int) (heading, message string) {
	switch status {
	case http.StatusNotFound:
		return "No such manuscript.", "This page was left blank. The document you’re looking for isn’t on the desk."
	default:
		return "The press jammed.", "Something went wrong on our end. Try again in a moment."
	}
}

// docPath resolves slug to an absolute path within the docs directory.
func (s *Server) docPath(slug string) (string, error) {
	base, err := filepath.Abs(s.cfg.DocsDir)
	if err != nil {
		return "", err
	}
	p := filepath.Join(base, slug+".md")
	if p != base && !strings.HasPrefix(p, base+string(filepath.Separator)) {
		return "", errors.New("doc path escapes docs directory")
	}
	return p, nil
}

// docExists reports whether docs/<slug>.md exists, without reading it.
func (s *Server) docExists(slug string) bool {
	p, err := s.docPath(slug)
	if err != nil {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func (s *Server) readDoc(slug string) ([]byte, os.FileInfo, error) {
	p, err := s.docPath(slug)
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, nil, err
	}
	src, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, err
	}
	return src, fi, nil
}

func (s *Server) docETag(fi os.FileInfo) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%d:%d:%d", s.etagSeed, fi.ModTime().UnixNano(), fi.Size()))
	return `"` + hex.EncodeToString(h[:8]) + `"`
}
