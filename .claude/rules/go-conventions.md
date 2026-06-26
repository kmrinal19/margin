---
paths:
  - "**/*.go"
---

# Production-Go conventions for margin (do not deviate)

Sourced from Effective Go, Google Go Style Guide, Uber Go Style Guide, and Go Code
Review Comments (web-verified 2026-06-25). Each rule is enforceable; `golangci-lint`
and the gofmt hook back most of them.

## Layout & packages
- Keep the layout: `cmd/margin/main.go`; logic under `internal/{server,render,anchor,store,client}`; embedded assets in `web/`. **No `pkg/` dir; do not adopt golang-standards/project-layout.** (go.dev/doc/modules/layout)
- One package per directory; package name = last path element. Name packages for what they provide (`render`, `anchor`, `store`) — **never** `util`/`common`/`helpers`/`types`.
- Minimize each package's exported surface. Export only what `cmd/margin` and siblings need; everything else lowercase.

## Errors
- **Return errors; never panic for ordinary failures** (missing doc, bad anchor, SQLITE_BUSY are returned). Reserve `panic` for genuinely impossible states (e.g. a `//go:embed` asset failing to parse at init).
- Wrap with `fmt.Errorf("context: %w", err)` **only when a caller needs `errors.Is/As`**; otherwise `%v` to sever the chain at a boundary. Verb goes last.
- Error strings: lowercase, no trailing punctuation. Sentinels are `Err`-prefixed (`var ErrDocNotFound = errors.New("doc not found")`); typed errors are `Error`-suffixed (`OrphanError`), extracted with `errors.As`.
- Never discard an error with `_` (errcheck enforces). The one legitimate ignore — `http.ErrServerClosed` from `ListenAndServe` — uses `errors.Is`, not blind discard.

## Context
- Every function doing I/O or that can block takes `ctx context.Context` as its **first** parameter (DB `QueryContext`/`ExecContext`, CLI HTTP calls, handlers). **Never store a Context in a struct field.**
- Derive request ctx from `r.Context()`; derive the CLI's ctx from `signal.NotifyContext` so Ctrl-C cancels in-flight work. `context.Background()` only at the top of `main`/tests. CLI `http.Client{Timeout: 10s}`.

## Types, interfaces, construction
- **Return concrete types** from constructors (`NewStore(...) (*Store, error)`). Accept narrow interfaces only where a behavior is genuinely needed, and **define margin-specific interfaces in the consuming package** (e.g. a small `threadStore` in `internal/server`), not the implementer. Don't pre-create interfaces for mocking.
- Pass interfaces by value, never `*Interface`. Add compile-time assertions for exported types that must satisfy an interface: `var _ http.Handler = (*Server)(nil)`.
- Make zero values useful: embed `sync.Mutex` as a non-pointer field. Inject deps (the two `*sql.DB` handles, renderer, `*slog.Logger`) through constructors — **no package-level mutable globals, no goroutines in `init()`**.
- Prefer a plain `Config` struct passed to `server.New(cfg)` over functional options for the small, mostly-required serve flags.
- Don't mix pointer and value receivers on one type. Mutex-bearing/mutated types use pointer receivers.

## Concurrency
- Every goroutine you spawn (file-watcher, checkpoint loop) has a defined stop tied to the shutdown context and is waited on via `sync.WaitGroup` (or `errgroup` if it returns errors). `net/http` already manages per-request goroutines — the risk is the ones you add.
- Buffered channels are size 1 or unbuffered unless justified. No goroutine leaks; always provide a way to stop.

## Logging & config
- `log/slog` only, `TextHandler` to `os.Stderr`, one `*slog.Logger` injected from `main`; level via a `slog.LevelVar` wired to `--verbose`. CLI `--json` payloads go to **stdout**, logs to **stderr**.

## Testing
- Table-driven + `t.Parallel()` subtests; `cmp.Diff(want, got)` (go-cmp) over `reflect.DeepEqual`. Golden files under `testdata/` with a `-update` flag for `internal/render` HTML and `internal/anchor` cascade output. Temp-file SQLite DB per test via `t.Cleanup`. No testify unless a real mocking need appears.

## Tooling (enforced)
- Format with `gofmt`/`gofumpt`; run `go vet ./...` on every change. `golangci-lint run` per `.golangci.yml`. `go test -race ./...` (the server is multi-session). `govulncheck ./...` periodically. Keep `go.sum` committed; run `go mod tidy`.
- Keep the dependency tree tiny and justified — each dep undermines the offline single-binary guarantee.
