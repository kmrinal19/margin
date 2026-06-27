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
	status, data := reqJSON(t, "POST", ts.URL+"/api/docs/guide/comments", map[string]any{
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
	code, data := reqJSON(t, "GET", ts.URL+"/api/docs/guide/comments?status="+status, nil)
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
