package server

import (
	"os"
	"strconv"
	"sync"

	"github.com/kmrinal19/margin/internal/anchor"
	"github.com/kmrinal19/margin/internal/render"
)

// Both caches key on the source file's mtime+size — the same inputs docETag
// hashes — so an unchanged doc is parsed exactly once, not on every GET. Values
// are immutable after build, so the cached pointers are safe to share across the
// concurrent request goroutines. Each map holds one entry per doc, replaced when
// the file changes, so size is bounded by the corpus.

func fileKey(fi os.FileInfo) string {
	return strconv.FormatInt(fi.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(fi.Size(), 10)
}

// docCache memoizes the parsed re-anchoring Doc per slug.
type docCache struct {
	mu sync.Mutex
	m  map[string]*docCacheEntry
}

type docCacheEntry struct {
	key string
	doc *anchor.Doc
}

func newDocCache() *docCache { return &docCache{m: make(map[string]*docCacheEntry)} }

// docFor returns the re-anchoring Doc for slug, parsing the source only when the
// file has changed since the last call. ok is false if the source is gone or
// unreadable (callers then treat every anchor as orphaned).
func (s *Server) docFor(slug string) (*anchor.Doc, bool) {
	p, err := s.docPath(slug)
	if err != nil {
		return nil, false
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, false
	}
	key := fileKey(fi)

	s.docs.mu.Lock()
	if e := s.docs.m[slug]; e != nil && e.key == key {
		doc := e.doc
		s.docs.mu.Unlock()
		return doc, true
	}
	s.docs.mu.Unlock()

	// Parse outside the lock (it can be the slowest part of a GET). A concurrent
	// cold miss may parse twice; that is harmless — the Doc is immutable and the
	// last writer wins.
	src, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	doc := anchor.NewDoc(s.rnd.Blocks(src))

	s.docs.mu.Lock()
	s.docs.m[slug] = &docCacheEntry{key: key, doc: doc}
	s.docs.mu.Unlock()
	return doc, true
}

// metaCache memoizes a doc's index metadata (title/date/status/excerpt/reading
// time) per absolute path. Comment counts are NOT cached — they are overlaid
// fresh on each index request.
type metaCache struct {
	mu sync.Mutex
	m  map[string]*metaCacheEntry
}

type metaCacheEntry struct {
	key  string
	info render.DocInfo
}

func newMetaCache() *metaCache { return &metaCache{m: make(map[string]*metaCacheEntry)} }

// metaFor returns the cached DocInfo for the doc at absolute path p (already
// stat'd by the caller), parsing front-matter only when the file has changed.
func (s *Server) metaFor(slug, p string, fi os.FileInfo) render.DocInfo {
	key := fileKey(fi)

	s.meta.mu.Lock()
	if e := s.meta.m[p]; e != nil && e.key == key {
		info := e.info
		s.meta.mu.Unlock()
		return info
	}
	s.meta.mu.Unlock()

	info := render.DocInfo{Slug: slug, Title: slug}
	if src, err := os.ReadFile(p); err == nil {
		m := s.rnd.DocMeta(slug, src)
		info.Title, info.Date, info.Status = m.Title, m.Date, m.Status
		info.Excerpt, info.ReadMins = m.Excerpt, m.ReadMins
	}

	s.meta.mu.Lock()
	s.meta.m[p] = &metaCacheEntry{key: key, info: info}
	s.meta.mu.Unlock()
	return info
}
