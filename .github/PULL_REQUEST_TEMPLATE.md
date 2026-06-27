<!-- Thanks for contributing to margin! -->

## What & why

<!-- What does this change and why? Link any issue: Closes #123 -->

## Checklist

- [ ] `gofmt`, `go vet ./...`, `golangci-lint run` are clean
- [ ] `go test -race ./...` passes (added/updated tests for behavior changes)
- [ ] `CGO_ENABLED=0 go build ./cmd/margin` still builds (single static binary)
- [ ] Front-end change? Playwright E2E (`cd e2e && npx playwright test`) passes
- [ ] Respects the hard invariants (offline-only, pure-Go deps, untrusted comments escaped) — see CONTRIBUTING.md
- [ ] Updated `PRD.md` if an architecture decision changed
