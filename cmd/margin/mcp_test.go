package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kmrinal19/margin/internal/mcp"
	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
)

// TestMCPToolsRoundTrip drives the real margin MCP tools (over the real store +
// renderer + re-anchor) the way an agent would: list, read, resolve.
func TestMCPToolsRoundTrip(t *testing.T) {
	docs, data := t.TempDir(), t.TempDir()
	const md = "## Intro\n\nThe quick brown fox jumps over the lazy dog.\n"
	if err := os.WriteFile(filepath.Join(docs, "t.md"), []byte(md), 0o644); err != nil {
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
	bid := ""
	for _, b := range rnd.Blocks([]byte(md)) {
		if strings.Contains(b.Text, "quick brown fox") {
			bid = b.ID
		}
	}
	if _, err := st.CreateThread(context.Background(), "t",
		store.Anchor{BlockID: bid, QuoteExact: "quick brown fox"}, "human", "right animal?"); err != nil {
		t.Fatal(err)
	}

	srv := &mcp.Server{
		Name:         "margin",
		Version:      "test",
		Instructions: mcpInstructions,
		Tools:        (&mcpTools{docsDir: docs, rnd: rnd, st: st}).all(),
	}

	call := func(name, args string) map[string]any {
		t.Helper()
		line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}` + "\n"
		var out bytes.Buffer
		if err := srv.Serve(context.Background(), strings.NewReader(line), &out); err != nil {
			t.Fatal(err)
		}
		var resp map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
			t.Fatalf("decode: %v (%s)", err, out.String())
		}
		res, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("no result: %s", out.String())
		}
		return res
	}
	text := func(res map[string]any) string {
		return res["content"].([]any)[0].(map[string]any)["text"].(string)
	}

	// list_docs surfaces the doc + open count
	if got := text(call("list_docs", "{}")); !strings.Contains(got, `"t"`) || !strings.Contains(got, `"open":1`) {
		t.Errorf("list_docs = %s", got)
	}

	// open_comments returns the token-minimal thread
	oc := text(call("open_comments", `{"slug":"t"}`))
	if !strings.Contains(oc, "quick brown fox") || !strings.Contains(oc, "right animal?") {
		t.Errorf("open_comments = %s", oc)
	}

	// resolve_thread closes it
	if r := call("resolve_thread", `{"thread_id":1,"note":"fixed"}`); r["isError"].(bool) {
		t.Errorf("resolve_thread errored: %s", text(r))
	}
	// now no open comments remain
	if got := text(call("open_comments", `{"slug":"t"}`)); !strings.Contains(got, "[]") {
		t.Errorf("expected no open comments after resolve, got %s", got)
	}

	// bad input is reported as a tool error, not a crash
	if r := call("open_comments", `{"slug":"../etc"}`); !r["isError"].(bool) {
		t.Error("invalid slug should be a tool error")
	}
}
