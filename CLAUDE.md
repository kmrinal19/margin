# margin — Claude Code guide

`margin` is a local, offline, single static Go binary for reviewing AI-authored
design docs: it renders Markdown (`docs/<slug>.md`) to polished HTML over
`http://127.0.0.1`, lets a human leave GitHub-style inline **anchored** comments,
and exposes those comments via a CLI/REST API so an AI agent can read them, revise
the doc, and resolve them.

See `PRD.md` for the full spec (architecture, anchoring cascade, design system, AI
round-trip, milestones). **Treat `PRD.md` as the source of truth** — when an
architecture decision changes, update `PRD.md` rather than silently diverging.

## Commands

| Command | Purpose |
|---|---|
| `CGO_ENABLED=0 go build -o margin ./cmd/margin` | Build the single static binary |
| `go test -race ./...` | Run tests (race detector — the server is multi-session) |
| `go vet ./...` | Vet |
| `gofmt -w .` | Format (also auto-run by a PostToolUse hook) |
| `golangci-lint run` | Lint (config: `.golangci.yml`) |
| `./margin serve` | Serve → http://127.0.0.1:8848 |
| `./margin docs` | List docs + open-comment counts |
| `./margin comments <slug> --open --json` | Token-minimal open comments (agent) |
| `./margin resolve <thread-id> --note "…"` | Resolve a thread (agent) |

## Layout

- `cmd/margin/` — CLI entry: `serve` + thin-client subcommands (`flag` dispatch).
- `internal/server/` — HTTP routes, handlers, middleware.
- `internal/render/` — Markdown→HTML, block-id stamping, callouts, shell + TOC.
- `internal/anchor/` — block hashing + the 4-tier re-anchor cascade + fuzzy match.
- `internal/store/` — SQLite (two-pool) schema + queries.
- `internal/client/` — the CLI's HTTP client to the API.
- `web/` — embedded assets (`design-system.css`, `widget.js`, `shell.html.tmpl`) via `//go:embed`.
- `docs/` — authored Markdown docs (committed). `data/` — SQLite DB (gitignored).

## Hard invariants (do not break)

- **Offline-only.** Bind `127.0.0.1` by default; never `0.0.0.0`. No outbound network
  at runtime. On port conflict, fail loudly and exit non-zero — never silently rebind.
- **Single static binary.** Pure-Go deps only (`CGO_ENABLED=0` must build). All assets
  embedded with `//go:embed`. Keep deps tiny: stdlib + `goldmark` + `modernc.org/sqlite`
  (+ `go-cmp` test-only). No chi/cobra/testify/third-party logger without a real need.
- **Block IDs are generated, never hand-authored.** `b-<hash>` = first 8 hex of
  `sha256(normalize(block text))`, stamped by the render AST transformer. `normalize()`
  MUST be byte-identical at stamp time and re-anchor time (one shared function) or every
  comment orphans.
- **SQLite is hit concurrently** by multiple AI sessions + the browser. Use the **two-pool**
  pattern: a writer pool (`SetMaxOpenConns(1)`, `_txlock=immediate`) for every
  INSERT/UPDATE/DELETE/tx, a reader pool (N conns) for every SELECT, both WAL.
  **Never hold a write transaction across a render, a network read, or the re-anchor
  cascade** — keep write txns single-digit-ms.
- **Re-anchoring runs server-side (Go).** Heal stored anchors on fuzzy hits; **prefer
  orphan over mis-anchor**. Orphans are surfaced in the sidebar, never silently dropped
  or mis-attached.
- **Comments are the one untrusted input.** HTML-escape all comment text + echoed quotes
  on render.
- **No JS syntax highlighter, no web fonts.** Fenced code renders plain (CSS-only);
  system font stack only — for offline robustness and token cost.
- **CLI = thin client over REST.** Agents never write to SQLite directly. Machine JSON →
  stdout; human/diagnostic logs → stderr (keeps agent payloads token-minimal).

## Conventions

General production-Go rules live in `.claude/rules/go-conventions.md`. Area-specific
rules auto-load when you touch matching files (`.claude/rules/`): `anchoring`,
`sqlite-concurrency`, `goldmark-render`, `http-server`, `cli-json`, `frontend`.

## Stack (web-verified 2026-06-25)

Go 1.26 · stdlib `net/http` (1.22+ `ServeMux`) · `goldmark` v1.8.2 ·
`modernc.org/sqlite` v1.53.0 (pure-Go) · stdlib `flag` + `log/slog` · `go-cmp` (test).
