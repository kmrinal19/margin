# margin

A local, offline, single-binary tool for reviewing AI-authored design docs.

`margin` renders Markdown docs as beautiful HTML, lets a human leave GitHub-style
inline **anchored** comments (threads, replies, resolve), and exposes those comments
through a CLI/REST API so an AI agent can read them, revise the doc, and resolve
them — a tight human-↔-AI review loop, served over `http://127.0.0.1`, no SaaS.

The full design spec lives in **[PRD.md](./PRD.md)**.

## Quick start

```sh
# build the single static binary (pure-Go, no cgo)
CGO_ENABLED=0 go build -o margin ./cmd/margin

# serve the docs in ./docs
./margin serve            # → http://127.0.0.1:8848
```

Open `http://127.0.0.1:8848`, pick a doc, **select any text** to leave a comment.

## The review loop

```sh
# the agent lists open comments — token-minimal, no need to re-read the doc
./margin comments welcome --json
# → [{"t":42,"b":"b-9e1c44a2","q":"the main fee","ctx":["paid "," today"],"c":"…","s":"open"}]

# the agent edits docs/welcome.md, then resolves the thread
./margin resolve 42 --note "Renamed to 'block fee'; see the Eligibility section."
```

The human reloads and sees the resolution. Comments **survive edits**: a server-side
4-tier re-anchoring cascade re-locates each comment after the doc changes, healing
anchors that drifted and surfacing anything it can't re-locate as an **orphan** —
never silently lost, never mis-attached.

## CLI

```
margin serve    [--port 8848] [--host 127.0.0.1] [--docs ./docs] [--data ./data]
margin docs                                  list docs + open-comment counts
margin comments <slug> [--all] [--json]      list threads (token-minimal with --json)
margin resolve  <thread-id> [--note "…"]     resolve a thread
margin reopen   <thread-id>                  reopen a resolved thread
margin export   <slug> [--out file.html]     write a portable, self-contained HTML
```

Client subcommands talk to a running `margin serve` (default `http://127.0.0.1:8848`;
override with `--server`).

## Authoring docs

Drop Markdown files in `docs/`; the filename stem is the slug. Optional front matter:

```markdown
---
title: My Design Doc
date: 2026-06-25
status: draft
---
```

GitHub-Flavored Markdown is supported, plus callouts:

```markdown
:::warn Heads up
This renders as a tinted callout box.
:::
```

(`info` · `good` · `warn` · `danger`)

## Stack

Go 1.26 · stdlib `net/http` (1.22+ `ServeMux`) + `log/slog` · `goldmark` (Markdown) ·
`modernc.org/sqlite` (pure-Go) · `sergi/go-diff` (fuzzy re-anchor) · all front-end assets
embedded with `//go:embed`. No web fonts, no JS framework, no syntax highlighter.

## Development

```sh
make            # list all tasks
make install    # build + install `margin` onto your PATH
make check      # vet + golangci-lint + go test -race  (the pre-push gate)
make e2e        # build, then run the Playwright browser tests
make run        # go run the server (make run ARGS="--port 8848")
```

The underlying commands, if you prefer them raw:

```sh
go test -race ./...          # unit + integration tests
go vet ./...
golangci-lint run            # config: .golangci.yml
cd e2e && npm install && npx playwright test   # browser E2E of the comment UI
```

CI (`.github/workflows/ci.yml`) runs vet + race tests + golangci-lint + the
Playwright E2E on every push and pull request.

Architecture, layout, and the hard invariants are documented for contributors (human
and AI) in [CLAUDE.md](./CLAUDE.md) and the path-scoped rules under `.claude/rules/`.
