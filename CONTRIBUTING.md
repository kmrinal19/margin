# Contributing to margin

Thanks for your interest! margin is a small, deliberately-scoped tool, so a quick
read here will save a round-trip.

## Principles (please don't regress these)

margin holds a few **hard invariants** — see [`CLAUDE.md`](./CLAUDE.md) and
[`PRD.md`](./PRD.md) for the full rationale:

- **Offline-only.** Bind `127.0.0.1` by default; no outbound network at runtime.
- **Single static binary.** `CGO_ENABLED=0` must build. Pure-Go deps only — stdlib +
  `goldmark` + `modernc.org/sqlite` + `sergi/go-diff` + `golang.org/x/text`. Don't add
  a dependency without a strong reason.
- **No web fonts, no JS syntax highlighter.** CSS-only, system fonts.
- **Comments are untrusted input** — always HTML-escaped.
- **Prefer an honest orphan over a confident mis-anchor** in the re-anchoring cascade.

`PRD.md` is the source of truth — if you change an architecture decision, update it.

## Dev setup

```sh
git clone https://github.com/kmrinal19/margin && cd margin
CGO_ENABLED=0 go build -o margin ./cmd/margin
./margin serve            # http://127.0.0.1:8848
```

## Before you open a PR

All of these must be green (CI enforces them):

```sh
gofmt -l .                 # formatting (also auto-run by a hook)
go vet ./...
go test -race ./...        # the server is multi-session — keep the race detector clean
golangci-lint run          # config: .golangci.yml (golangci-lint v2)
CGO_ENABLED=0 go build -o margin ./cmd/margin
```

Front-end / comment-layer changes also run the Playwright E2E suite:

```sh
cd e2e && npm install && npx playwright install --with-deps chromium && npx playwright test
```

Render/cascade golden files: regenerate with `go test ./internal/render -update` and
review the diff.

## Conventions

- Production-Go rules live in [`.claude/rules/go-conventions.md`](./.claude/rules/);
  area-specific rules sit alongside it (anchoring, sqlite-concurrency, http-server,
  goldmark-render, cli-json, frontend).
- Keep commits focused; conventional-commit prefixes (`feat:`, `fix:`, `perf:`,
  `test:`, `docs:`, `chore:`) are appreciated and feed the release changelog.
- Add tests for behavior changes. Bug fixes should come with a regression test.

## Reporting issues

Use the issue templates. For security problems, see [`SECURITY.md`](./SECURITY.md)
(report privately, not via a public issue).
