---
paths:
  - "internal/server/**/*.go"
---

# HTTP server

## Router
- stdlib `net/http` enhanced `ServeMux` (Go 1.22+). Method+path patterns:
  `mux.HandleFunc("POST /api/comments/{slug...}", h)` + `r.PathValue("slug")`.
  Covers all of §10 with zero deps. No chi/gin.
- **Slugs are nested** (`payments/refunds`). A multi-segment `{slug...}` must be the
  **last** path element, so per-doc comment routes are `/api/comments/{slug...}`, the
  doc page is `/doc/{slug...}`, and `validSlug` checks each `/`-segment (rejecting `.`
  so traversal is impossible). The catch-all 404 is `GET /` (scoped to GET so wrong-method
  requests still get stdlib's 405+Allow).

## http.Server config (never the zero value)
```go
srv := &http.Server{
    Handler:           mux,           // wrapped in the middleware chain
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       15 * time.Second,
    WriteTimeout:      15 * time.Second,
    IdleTimeout:       120 * time.Second,
    MaxHeaderBytes:    1 << 20,
}
```
- Zero-value timeouts = no timeout → one stalled keep-alive client holds a goroutine+fd forever.
- Bound handler execution with `http.TimeoutHandler(h, 10*time.Second, msg)` — note 10s **< WriteTimeout 15s** so the client actually receives the 503. `Server.*Timeout` does NOT bound handler execution; `TimeoutHandler` is the only stdlib mechanism.
- Bound request bodies with `http.MaxBytesReader` (~`1<<20` for comment POSTs).

## Middleware chain (outermost first)
`recover` (log stack, return 500 — a panic in one session's handler must not crash the shared server) → `timeout` → `slog` request logging.
Each as `func(http.Handler) http.Handler`.

## Lifecycle
- Bind `127.0.0.1` by default. **Pre-bind via `net.Listen`** so an `EADDRINUSE` is reported and exits non-zero — never silently rebind to another port (offline-only, the human's URL is fixed).
- Graceful shutdown: `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM); defer stop()`. Serve in a goroutine; treat `http.ErrServerClosed` (via `errors.Is`) as clean exit; on `<-ctx.Done()` call `srv.Shutdown` with a fresh `context.WithTimeout(~10s)`, then checkpoint + close both DB pools.

## Security (localhost still matters)
- Validate `{slug}` against `^[a-z0-9-]+$`; resolve `docs/<slug>.md` with `filepath.Clean` + a base-dir containment check before opening (path-traversal).
- **HTML-escape all comment text and any echoed quote on render** (`html/template` auto-escapes). The JSON API returns raw text; the client escapes on insertion. Comments are the one untrusted input.
- Behind a `--debug` flag (loopback-only): `import _ "net/http/pprof"` + a few `expvar` counters (renders, anchor-tier hits, open-threads). Off by default.
