# margin

[![CI](https://github.com/kmrinal19/margin/actions/workflows/ci.yml/badge.svg)](https://github.com/kmrinal19/margin/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kmrinal19/margin?sort=semver)](https://github.com/kmrinal19/margin/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/kmrinal19/margin)](go.mod)

A local, offline, single-binary tool for reviewing AI-authored design docs.

`margin` renders Markdown docs as beautiful HTML, lets a human leave GitHub-style
inline **anchored** comments (threads, replies, resolve), and exposes those comments
through a CLI/REST API so an AI agent can read them, revise the doc, and resolve
them — a tight human-↔-AI review loop, served over `http://127.0.0.1`, no SaaS.

The full design spec lives in **[PRD.md](./PRD.md)**.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/kmrinal19/margin/main/install.sh | sh
```

One command, no Go toolchain needed — it downloads the right prebuilt static binary
for your OS/arch from the [Releases](https://github.com/kmrinal19/margin/releases),
verifies its checksum, and installs it. (Installing this way avoids the macOS
Gatekeeper / Windows SmartScreen prompt a browser download would trigger.)

<details><summary>Other ways</summary>

```sh
go install github.com/kmrinal19/margin/cmd/margin@latest   # if you have Go
# or build from source:
CGO_ENABLED=0 go build -o margin ./cmd/margin
```
</details>

## Quick start

```sh
margin serve              # → http://127.0.0.1:8848 (reviews ./docs)
```

Open `http://127.0.0.1:8848`, pick a doc, **select any text** to leave a comment.
The masthead **Download** menu exports the doc as Markdown, self-contained HTML, or
PDF (via your browser's print) — with an optional toggle to embed the review comments.

## Let your AI agent take over

margin speaks the **Model Context Protocol**, so any MCP-capable agent (Claude Code,
Codex, Cursor, Gemini CLI, Windsurf, …) can read and resolve your comments natively —
with **zero edits to your repo or instruction files**. One command wires them up:

```sh
margin agent-setup        # registers margin's tools in each installed agent's own config
```

Then just tell your agent *"address the review comments."* It calls margin's tools
(`open_comments`, `resolve_thread`, `reply_thread`, …) and drives the loop. Under the
hood that's `margin mcp`, a stdio MCP server built into the same binary — offline, no
extra process to manage. Any agent that can run a shell command can also use the CLI
loop below directly.

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
margin comments <slug> [--all] [--json [--full]]   list threads (token-minimal with --json)
margin reply    <thread-id> --note "…"       reply without resolving (agent)
margin resolve  <thread-id> [--note "…"]     resolve a thread
margin reopen   <thread-id>                  reopen a resolved thread
margin delete   <thread-id>                  permanently delete a thread
margin export   <slug> [--out file.html]     write a portable, self-contained HTML
margin mcp      [--docs ./docs] [--data ./data]   run as a stdio MCP server (agents)
margin agent-setup [--print-only]            register margin's MCP tools with installed agents
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

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](./CONTRIBUTING.md) for the dev setup
and the gates, and [SECURITY.md](./SECURITY.md) to report a vulnerability privately.

## License

[MIT](./LICENSE) © Mrinal Kumar

