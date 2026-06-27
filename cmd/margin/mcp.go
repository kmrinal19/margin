package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kmrinal19/margin/internal/anchor"
	"github.com/kmrinal19/margin/internal/mcp"
	"github.com/kmrinal19/margin/internal/reanchor"
	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
)

// mcpInstructions is surfaced to the model on connect (MCP `initialize`). It is
// the zero-learning onboarding: a cold agent must be able to run the whole
// review round-trip from this text + the tool descriptions alone. Keep it < 2KB.
const mcpInstructions = `margin is how a human reviewer leaves anchored comments on the Markdown docs in this project; your job is to address them.

Loop:
1. list_docs — see docs and how many open comments each has.
2. open_comments(slug) — get the open threads as compact JSON: t=thread id, b=block id, q=the exact quoted text the comment is attached to, ctx=[prefix,suffix] around it, c=latest comment, s=status. relocated=true means the quoted text may have moved (re-verify it); orphaned=true means it could not be located.
3. For each thread, find the quoted text q inside docs/<slug>.md (use ctx to disambiguate) and EDIT the source file to address the reviewer's comment.
4. resolve_thread(thread_id, note) with a one-line summary of what you changed — OR reply_thread(thread_id, note) to respond WITHOUT resolving (to ask a question or push back).

Always make the actual edit to the .md before resolving. The comments are authoritative human feedback.`

// cmdMCP runs margin as a stdio MCP server: agents launch it and get margin's
// review tools natively. It operates directly on ./docs + ./data (no running
// `margin serve` required) and speaks newline-delimited JSON-RPC on stdin/stdout,
// so logs must never touch stdout.
func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	docsDir := fs.String("docs", "./docs", "directory of Markdown docs")
	dataDir := fs.String("data", "./data", "directory holding the comment store")
	_ = fs.Parse(args)

	ctx, stop := clientCtx()
	defer stop()

	rnd, err := render.New()
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	tools := &mcpTools{docsDir: *docsDir, rnd: rnd, st: st}
	srv := &mcp.Server{
		Name:         "margin",
		Version:      version,
		Instructions: mcpInstructions,
		Tools:        tools.all(),
	}
	return srv.Serve(ctx, os.Stdin, os.Stdout)
}

type mcpTools struct {
	docsDir string
	rnd     *render.Renderer
	st      *store.Store
}

func (m *mcpTools) all() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "list_docs",
			Description: "List the project's Markdown docs and how many open review comments each has.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			Handler:     m.listDocs,
		},
		{
			Name:        "open_comments",
			Description: "Get a doc's review comment threads as compact JSON (t=thread id, b=block id, q=quoted text, ctx=[prefix,suffix], c=latest comment, s=status, relocated/orphaned flags). Locate q in docs/<slug>.md to find what each comment refers to.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"slug":{"type":"string","description":"doc slug, e.g. \"welcome\" or \"payments/refunds\""},"all":{"type":"boolean","description":"include resolved threads (default: open only)"},"full":{"type":"boolean","description":"include the full message history per thread"}},"required":["slug"]}`),
			Handler:     m.openComments,
		},
		{
			Name:        "resolve_thread",
			Description: "Mark a review thread resolved after you have edited the doc to address it. Optionally include a one-line note of what changed (posted as an AI reply).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"thread_id":{"type":"integer"},"note":{"type":"string"}},"required":["thread_id"]}`),
			Handler:     m.resolveThread,
		},
		{
			Name:        "reply_thread",
			Description: "Reply to a review thread WITHOUT resolving it — to answer a question or push back. Posted as an AI reply; the thread stays open.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"thread_id":{"type":"integer"},"note":{"type":"string"}},"required":["thread_id","note"]}`),
			Handler:     m.replyThread,
		},
		{
			Name:        "reopen_thread",
			Description: "Reopen a previously resolved review thread.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"thread_id":{"type":"integer"}},"required":["thread_id"]}`),
			Handler:     m.reopenThread,
		},
	}
}

func (m *mcpTools) listDocs(ctx context.Context, _ json.RawMessage) (string, error) {
	counts, err := m.st.OpenCounts(ctx)
	if err != nil {
		return "", err
	}
	base, err := filepath.Abs(m.docsDir)
	if err != nil {
		return "", err
	}
	type docRow struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
		Open  int    `json:"open"`
	}
	var rows []docRow
	walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != base && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		slug := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		if !render.ValidSlug(slug) {
			return nil
		}
		title := slug
		if src, e := os.ReadFile(path); e == nil {
			title = m.rnd.DocMeta(slug, src).Title
		}
		rows = append(rows, docRow{Slug: slug, Title: title, Open: counts[slug]})
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return "", walkErr
	}
	return marshalJSON(rows)
}

func (m *mcpTools) openComments(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Slug string `json:"slug"`
		All  bool   `json:"all"`
		Full bool   `json:"full"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if !render.ValidSlug(a.Slug) {
		return "", fmt.Errorf("invalid slug %q", a.Slug)
	}
	filter := store.FilterOpen
	if a.All {
		filter = store.FilterAll
	}
	threads, err := m.st.ListThreads(ctx, a.Slug, filter)
	if err != nil {
		return "", err
	}
	// re-resolve against the current source (read-only; never persists a heal)
	if src, e := os.ReadFile(filepath.Join(m.docsDir, filepath.FromSlash(a.Slug)+".md")); e == nil {
		threads = reanchor.ResolveAll(anchor.NewDoc(m.rnd.Blocks(src)), threads)
	}
	return marshalJSON(toCompact(threads, a.Full))
}

func (m *mcpTools) resolveThread(ctx context.Context, args json.RawMessage) (string, error) {
	id, note, err := threadArgs(args, false)
	if err != nil {
		return "", err
	}
	if err := m.st.SetStatus(ctx, id, store.StatusResolved, "ai", note); err != nil {
		return "", err
	}
	return fmt.Sprintf("resolved thread #%d", id), nil
}

func (m *mcpTools) replyThread(ctx context.Context, args json.RawMessage) (string, error) {
	id, note, err := threadArgs(args, true)
	if err != nil {
		return "", err
	}
	if _, err := m.st.AddReply(ctx, id, "ai", note); err != nil {
		return "", err
	}
	return fmt.Sprintf("replied to thread #%d", id), nil
}

func (m *mcpTools) reopenThread(ctx context.Context, args json.RawMessage) (string, error) {
	id, _, err := threadArgs(args, false)
	if err != nil {
		return "", err
	}
	if err := m.st.SetStatus(ctx, id, store.StatusOpen, "ai", ""); err != nil {
		return "", err
	}
	return fmt.Sprintf("reopened thread #%d", id), nil
}

// threadArgs extracts {thread_id, note}; noteRequired enforces a non-empty note.
func threadArgs(args json.RawMessage, noteRequired bool) (int64, string, error) {
	var a struct {
		ThreadID int64  `json:"thread_id"`
		Note     string `json:"note"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return 0, "", fmt.Errorf("invalid arguments: %w", err)
	}
	if a.ThreadID <= 0 {
		return 0, "", errors.New("thread_id is required")
	}
	a.Note = strings.TrimSpace(a.Note)
	if noteRequired && a.Note == "" {
		return 0, "", errors.New("note is required")
	}
	return a.ThreadID, a.Note, nil
}

func marshalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
