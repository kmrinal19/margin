package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
)

const sampleDoc = `## Eligibility

The eligibility window opens in March and closes in May for all applicants.

## Fees

Pay the main semester fee at registration.
`

func newTestServer(t *testing.T) (*httptest.Server, string, *render.Renderer) {
	t.Helper()
	docs := t.TempDir()
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(sampleDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	rnd, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := New(Config{Host: "127.0.0.1", DocsDir: docs, DataDir: data},
		slog.New(slog.NewTextHandler(io.Discard, nil)), rnd, st)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, docs, rnd
}

func blockIDFor(t *testing.T, rnd *render.Renderer, sub string) string {
	t.Helper()
	for _, b := range rnd.Blocks([]byte(sampleDoc)) {
		if strings.Contains(b.Text, sub) {
			return b.ID
		}
	}
	t.Fatalf("no block contains %q", sub)
	return ""
}

func reqJSON(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func createComment(t *testing.T, ts *httptest.Server, rnd *render.Renderer, sub, body string) store.Thread {
	t.Helper()
	bid := blockIDFor(t, rnd, sub)
	status, data := reqJSON(t, "POST", ts.URL+"/api/comments/guide", map[string]any{
		"anchor": map[string]any{
			"block_id":     bid,
			"quote_exact":  sub,
			"quote_prefix": "",
			"quote_suffix": "",
			"char_start":   0,
			"char_end":     len(sub),
		},
		"body":   body,
		"author": "human",
	})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d: %s", status, data)
	}
	var th store.Thread
	if err := json.Unmarshal(data, &th); err != nil {
		t.Fatal(err)
	}
	return th
}

func listThreads(t *testing.T, ts *httptest.Server, status string) []store.Thread {
	t.Helper()
	code, data := reqJSON(t, "GET", ts.URL+"/api/comments/guide?status="+status, nil)
	if code != http.StatusOK {
		t.Fatalf("list: status %d", code)
	}
	var out struct {
		Threads []store.Thread `json:"threads"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out.Threads
}

func TestAPICreateAndList(t *testing.T) {
	ts, _, rnd := newTestServer(t)
	th := createComment(t, ts, rnd, "opens in March", "When exactly?")
	if th.ID == 0 || th.Anchor.BlockID == "" {
		t.Fatalf("bad thread: %+v", th)
	}
	got := listThreads(t, ts, "open")
	if len(got) != 1 || got[0].Comments[0].Body != "When exactly?" {
		t.Fatalf("list = %+v", got)
	}
}

func TestDocLevelComment(t *testing.T) {
	ts, _, _ := newTestServer(t)

	// scope=doc creates a note with no text anchor
	code, data := reqJSON(t, "POST", ts.URL+"/api/comments/guide", map[string]any{
		"scope": "doc", "body": "Overall: needs a security section.", "author": "human",
	})
	if code != http.StatusCreated {
		t.Fatalf("create doc note: %d: %s", code, data)
	}
	var th store.Thread
	if err := json.Unmarshal(data, &th); err != nil {
		t.Fatal(err)
	}
	if th.Anchor.BlockID != store.DocBlockID || !th.Anchor.IsDoc() {
		t.Errorf("doc note anchor = %+v, want block_id=%q", th.Anchor, store.DocBlockID)
	}
	if th.Anchor.QuoteExact != "" {
		t.Errorf("doc note should have no quote, got %q", th.Anchor.QuoteExact)
	}

	// it never orphans, even though it has no locatable span in the source
	got := listThreads(t, ts, "all")
	if len(got) != 1 {
		t.Fatalf("want 1 thread, got %d", len(got))
	}
	if got[0].Orphaned {
		t.Error("a document-level note must never orphan")
	}
	if !got[0].Anchor.IsDoc() {
		t.Error("listed thread should still be doc-level")
	}

	// an anchored create still requires a quote
	bad, _ := reqJSON(t, "POST", ts.URL+"/api/comments/guide", map[string]any{
		"body": "no anchor", "author": "human",
	})
	if bad != http.StatusBadRequest {
		t.Errorf("anchored create without a quote = %d, want 400", bad)
	}
}

func TestValidSlugNested(t *testing.T) {
	t.Parallel()
	ok := []string{"welcome", "payments/refunds", "a/b/c", "auth/oauth-flow"}
	bad := []string{"", "/", "a/", "/a", "a//b", "../etc", "a/../b", "A/b", "a/.hidden", "a b"}
	for _, s := range ok {
		if !validSlug(s) {
			t.Errorf("validSlug(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if validSlug(s) {
			t.Errorf("validSlug(%q) = true, want false", s)
		}
	}
}

func TestNestedDocsListedAndServed(t *testing.T) {
	t.Parallel()
	docs := t.TempDir()
	data := t.TempDir()
	must := func(p, body string) {
		full := filepath.Join(docs, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("welcome.md", "# Welcome\n\nTop level.\n")
	must("payments/refunds.md", "# Refunds\n\nRefunds go to the original method.\n")
	must("payments/disputes/chargebacks.md", "# Chargebacks\n\nRepresentment flow.\n")

	rnd, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := New(Config{Host: "127.0.0.1", DocsDir: docs, DataDir: data},
		slog.New(slog.NewTextHandler(io.Discard, nil)), rnd, st)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	// the recursive scan surfaces nested slugs
	code, data2 := reqJSON(t, "GET", ts.URL+"/api/docs", nil)
	if code != http.StatusOK {
		t.Fatalf("list docs: %d", code)
	}
	for _, want := range []string{`"payments/refunds"`, `"payments/disputes/chargebacks"`, `"welcome"`} {
		if !strings.Contains(string(data2), want) {
			t.Errorf("/api/docs missing slug %s", want)
		}
	}

	// a nested doc page renders
	resp, err := http.Get(ts.URL + "/doc/payments/disputes/chargebacks")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("nested doc page = %d, want 200", resp.StatusCode)
	}

	// comments round-trip on a nested slug via the trailing-wildcard route
	var bid string
	for _, b := range rnd.Blocks([]byte("# Refunds\n\nRefunds go to the original method.\n")) {
		if strings.Contains(b.Text, "original method") {
			bid = b.ID
		}
	}
	st2, _ := reqJSON(t, "POST", ts.URL+"/api/comments/payments/refunds", map[string]any{
		"anchor": map[string]any{"block_id": bid, "quote_exact": "original method", "char_start": 0, "char_end": 15},
		"body":   "which one?", "author": "human",
	})
	if st2 != http.StatusCreated {
		t.Fatalf("create on nested slug = %d, want 201", st2)
	}
	lc, ld := reqJSON(t, "GET", ts.URL+"/api/comments/payments/refunds?status=all", nil)
	if lc != http.StatusOK || !strings.Contains(string(ld), "which one?") {
		t.Errorf("list on nested slug = %d, body %s", lc, ld)
	}

	// the index renders a collapsible folder tree
	page, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	pb, _ := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if !strings.Contains(string(pb), `data-dir="payments"`) || !strings.Contains(string(pb), `data-dir="payments/disputes"`) {
		t.Error("index is missing the nested folder tree")
	}
}

func TestErrorPagesAndMethodHandling(t *testing.T) {
	ts, _, _ := newTestServer(t)

	// a missing doc → templated HTML 404, not bare plaintext
	resp, err := http.Get(ts.URL + "/doc/no-such-doc")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing doc status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("404 content-type = %q, want text/html", ct)
	}
	if !strings.Contains(string(body), "No such manuscript") {
		t.Error("404 body is not the templated error page")
	}

	// a wrong method on a real route → 405 with an Allow header (the catch-all
	// must NOT mask this as a 404)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/docs", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST to a GET route = %d, want 405", resp2.StatusCode)
	}
	if resp2.Header.Get("Allow") == "" {
		t.Error("405 response is missing the Allow header")
	}
}

func TestReanchorHealsAfterEdit(t *testing.T) {
	ts, docs, rnd := newTestServer(t)
	th := createComment(t, ts, rnd, "opens in March", "note")
	origBlock := th.Anchor.BlockID

	// Edit the commented block so its content hash (id) changes, but the quoted
	// text survives — the cascade should re-home the thread, not orphan it.
	edited := strings.Replace(sampleDoc,
		"The eligibility window opens in March",
		"Note: the eligibility window opens in March",
		1)
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	got := listThreads(t, ts, "open")
	if len(got) != 1 {
		t.Fatalf("want 1 thread, got %d", len(got))
	}
	if got[0].Orphaned {
		t.Error("thread should have healed, not orphaned")
	}
	if got[0].Anchor.BlockID == origBlock {
		t.Error("block id should have been healed to the edited block's new id")
	}
}

func TestReanchorOrphansWhenQuoteGone(t *testing.T) {
	ts, docs, rnd := newTestServer(t)
	createComment(t, ts, rnd, "opens in March", "note")

	// Remove the quoted text entirely.
	edited := strings.Replace(sampleDoc,
		"The eligibility window opens in March and closes in May for all applicants.",
		"The eligibility window has been discontinued.",
		1)
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	got := listThreads(t, ts, "all")
	if len(got) != 1 {
		t.Fatalf("want 1 thread, got %d", len(got))
	}
	if !got[0].Orphaned {
		t.Error("thread should be orphaned after its quote was deleted")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"127.0.0.1":   true,
		"::1":         true,
		"localhost":   true,
		"":            true,
		"0.0.0.0":     false,
		"192.168.1.5": false,
		"example.com": false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestSlugValidationAndNotFound(t *testing.T) {
	ts, _, _ := newTestServer(t)
	code, _ := reqJSON(t, "GET", ts.URL+"/doc/../etc/passwd", nil)
	if code == http.StatusOK {
		t.Error("path traversal slug should not 200")
	}
	code2, _ := reqJSON(t, "GET", ts.URL+"/doc/missing", nil)
	if code2 != http.StatusNotFound {
		t.Errorf("missing doc = %d, want 404", code2)
	}
}

// spyStore wraps a real store to observe heal writes (the no-write-storm invariant).
type spyStore struct {
	*store.Store
	healCalls int
	healRows  int
}

func (s *spyStore) UpdateAnchorResolutions(ctx context.Context, rows []store.HealRow) error {
	s.healCalls++
	s.healRows += len(rows)
	return s.Store.UpdateAnchorResolutions(ctx, rows)
}

// newSpyServer builds a server whose store is observable, for write-accounting tests.
func newSpyServer(t *testing.T) (*httptest.Server, *spyStore, string, *render.Renderer) {
	t.Helper()
	docs, data := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(sampleDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	rnd, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	spy := &spyStore{Store: st}
	srv := New(Config{Host: "127.0.0.1", DocsDir: docs, DataDir: data},
		slog.New(slog.NewTextHandler(io.Discard, nil)), rnd, spy)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, spy, docs, rnd
}

func TestNoWriteStormOnStableDoc(t *testing.T) {
	ts, spy, docs, _ := newSpyServer(t)

	// seed an anchored comment, then edit the doc so the anchor must heal
	createComment(t, ts, mustRenderer(t), "main semester fee", "which fee?")
	if err := os.WriteFile(filepath.Join(docs, "guide.md"),
		[]byte(strings.Replace(sampleDoc, "Pay the main semester fee", "Please pay the main semester fee", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	// The first GET(s) heal the drifted anchor. Healing converges in a bounded
	// number of steps (a fuzzy heal rewrites the anchor so the next resolve hits
	// tier 1 and bumps confidence to 1.0), then reaches a fixed point.
	for i := 0; i < 6; i++ {
		_ = listThreads(t, ts, "all")
	}
	if spy.healRows == 0 {
		t.Fatal("expected the drifted anchor to heal at least once")
	}
	converged := spy.healRows

	// Once converged, the browser's frequent polling must perform ZERO further
	// heal writes — a stable doc must never touch the single writer pool.
	for i := 0; i < 5; i++ {
		_ = listThreads(t, ts, "all")
	}
	if spy.healRows != converged {
		t.Errorf("stable doc kept writing (a write storm): %d heal rows at convergence, %d after polling", converged, spy.healRows)
	}
}

// mustRenderer is a tiny helper so createComment (which needs a renderer to find
// block ids) can be reused with the spy server.
func mustRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	r, err := render.New()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestJSONErrorContract(t *testing.T) {
	ts, _, _ := newTestServer(t)

	// unknown field is rejected (DisallowUnknownFields)
	code, _ := reqJSON(t, "POST", ts.URL+"/api/comments/guide", map[string]any{
		"body": "x", "author": "human", "bogus": 1,
		"anchor": map[string]any{"block_id": "b-1", "quote_exact": "x"},
	})
	if code != http.StatusBadRequest {
		t.Errorf("unknown field = %d, want 400", code)
	}

	// an over-large body is rejected with 413 (MaxBytesReader)
	big := strings.Repeat("a", 2<<20)
	code2, _ := reqJSON(t, "POST", ts.URL+"/api/comments/guide", map[string]any{
		"body": big, "author": "human",
		"anchor": map[string]any{"block_id": "b-1", "quote_exact": "x"},
	})
	if code2 != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body = %d, want 413", code2)
	}

	// missing quote on an anchored create
	code3, _ := reqJSON(t, "POST", ts.URL+"/api/comments/guide", map[string]any{
		"body": "x", "author": "human", "anchor": map[string]any{"block_id": "b-1"},
	})
	if code3 != http.StatusBadRequest {
		t.Errorf("missing quote = %d, want 400", code3)
	}
}

func TestConcurrentResolveAndHealRace(t *testing.T) {
	ts, docs, rnd := newTestServer(t)
	// seed a few anchored comments
	for _, q := range []string{"eligibility window", "main semester fee", "all applicants"} {
		createComment(t, ts, rnd, q, "note on "+q)
	}
	// edit the doc so every anchor must heal
	edited := strings.NewReplacer(
		"The eligibility window", "Note: the eligibility window",
		"Pay the main semester fee", "Please pay the main semester fee",
	).Replace(sampleDoc)
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// hammer the resolve+heal path concurrently (run under -race)
	const N = 40
	done := make(chan struct{}, N)
	for i := 0; i < N; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			resp, err := http.Get(ts.URL + "/api/comments/guide?status=all")
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}
	for i := 0; i < N; i++ {
		<-done
	}
	got := listThreads(t, ts, "all")
	if len(got) != 3 {
		t.Fatalf("want 3 threads after concurrent heals, got %d", len(got))
	}
}

func TestOrphanThenRestore(t *testing.T) {
	ts, docs, rnd := newTestServer(t)
	createComment(t, ts, rnd, "main semester fee", "which fee?")

	// delete the quoted text → the thread orphans
	gone := strings.Replace(sampleDoc, "Pay the main semester fee at registration.", "Pricing is described elsewhere.", 1)
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(gone), 0o644); err != nil {
		t.Fatal(err)
	}
	got := listThreads(t, ts, "all")
	if len(got) != 1 || !got[0].Orphaned {
		t.Fatalf("expected the thread to orphan, got %+v", got)
	}

	// restore the text → the thread un-orphans and re-anchors
	if err := os.WriteFile(filepath.Join(docs, "guide.md"), []byte(sampleDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	got = listThreads(t, ts, "all")
	if len(got) != 1 || got[0].Orphaned {
		t.Fatalf("expected the thread to recover after restore, got %+v", got)
	}
}

func TestDocLevelResolveCycle(t *testing.T) {
	ts, _, _ := newTestServer(t)
	// create a doc-level note
	code, data := reqJSON(t, "POST", ts.URL+"/api/comments/guide", map[string]any{
		"scope": "doc", "body": "overall note", "author": "human",
	})
	if code != http.StatusCreated {
		t.Fatalf("create doc note: %d", code)
	}
	var th store.Thread
	_ = json.Unmarshal(data, &th)

	// resolve then reopen — a doc-level note behaves like any thread and never orphans
	if c, _ := reqJSON(t, "PATCH", ts.URL+"/api/threads/"+itoa(th.ID), map[string]any{"status": "resolved", "by": "ai"}); c != http.StatusOK {
		t.Fatalf("resolve doc note: %d", c)
	}
	res := listThreads(t, ts, "all")
	if len(res) != 1 || res[0].Status != "resolved" || res[0].Orphaned {
		t.Fatalf("doc note should be resolved and never orphaned, got %+v", res)
	}
	if c, _ := reqJSON(t, "PATCH", ts.URL+"/api/threads/"+itoa(th.ID), map[string]any{"status": "open", "by": "ai"}); c != http.StatusOK {
		t.Fatalf("reopen doc note: %d", c)
	}
	res = listThreads(t, ts, "open")
	if len(res) != 1 {
		t.Fatalf("reopened doc note should be open, got %+v", res)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
