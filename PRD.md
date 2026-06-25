# margin — Product Requirements Document

**Status:** Draft v1 · **Date:** 2026-06-25 · **Owner:** Mrinal Kumar
**One-liner:** A local, offline, single-binary Go tool for reviewing AI-authored design docs — renders Markdown as beautiful HTML, lets a human leave GitHub-style inline anchored comments, and exposes those comments through a CLI/REST API so an AI agent can read them, revise the doc, and resolve them.

> This document is the build spec. It is intentionally detailed so a fresh session (human or agent) can implement `margin` from cold without re-deriving the design. Sections 1–7 are the "what," 8–12 the "how," 13–15 the plan, and the Appendices carry the copy-pasteable specs (design system CSS, anchoring algorithm, AI round-trip shapes).

---

## 1. Summary

`margin` is a personal/small-team tool that closes the loop between an AI that *writes* design docs and a human who *reviews* them. The AI authors a doc as Markdown; `margin serve` renders it as a polished HTML page over `http://localhost`; the human leaves inline, text-anchored comments (threads, replies, resolve) exactly like a GitHub PR review; and the AI then reads those comments back through a CLI, locates each commented span in the source, revises, and marks the thread resolved.

It is **fully local and offline** (localhost, no SaaS, no network), ships as a **single static binary**, and is **token-efficient by design** — the AI pulls a compact structured list of open comments (~hundreds of tokens) instead of re-reading the whole document (~thousands).

The novel part is the **AI round-trip**: most commenting tools assume only humans read comments. `margin` treats "an agent consumes the comments programmatically" as a first-class, primary use case.

---

## 2. Background & motivation

How we got here (so the rationale isn't lost):

1. An AI assistant (Claude Code) writes design/spec docs as self-contained HTML. They look good but are a **dead end for review** — no way to comment.
2. **Offline single-file widget** (comments in `localStorage`, export JSON): clunky round-trip — the human exports a file, then has to hand it to the AI; `file://` breaks `localStorage`/`fetch` in inconsistent ways across browsers.
3. **Cloud docs** (Confluence / Google Docs): great commenting, but require auth, aren't offline, and (Google Docs) can't be read back programmatically.
4. **A local server** dissolves the worst problems — serving over `http://localhost` gives a real origin (so `fetch`/`localStorage`/same-origin POST all work), persists comments centrally, and exposes an API the AI can query. This is the chosen shape.
5. **Build vs buy:** a scan of ~14 tools found exactly one close match — [`jot`](https://github.com/badlogic/jot) (MIT, "built for humans and agents"). It was rejected for *this* project because (a) it isn't themeable to our "awe-grade" design bar, (b) it's young (v0.1.x), (c) Node runtime, and (d) we want to own the round-trip + aesthetics. See Appendix D.
6. Decision: **build a bespoke tool in Go.** This PRD is that spec.

**Design ethos:** the rendered docs must be genuinely beautiful ("leaves people in awe"), and the whole system must not waste tokens.

---

## 3. Goals

- **G1 — Beautiful rendering.** Markdown → an "awe-grade" HTML page using a fixed, refined design system (Appendix A): system fonts, light+dark, sticky TOC, callouts, schema blocks, tables, code, print support.
- **G2 — GitHub-style inline comments.** Select text → comment; threaded replies; resolve/reopen; right-gutter markers; comments sidebar with Open/Resolved/Orphaned filters; keyboard nav.
- **G3 — Comments survive edits.** Robust re-anchoring across re-renders; anything that can't be re-anchored surfaces as an **orphan** — never silently lost, never silently mis-attached.
- **G4 — AI round-trip.** An agent can: list **open** comments in a token-minimal form, locate each in the source via (block-id + quote), revise, and resolve — primarily via a **CLI**, backed by REST.
- **G5 — Local, offline, single binary.** `http://localhost`, no SaaS, `go build` → one static binary, near-zero setup.
- **G6 — Token-efficient end to end.** Render-on-read, structured comment API, no re-emitting chrome, no heavyweight client deps.

## 4. Non-goals (v1)

- ❌ Public internet exposure / hardened multi-tenant serving. **Localhost (or trusted LAN) only.**
- ❌ Accounts/auth in v1 (single user; LAN identity is just a name string).
- ❌ Real-time collaborative editing (that's `jot`'s territory; not needed here).
- ❌ WYSIWYG editing — docs are authored as Markdown files in the user's/agent's editor.
- ❌ JS syntax highlighting; ❌ web fonts. (CSS-only code blocks, system font stack — both for token cost and offline robustness.)
- ❌ Large-scale multi-writer concurrency guarantees.

## 5. Users

- **Human reviewer (doc owner).** Opens the rendered doc in a browser, reads, leaves anchored comments, threads/resolves, sees orphaned comments.
- **AI agent (author / reviser).** The coding assistant that wrote the doc; consumes comments via the CLI to revise and resolve. Primary "machine" user.
- **(Later) LAN teammates.** Additional reviewers on the same network; identity is a name captured once.

## 6. Core flows

- **A — Author & serve.** Agent writes `docs/<slug>.md` → `margin serve` → human opens `http://localhost:<port>/doc/<slug>`.
- **B — Review.** Human selects text → comment popover → anchored thread created → replies → resolve/reopen.
- **C — AI round-trip.** Agent runs `margin comments <slug> --open --json` → compact JSON (thread id, block id, quote, comment) → locates the span in the `.md` → edits → `margin resolve <thread-id> --note "…"` → human reloads, sees resolved threads + any new orphans.
- **D — Share (later).** `margin export <slug> --inline` → a portable single-file HTML for someone outside the repo.

---

## 7. Functional requirements

### 7.1 Document rendering
- **Source:** Markdown files in `docs/`, one per doc; `slug` = filename stem. Optional YAML front-matter (`title`, `date`, `status`).
- **Render-on-read:** every `GET /doc/<slug>` re-renders from the `.md`. No save-time cache → the page is always consistent with what the agent just wrote. (Optional later: memoize on file `mtime`+hash for very large docs.)
- **Engine:** [goldmark](https://github.com/yuin/goldmark) — CommonMark + GFM (tables, strikethrough, task lists, autolinks), heading anchors/TOC. Fenced code rendered **plain** (CSS-styled, no highlighter).
- **Shell:** wrap rendered HTML in the design-system shell — sticky header, sticky TOC (built from headings), reading-progress bar, centered article column — and inject the comment-widget JS + design CSS from embedded assets.
- **Stable block IDs (powers anchoring):** an AST transformer stamps every block-level node with `id="b-<hash>"`, where `<hash>` is the first 8–10 hex chars of `SHA-256(normalized block text)`. **Auto-generated, never hand-authored.** Editing a block changes its hash (its comments then fall through to the quote-fuzzy tier or become orphans); unedited blocks keep their ID so comments re-attach exactly.

### 7.2 Design system ("awe-grade") — see Appendix A for the full token spec
- System font stack (zero web-font downloads). Slate palette, **light + dark** auto-swapped via `prefers-color-scheme`, all via CSS custom properties.
- 17px / 1.65 body, **68ch** measure, modular type scale; sticky TOC + header + reading-progress; callouts/admonitions, schema/model blocks, tables, badges, blockquotes.
- Polish: smooth scroll + `scroll-margin-top`, `:target` flash, **print stylesheet**, `prefers-reduced-motion`, visible focus rings, WCAG AA/AAA contrast.
- **Callout authoring convention** (decide exact syntax at build): e.g. fenced `:::warn … :::` containers, or python-markdown-style admonitions, mapped by the renderer to `<div class="callout warn">`.

### 7.3 Commenting
- **Create / anchor:** on `mouseup` over a non-empty selection in the article, float a small "comment" button near the selection. On submit, capture a W3C-style anchor (Appendix B): the containing block's `data-bid`, a TextQuoteSelector `{prefix(≤32), exact, suffix(≤32)}`, and a **block-relative** position `{start,end}`. POST to the API.
- **Render:** wrap the anchored range in `<mark>` tinted by status (open = amber, resolved = muted). Right-gutter markers aligned to anchored lines; stacked count badge when multiple share a line. Click a marker/highlight → popover with the thread + reply box + Resolve/Reopen.
- **Sidebar:** collapsible right panel; one card per thread (quote snippet + messages + status chip); filter tabs **Open | Resolved | Orphaned** with live counts; click a card → scroll + flash the anchor.
- **Threads:** flat, single-level replies (GitHub model — no deep nesting). Resolve/reopen at the **thread** level.
- **Keyboard:** `c` comment on selection, `j`/`k` next/prev thread, `r` reply, `e` resolve/reopen, `Enter` jump-to-anchor, `Esc` close.
- **XSS:** **all** comment text (and any echoed quote) is HTML-escaped on render — comments are the one untrusted input in an otherwise static page.
- **Persistence:** server-side **SQLite** is the source of truth (the widget talks to the REST API; served over localhost so there are no `file://` storage issues).

### 7.4 Re-anchoring on edit (Appendix B)
On render (and when serializing threads for the API), re-resolve each open thread via a **4-tier cascade**, accepting the first that validates against the stored `exact`:
1. **Block-id hit** — element with matching `data-bid`; text at stored position still equals `exact`. (Fast path, the common case.)
2. **Position-in-block** — block found, text shifted; validate the sliced substring at `{start,end}`.
3. **Context fuzzy** — block found, `exact` moved; fuzzy-search `prefix`/`suffix` near the expected offset.
4. **Quote-only fuzzy** — block gone; fuzzy-search `exact` across the whole doc (Bitap-style), conservative threshold.

**Heal** (rewrite the stored anchor) whenever tiers 2–4 succeed, so drift doesn't compound across re-renders. **Prefer orphan over mis-anchor** — a too-loose match that points the agent at the wrong span is worse than an honest orphan. Failed threads are marked `orphaned` and shown in the sidebar with their last-known quote. This cascade should run **server-side (Go)** so both the widget and the AI see freshly-resolved anchors + an `anchorStatus`.

### 7.5 AI round-trip — the differentiator
- **Primary interface: a CLI** that is a thin client over the REST API. The agent never touches SQLite directly (no schema coupling, no unvalidated writes, no WAL-read surprises).
- **Commands:** `margin comments <slug> [--open] [--json]`, `margin resolve <thread-id> [--note "…"]`, `margin reopen <thread-id>`, `margin docs`.
- **Token-minimal JSON** (`--open --json`): an array of `{ t, b, q, ctx, c, s }` (Appendix C). The `(b, q)` pair lets the agent locate the exact span **without reading the whole doc**.
- **Resolve** optionally posts an AI reply note, then flips status. On the next render the human sees the resolution and any newly-orphaned threads.
- **Token math:** ~350 tokens to fetch a doc's open comments + a few targeted span reads, vs ~3k–15k to re-read the whole doc → **~8–40× cheaper** per round-trip (Appendix C).

### 7.6 Server & serving
- `margin serve [--port 8848] [--host 127.0.0.1] [--docs ./docs] [--data ./data]` — long-running HTTP on localhost. Serves the doc index, rendered docs, embedded static assets, and the REST API.
- **Single binary:** design-system CSS + widget JS embedded via `//go:embed`. At runtime only `docs/` and `data/` are needed on disk.

---

## 8. Architecture (Go)

### 8.1 Components
- `cmd/margin` — CLI entry (`serve` + client subcommands).
- `internal/server` — HTTP routes + handlers.
- `internal/render` — Markdown → HTML, block-id injection, shell + widget wrap, TOC.
- `internal/anchor` — block hashing + the re-anchor cascade + fuzzy match.
- `internal/store` — SQLite schema + queries.
- `internal/client` — the CLI's HTTP client to the API.
- `web/` — embedded assets (`design-system.css`, `widget.js`, `shell.html.tmpl`).

### 8.2 Tech stack (recommended; genuine choices flagged in §14)
- **Go 1.26** (installed: `go1.26.0 darwin/arm64`).
- **HTTP:** stdlib `net/http` with the Go 1.22+ enhanced `ServeMux` (method + path-pattern routing) — no framework needed. *(Alt: `chi` for middleware ergonomics.)*
- **Markdown:** `goldmark` (de-facto Go md lib, used by Hugo; clean AST transformers for block-id injection).
- **SQLite:** `modernc.org/sqlite` (pure-Go, **no cgo** → trivial single-binary, easy cross-compile). *(Alt: `mattn/go-sqlite3` — cgo, faster, but complicates static builds. **Recommend modernc.**)*
- **Static assets:** stdlib `embed`.
- **CLI:** stdlib `flag` with a small subcommand dispatch (minimal deps). *(Alt: `spf13/cobra` for richer help.)*
- **Fuzzy re-anchor:** hand-rolled Bitap / a small `diff-match-patch` Go port. *(Decision.)*
- **Build:** `go build -o margin ./cmd/margin` → one static binary.

### 8.3 Rendering pipeline
`read docs/<slug>.md` → goldmark parse → **AST transformer stamps `id="b-<hash>"` on block nodes** → render HTML → build TOC from heading nodes → wrap in `shell.html.tmpl` (header / sticky TOC / reading-progress / article) → reference embedded `design-system.css` + `widget.js` → serve. The widget boots, fetches the doc's comments from the API, runs the client-side highlight/marker rendering against the (already server-resolved) anchors.

### 8.4 Directory layout
```
margin/
  cmd/margin/main.go         # CLI: serve + comments + resolve + reopen + docs
  internal/
    server/                  # routes + handlers
    render/                  # md->html, block-id inject, shell wrap, toc
    anchor/                  # block hashing + re-anchor cascade + fuzzy
    store/                   # sqlite schema + queries (WAL)
    client/                  # CLI HTTP client to the API
  web/
    design-system.css        # Appendix A (embedded)
    widget.js                # comment widget (embedded)
    shell.html.tmpl          # doc shell template (embedded)
  docs/                      # authored markdown docs (committed)
  data/                      # sqlite db (gitignored)
  go.mod
  .gitignore                 # data/
  PRD.md
  README.md
```
Run (dev): `go run ./cmd/margin serve` → open `http://127.0.0.1:8848`.

---

## 9. Data model (SQLite)

```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;

CREATE TABLE doc (
  id            INTEGER PRIMARY KEY,
  slug          TEXT NOT NULL UNIQUE,      -- filename stem; docs/<slug>.md
  title         TEXT,
  last_rendered TEXT                       -- ISO8601, informational
);

CREATE TABLE comment_thread (
  id          INTEGER PRIMARY KEY,
  doc_id      INTEGER NOT NULL REFERENCES doc(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'open',   -- 'open' | 'resolved'
  orphaned    INTEGER NOT NULL DEFAULT 0,     -- 1 if last re-anchor failed
  created_at  TEXT NOT NULL,
  resolved_at TEXT,
  resolved_by TEXT
);
CREATE INDEX ix_thread_doc_status ON comment_thread(doc_id, status);

CREATE TABLE anchor (                          -- 1:1 with thread
  thread_id       INTEGER PRIMARY KEY REFERENCES comment_thread(id) ON DELETE CASCADE,
  block_id        TEXT NOT NULL,               -- content-hash block id (data-bid)
  quote_exact     TEXT NOT NULL,               -- TextQuoteSelector: exact
  quote_prefix    TEXT,                         -- up to 32 chars before
  quote_suffix    TEXT,                         -- up to 32 chars after
  char_start      INTEGER,                      -- block-relative offsets (last good)
  char_end        INTEGER,
  last_confidence REAL                          -- 1.0 exact … below threshold = orphan
);

CREATE TABLE comment (
  id         INTEGER PRIMARY KEY,
  thread_id  INTEGER NOT NULL REFERENCES comment_thread(id) ON DELETE CASCADE,
  author     TEXT NOT NULL,                     -- 'human' (single-user) / LAN name / 'ai'
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX ix_comment_thread ON comment(thread_id);
```
Notes: status lives on the **thread** (resolve is thread-level). Anchor offsets are **block-relative**, not document-global — editing block 3 must not shift comments in block 40.

---

## 10. REST API (widget and CLI share these)

| Method | Route | Purpose |
|---|---|---|
| `GET` | `/` | Doc index (list + open-comment counts) |
| `GET` | `/doc/{slug}` | Rendered HTML (design CSS + widget JS), render-on-read |
| `GET` | `/static/{...}` | Embedded assets |
| `GET` | `/api/docs/{slug}/comments?status=open` | List threads (anchors re-resolved server-side) |
| `POST` | `/api/docs/{slug}/comments` | Create thread + anchor + first comment |
| `POST` | `/api/threads/{id}/replies` | Add a reply |
| `PATCH` | `/api/threads/{id}` | Resolve / reopen (`{status, by, note?}`) |

`POST /api/docs/{slug}/comments` body:
```json
{ "anchor": {"block_id":"b-1a2b3c4d","quote_exact":"…","quote_prefix":"…","quote_suffix":"…","char_start":12,"char_end":29},
  "body": "…", "author": "human" }
```

---

## 11. CLI surface

```
margin serve [--port 8848] [--host 127.0.0.1] [--docs ./docs] [--data ./data]
margin docs                                  # list docs + open-comment counts
margin comments <slug> [--open] [--json]     # default --open; token-minimal JSON with --json
margin resolve <thread-id> [--note "…"]      # PATCH status=resolved, author=ai, optional reply
margin reopen  <thread-id>
margin export  <slug> --inline               # (later) portable single-file HTML
```
The client subcommands are thin HTTP calls to a running `serve`. If the server is down, `comments` may fall back to read-only SQLite; **writes always go through the API**.

---

## 12. Non-functional requirements

- **Offline / local.** No network calls. Bind `127.0.0.1` by default. Serving over localhost is the **enabling decision** (it's what makes `fetch`/`localStorage`/same-origin POST work — a `file://` page can't).
- **Single binary.** `go build` → one static artifact; assets embedded. No runtime deps beyond `docs/` + `data/`.
- **Performance.** Render-on-read is microseconds for doc-sized Markdown; no cache-invalidation class of bug. Add `mtime`+hash memoization only if docs get huge.
- **Security.** Localhost only in v1; do **not** expose publicly (the dev-grade server isn't hardened). LAN later: bind `0.0.0.0` behind VPN/LAN only, capture a reviewer name for attribution.
- **Accessibility.** Design system meets WCAG AA (most pairs AAA); visible focus; reduced-motion; print.
- **Token efficiency.** Structured open-comments API; `(block_id, quote)` removes the need to re-read docs; chrome lives in embedded assets, never re-emitted.

---

## 13. Milestones

- **M0 — Scaffold.** `go mod init`, embed, `margin serve` returns a hello page; CLI dispatch.
- **M1 — Render.** Markdown → HTML + design shell + sticky TOC + **block-id injection**. Looks beautiful (Appendix A applied).
- **M2 — Comments (write path).** SQLite store + widget select-to-comment + REST create/list/reply + render highlights & markers.
- **M3 — Anchoring.** Re-anchor cascade (server-side) + heal + **orphan sidebar bucket** + Open/Resolved/Orphaned filters.
- **M4 — AI round-trip.** `margin comments --open --json` + `margin resolve` + token-minimal shapes (Appendix C). End-to-end loop works.
- **M5 — Polish.** Dark mode, print, keyboard nav, sidebar filters/counts, `:target` flash, reduced-motion.
- **M6 — Share.** `export --inline` portable single-file HTML.
- **Later.** LAN multi-user + reviewer identity.

## 14. Open decisions (resolve at build start)

- **Module path / GitHub org** — e.g. `github.com/<handle>/margin`.
- **Router** — stdlib `ServeMux` (recommended) vs `chi`.
- **SQLite driver** — `modernc.org/sqlite` (recommended, pure-Go) vs `mattn/go-sqlite3` (cgo).
- **CLI lib** — stdlib `flag` (recommended) vs `cobra`.
- **Fuzzy** — hand-roll Bitap vs a `diff-match-patch` Go port.
- **Callout syntax** — `:::note` fenced containers vs an admonition extension.
- **Default port** — 8848? (pick something memorable, fail loudly on conflict — never silently rebind).
- **Block-id hash** — content-hash (recommended; edited block → re-anchor via quote) vs stable source-order slug + persisted map. Both have failure modes; content-hash + quote-fuzzy is the recommended combo.
- **Comment authorship** — single-user hardcodes `author='human'`; LAN prompts once.

## 15. Out of scope / YAGNI

Reiterating §4: no public exposure, no accounts (v1), no realtime editing, no WYSIWYG, no JS highlighter, no web fonts, no multi-writer-at-scale promises. Don't add a "production" server stack — there is no production; it's localhost/LAN. Don't pre-build LAN/multi-user until it's actually needed.

---

## Appendix A — Design system (copy-pasteable)

System fonts only (no downloads), slate palette with light + dark via `prefers-color-scheme`, 17px/1.65 body on a 68ch measure. Verified contrast — light: ink `#0f172a` on `#fff` = 16:1 (AAA), muted `#475569` = 7.6:1; dark: ink `#e6edf3` on `#0d1117` = 14.7:1. This is the embedded `web/design-system.css`.

```css
:root{
  /* TYPOGRAPHY */
  --font-sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Apple Color Emoji", "Segoe UI Emoji", sans-serif;
  --font-mono: ui-monospace, "SF Mono", "SFMono-Regular", "Menlo", "Cascadia Code", "Consolas", "Liberation Mono", monospace;
  --fs-xs:0.75rem; --fs-sm:0.875rem; --fs-base:1.0625rem; --fs-h3:1.25rem; --fs-h2:1.5rem; --fs-h1:2rem; --fs-display:2.5rem;
  --lh-body:1.65; --lh-heading:1.22; --lh-tight:1.4; --measure:68ch;
  --tracking-heading:-0.014em; --tracking-display:-0.022em;
  /* SPACING (4px base) */
  --s1:0.25rem; --s2:0.5rem; --s3:0.75rem; --s4:1rem; --s5:1.5rem; --s6:2rem; --s7:3rem; --s8:4rem;
  --radius:8px; --radius-sm:5px; --header-h:56px;
  /* COLOR — LIGHT */
  --bg:#ffffff; --surface:#f8fafc; --surface-2:#f1f5f9; --border:#e2e8f0; --border-strong:#cbd5e1;
  --ink:#0f172a; --muted:#475569; --faint:#64748b; --accent:#4f46e5; --accent-soft:#eef2ff;
  --shadow:0 1px 2px rgba(15,23,42,.04),0 4px 12px rgba(15,23,42,.06);
  --info-fg:#1e40af; --info-bg:#eff6ff; --info-bd:#bfdbfe;
  --good-fg:#166534; --good-bg:#f0fdf4; --good-bd:#bbf7d0;
  --warn-fg:#854d0e; --warn-bg:#fffbeb; --warn-bd:#fde68a;
  --danger-fg:#991b1b; --danger-bg:#fef2f2; --danger-bd:#fecaca;
  /* comment-highlight tints */
  --hl-open:#fde68a66; --hl-resolved:#cbd5e133;
}
@media (prefers-color-scheme: dark){:root{
  --bg:#0d1117; --surface:#161b22; --surface-2:#1c2230; --border:#30363d; --border-strong:#3d444d;
  --ink:#e6edf3; --muted:#9da7b3; --faint:#7d8590; --accent:#818cf8; --accent-soft:#1e2333;
  --shadow:0 1px 2px rgba(0,0,0,.4),0 4px 14px rgba(0,0,0,.5);
  --info-fg:#93c5fd; --info-bg:#13243b; --info-bd:#1e3a5f;
  --good-fg:#86efac; --good-bg:#102a1c; --good-bd:#1d4731;
  --warn-fg:#fcd34d; --warn-bg:#2b220e; --warn-bd:#4a3a12;
  --danger-fg:#fca5a5; --danger-bg:#2c1416; --danger-bd:#4d2326;
  --hl-open:#fcd34d40; --hl-resolved:#30363d55;
}}

*,*::before,*::after{box-sizing:border-box;}
html{scroll-behavior:smooth;-webkit-text-size-adjust:100%;}
body{margin:0;background:var(--bg);color:var(--ink);font-family:var(--font-sans);
  font-size:var(--fs-base);line-height:var(--lh-body);-webkit-font-smoothing:antialiased;
  text-rendering:optimizeLegibility;font-feature-settings:"kern","liga";}

#progress{position:fixed;top:0;left:0;height:3px;width:0;background:var(--accent);z-index:60;transition:width .08s linear;}
.site-header{position:sticky;top:0;z-index:50;height:var(--header-h);display:flex;align-items:center;gap:var(--s4);
  padding:0 var(--s5);background:color-mix(in srgb,var(--bg) 82%,transparent);
  backdrop-filter:saturate(180%) blur(10px);-webkit-backdrop-filter:saturate(180%) blur(10px);border-bottom:1px solid var(--border);}
.site-header .doc-name{font-weight:600;letter-spacing:var(--tracking-heading);font-size:var(--fs-sm);}

.layout{display:grid;grid-template-columns:240px minmax(0,1fr);gap:var(--s7);max-width:1100px;margin:0 auto;padding:var(--s6) var(--s5);}
.toc{position:sticky;top:calc(var(--header-h) + var(--s4));align-self:start;max-height:calc(100vh - var(--header-h) - var(--s6));
  overflow-y:auto;font-size:var(--fs-sm);line-height:var(--lh-tight);}
.toc a{display:block;padding:var(--s1) var(--s3);color:var(--muted);text-decoration:none;border-left:2px solid transparent;border-radius:0 var(--radius-sm) var(--radius-sm) 0;}
.toc a:hover{color:var(--ink);background:var(--surface);}
.toc a.active{color:var(--accent);border-left-color:var(--accent);font-weight:500;}
.toc a.lvl-3{padding-left:var(--s5);font-size:var(--fs-xs);}
article{max-width:var(--measure);}

h1,h2,h3,h4{line-height:var(--lh-heading);letter-spacing:var(--tracking-heading);font-weight:650;margin:var(--s7) 0 var(--s4);scroll-margin-top:calc(var(--header-h) + var(--s4));}
h1{font-size:var(--fs-h1);} h2{font-size:var(--fs-h2);padding-bottom:var(--s2);border-bottom:1px solid var(--border);}
h3{font-size:var(--fs-h3);} h4{font-size:var(--fs-base);color:var(--muted);}
.doc-title{font-size:var(--fs-display);letter-spacing:var(--tracking-display);margin:0 0 var(--s3);line-height:1.1;}
.lead{font-size:1.15rem;color:var(--muted);line-height:1.55;margin-bottom:var(--s6);}
:target{animation:flash 1.6s ease-out;}
@keyframes flash{0%,18%{background:var(--accent-soft);}100%{background:transparent;}}

p,ul,ol{margin:0 0 var(--s4);} li{margin:var(--s1) 0;}
a{color:var(--accent);text-decoration:underline;text-underline-offset:2px;text-decoration-thickness:1px;
  text-decoration-color:color-mix(in srgb,var(--accent) 40%,transparent);}
a:hover{text-decoration-color:var(--accent);}
strong{font-weight:650;} small,.caption{font-size:var(--fs-sm);color:var(--faint);}
hr{border:0;border-top:1px solid var(--border);margin:var(--s7) 0;}

code,kbd,pre,samp{font-family:var(--font-mono);}
p code,li code,td code{font-size:.875em;background:var(--surface-2);border:1px solid var(--border);border-radius:var(--radius-sm);padding:.12em .4em;color:var(--ink);}
pre{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:var(--s4) var(--s5);
  overflow-x:auto;font-size:var(--fs-sm);line-height:1.55;tab-size:2;margin:0 0 var(--s5);box-shadow:var(--shadow);}
pre code{background:none;border:0;padding:0;font-size:inherit;color:var(--ink);}

.callout{border:1px solid var(--border);border-left-width:3px;border-radius:var(--radius);padding:var(--s4) var(--s5);margin:var(--s5) 0;background:var(--surface);}
.callout .c-title{font-weight:650;font-size:var(--fs-sm);letter-spacing:.01em;margin-bottom:var(--s2);display:flex;gap:var(--s2);align-items:center;}
.callout p:last-child{margin-bottom:0;}
.callout.info{border-left-color:var(--info-bd);background:var(--info-bg);} .callout.info .c-title{color:var(--info-fg);}
.callout.good{border-left-color:var(--good-bd);background:var(--good-bg);} .callout.good .c-title{color:var(--good-fg);}
.callout.warn{border-left-color:var(--warn-bd);background:var(--warn-bg);} .callout.warn .c-title{color:var(--warn-fg);}
.callout.danger{border-left-color:var(--danger-bd);background:var(--danger-bg);} .callout.danger .c-title{color:var(--danger-fg);}

.table-wrap{overflow-x:auto;margin:0 0 var(--s5);}
table{border-collapse:collapse;width:100%;font-size:var(--fs-sm);}
th,td{text-align:left;padding:var(--s3) var(--s4);border-bottom:1px solid var(--border);vertical-align:top;}
thead th{font-size:var(--fs-xs);text-transform:uppercase;letter-spacing:.05em;color:var(--muted);background:var(--surface-2);font-weight:600;}
tbody tr:hover{background:var(--surface);}

.schema{border:1px solid var(--border);border-radius:var(--radius);overflow:hidden;margin:0 0 var(--s5);font-family:var(--font-mono);font-size:var(--fs-sm);}
.schema-head{background:var(--surface-2);padding:var(--s3) var(--s4);font-weight:600;border-bottom:1px solid var(--border);}
.schema-row{display:grid;grid-template-columns:minmax(120px,1fr) 110px 2fr;gap:var(--s4);padding:var(--s3) var(--s4);border-bottom:1px solid var(--border);}
.schema-row:last-child{border-bottom:0;} .schema-row .f-type{color:var(--faint);} .schema-row .f-note{font-family:var(--font-sans);color:var(--muted);}

.badge{display:inline-block;font-size:var(--fs-xs);font-weight:600;letter-spacing:.02em;padding:.15em .55em;border-radius:999px;border:1px solid var(--border-strong);background:var(--surface-2);color:var(--muted);}
.badge.info{background:var(--info-bg);color:var(--info-fg);border-color:var(--info-bd);}
.badge.good{background:var(--good-bg);color:var(--good-fg);border-color:var(--good-bd);}
.badge.warn{background:var(--warn-bg);color:var(--warn-fg);border-color:var(--warn-bd);}
.badge.danger{background:var(--danger-bg);color:var(--danger-fg);border-color:var(--danger-bd);}

blockquote{margin:var(--s5) 0;padding:var(--s2) var(--s5);border-left:3px solid var(--border-strong);color:var(--muted);font-style:italic;}

/* COMMENT LAYER */
mark.mg-hl{background:var(--hl-open);border-radius:2px;cursor:pointer;padding:0 .05em;}
mark.mg-hl.resolved{background:var(--hl-resolved);}
.mg-marker{position:absolute;right:0;cursor:pointer;color:var(--accent);}
.mg-sidebar{position:fixed;top:var(--header-h);right:0;width:320px;height:calc(100vh - var(--header-h));
  overflow-y:auto;border-left:1px solid var(--border);background:var(--bg);padding:var(--s4);transform:translateX(100%);transition:transform .2s;}
.mg-sidebar.open{transform:none;}
.mg-card{border:1px solid var(--border);border-radius:var(--radius);padding:var(--s3);margin-bottom:var(--s3);background:var(--surface);}
.mg-card .mg-quote{font-size:var(--fs-xs);color:var(--faint);border-left:2px solid var(--border-strong);padding-left:var(--s2);margin-bottom:var(--s2);}

:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:2px;}
@media (prefers-reduced-motion: reduce){html{scroll-behavior:auto;}*,*::before,*::after{animation-duration:.001ms!important;animation-iteration-count:1!important;transition-duration:.001ms!important;}}
@media (max-width:1024px){.layout{grid-template-columns:minmax(0,1fr);}.toc{display:none;}}
@media print{:root{--bg:#fff;--ink:#000;--muted:#333;--border:#ccc;--surface:#fff;--surface-2:#fff;}
  #progress,.site-header,.toc,.mg-sidebar,.mg-marker{display:none!important;}
  .layout{display:block;max-width:none;padding:0;} article{max-width:none;}
  a{color:#000;text-decoration:underline;} a[href^="http"]::after{content:" (" attr(href) ")";font-size:.8em;color:#555;}
  pre,table,.callout,.schema{break-inside:avoid;} h1,h2,h3{break-after:avoid;}}
```

Doc-chrome JS (reading progress + active-TOC; the only non-widget JS — wrap reduced-motion-safe):
```js
const bar=document.getElementById('progress');
const onScroll=()=>{const h=document.documentElement;bar.style.width=(h.scrollTop/(h.scrollHeight-h.clientHeight)*100)+'%';};
document.addEventListener('scroll',onScroll,{passive:true});onScroll();
const links=[...document.querySelectorAll('.toc a')];
const map=new Map(links.map(a=>[a.getAttribute('href').slice(1),a]));
document.querySelectorAll('h2[id],h3[id]').forEach(h=>new IntersectionObserver(es=>es.forEach(e=>{
  const a=map.get(e.target.id);
  if(e.isIntersecting&&a){links.forEach(l=>l.classList.remove('active'));a.classList.add('active');}
}),{rootMargin:'-10% 0px -80% 0px'}).observe(h));
```

---

## Appendix B — Anchoring (selectors + cascade)

**Stored anchor** (W3C Web Annotation Data Model, block-scoped):
```json
{
  "block_id": "b-9e1c44a2",
  "quote":  { "prefix": "…paid the ", "exact": "MAIN semester fee", "suffix": " before arrival." },
  "pos":    { "start": 12, "end": 29 }      // BLOCK-RELATIVE, not document-global
}
```

**Block id stamping** (render time, Go AST transformer; client equivalent in JS):
```
for each block node: id = "b-" + sha256(normalize(text))[:8]   // normalize = collapse whitespace, NFC
```

**Re-resolution cascade** (first validated hit wins; heal on 2–4; orphan if none):
```
resolveAnchor(a, doc):
  blk = doc.querySelector("[id=a.block_id]")
  if blk:
    t = blk.text
    if t[a.pos.start : a.pos.end] == a.quote.exact:  return hit(blk, a.pos)          # tier 1
    m = fuzzyInBlock(t, a.quote)                                                      # tier 2/3 (prefix/suffix, hint=pos.start)
    if m:  return heal(a, blk, m)
  g = fuzzyWholeDoc(doc.text, a.quote.exact, hint=a.pos.start)                        # tier 4
  if g and scoreOK(g):  return healGlobal(a, g)
  a.status = "orphaned";  return null                                                # never silently drop
```
Tuning (diff-match-patch style): `Match_Threshold ≈ 0.3–0.4` (tighter than default 0.5 → prefer orphan over mis-anchor), `Match_Distance ≈ 1000`, keep `exact` ≤ ~32 chars (Bitap window). **Always re-validate against `exact` after a structural hit**, and **always rewrite (heal) the stored anchor** when a fuzzy tier succeeds.

Pitfalls: global offsets orphan everything on any earlier edit (→ block-relative); content-hash block-id changes when its own block is edited (→ quote-fuzzy must be reliable); mis-anchor is worse than orphan; normalize whitespace identically at store + resolve time.

---

## Appendix C — AI round-trip (shapes + token math)

**`margin comments <slug> --open --json`** → token-minimal array:
```json
[
  { "t": 42, "b": "b-9e1c44a2", "q": "MAIN semester fee",
    "ctx": ["…paid the ", " before arrival."],
    "c": "Should this be the advance fee, not the main one?", "s": "open" }
]
```
`t`=thread id, `b`=block id, `q`=exact quote (the search handle), `ctx`=[prefix,suffix], `c`=latest comment body, `s`=status. Omit resolved threads and all position math — the agent locates the span by searching the `.md` for `q` (disambiguated by `ctx`), edits, and resolves.

**Resolve:** `margin resolve 42 --note "Changed 'MAIN semester fee' → 'block fee'; see updated Eligibility block."`

**Token estimate (one review round-trip):**
- Re-read whole doc: 1.5k-word doc ≈ ~3k tokens; 8k-word doc ≈ ~12–15k tokens.
- Structured open-comments fetch: ~60–90 tokens/thread → ~300 tokens for 4 comments, + ~40 for the CLI call; each `(b,q)` enables a targeted ~50–150-token span read instead of ingesting the doc.
- **Net: ~350 tokens + targeted reads vs 3k–15k → ~8–40× cheaper.** Resolve ≈ ~30 tokens.

---

## Appendix D — Build-vs-buy (why build)

A 14-tool scan against five hard requirements (rendered docs · inline-anchored threaded+resolvable comments · AI-readable comment API · fully local/offline · tolerant of AI re-edits) found exactly one near-match: **`jot`** (github.com/badlogic/jot, MIT, "built for humans and agents") — REST + CLI, text-quote anchors, threads/resolve, disk storage. Rejected for this project because: not themeable to the design bar, young (v0.1.3, Apr 2026), Node runtime, and we want to own the round-trip. Every other candidate failed harder: **Outline** can't create *anchored* comments via API (open FR #12211) and needs Postgres+Redis; **Hypothesis h** is the W3C reference server but needs Postgres+Elasticsearch+RabbitMQ; **BookStack/HedgeDoc** have no inline comments; **Gitea/GitLab** line-comment creation is web-UI-only; **Docusaurus+giscus** is page-level and needs GitHub (not offline). Net: building a small Go tool is warranted — and we reuse `jot`'s text-quote anchoring idea + the W3C/Hypothesis fuzzy cascade.

**References:** github.com/badlogic/jot · w3.org/TR/annotation-model · web.hypothes.is/blog/fuzzy-anchoring · github.com/yuin/goldmark · modernc.org/sqlite · go.dev (net/http ServeMux 1.22 routing).
