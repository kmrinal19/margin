package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kmrinal19/margin/internal/store"
)

const maxBodyBytes = 1 << 20 // 1 MiB cap on comment POST bodies

// ── wire shapes ─────────────────────────────────────────────────────────────

type anchorReq struct {
	BlockID     string `json:"block_id"`
	EndBlockID  string `json:"end_block_id"`
	QuoteExact  string `json:"quote_exact"`
	QuoteTail   string `json:"quote_tail"`
	QuotePrefix string `json:"quote_prefix"`
	QuoteSuffix string `json:"quote_suffix"`
	CharStart   int    `json:"char_start"`
	CharEnd     int    `json:"char_end"`
}

type createCommentReq struct {
	Anchor anchorReq `json:"anchor"`
	Body   string    `json:"body"`
	Author string    `json:"author"`
	Scope  string    `json:"scope"` // "doc" → a document-level note with no text anchor
}

type replyReq struct {
	Body   string `json:"body"`
	Author string `json:"author"`
}

type patchThreadReq struct {
	Status string `json:"status"`
	By     string `json:"by"`
	Note   string `json:"note"`
}

// ── handlers ────────────────────────────────────────────────────────────────

// GET /api/docs/{slug}/comments?status=open|resolved|all
func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		writeError(w, http.StatusNotFound, "no such doc")
		return
	}

	filter := store.Filter(r.URL.Query().Get("status"))
	switch filter {
	case store.FilterOpen, store.FilterResolved, store.FilterAll:
	default:
		filter = store.FilterAll
	}

	threads, err := s.resolveThreads(r, slug, filter)
	if err != nil {
		s.log.Error("list comments", "slug", slug, "err", err)
		writeError(w, http.StatusInternalServerError, "could not list comments")
		return
	}
	if threads == nil {
		threads = []store.Thread{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": slug, "threads": threads})
}

// POST /api/docs/{slug}/comments
func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		writeError(w, http.StatusNotFound, "no such doc")
		return
	}
	if !s.docExists(slug) {
		writeError(w, http.StatusNotFound, "no such doc")
		return
	}

	var req createCommentReq
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "comment body is required")
		return
	}

	var a store.Anchor
	if req.Scope == store.ScopeDoc {
		// a document-level note: no text anchor (never highlighted, never orphans)
		a = store.Anchor{BlockID: store.DocBlockID}
	} else {
		if strings.TrimSpace(req.Anchor.QuoteExact) == "" {
			writeError(w, http.StatusBadRequest, "anchor.quote_exact is required")
			return
		}
		a = store.Anchor{
			BlockID:     req.Anchor.BlockID,
			EndBlockID:  req.Anchor.EndBlockID,
			QuoteExact:  clipRunes(req.Anchor.QuoteExact, 512), // bound per-GET cascade cost + DOM bloat
			QuoteTail:   clipRunes(req.Anchor.QuoteTail, 512),
			QuotePrefix: clipRunes(req.Anchor.QuotePrefix, 64),
			QuoteSuffix: clipRunes(req.Anchor.QuoteSuffix, 64),
			CharStart:   req.Anchor.CharStart,
			CharEnd:     req.Anchor.CharEnd,
		}
	}
	th, err := s.st.CreateThread(r.Context(), slug, a, authorOr(req.Author, "human"), req.Body)
	if err != nil {
		s.log.Error("create comment", "slug", slug, "err", err)
		writeError(w, http.StatusInternalServerError, "could not create comment")
		return
	}
	writeJSON(w, http.StatusCreated, th)
}

// POST /api/threads/{id}/replies
func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid thread id")
		return
	}
	var req replyReq
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "reply body is required")
		return
	}
	c, err := s.st.AddReply(r.Context(), id, authorOr(req.Author, "human"), req.Body)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such thread")
			return
		}
		s.log.Error("add reply", "thread", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not add reply")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// PATCH /api/threads/{id}  -> resolve / reopen
func (s *Server) handlePatchThread(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid thread id")
		return
	}
	var req patchThreadReq
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Status != store.StatusOpen && req.Status != store.StatusResolved {
		writeError(w, http.StatusBadRequest, `status must be "open" or "resolved"`)
		return
	}
	if err := s.st.SetStatus(r.Context(), id, req.Status, authorOr(req.By, "human"), req.Note); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such thread")
			return
		}
		s.log.Error("set status", "thread", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not update thread")
		return
	}
	th, err := s.st.GetThread(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "updated, but could not reload thread")
		return
	}
	writeJSON(w, http.StatusOK, th)
}

// PATCH /api/threads/{tid}/comments/{cid}  -> edit a message's body
func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	tid, ok := parseID(r.PathValue("tid"))
	cid, ok2 := parseID(r.PathValue("cid"))
	if !ok || !ok2 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "comment body is required")
		return
	}
	if err := s.st.EditComment(r.Context(), tid, cid, req.Body); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such comment")
			return
		}
		s.log.Error("edit comment", "thread", tid, "comment", cid, "err", err)
		writeError(w, http.StatusInternalServerError, "could not edit comment")
		return
	}
	th, err := s.st.GetThread(r.Context(), tid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "edited, but could not reload thread")
		return
	}
	writeJSON(w, http.StatusOK, th)
}

// DELETE /api/threads/{id}  -> permanently remove a thread (+ its comments/anchor)
func (s *Server) handleDeleteThread(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid thread id")
		return
	}
	if err := s.st.DeleteThread(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such thread")
			return
		}
		s.log.Error("delete thread", "thread", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not delete thread")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		// Don't echo the decoder error (it leaks struct field names/offsets).
		s.log.Debug("decode request", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func parseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

func authorOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
