# Security Policy

## Threat model

margin is a **local, offline** tool. By default it binds `127.0.0.1` only and makes
no outbound network connections at runtime. It is designed for a single user (or a
trusted LAN), without authentication.

Key properties:

- **Loopback by default.** `margin serve` refuses a non-loopback `--host` unless you
  pass `--insecure-allow-remote`. Do **not** expose margin on an untrusted network —
  the comment API is unauthenticated by design.
- **Comment text is the one untrusted input.** It is HTML-escaped on render and only
  ever inserted into the DOM via `textContent`, never as HTML.
- **Document sources are trusted** (authored locally by you or your agent).
- **No telemetry, no SaaS, no runtime network calls.**

## Supported versions

margin is pre-1.0; security fixes land on the latest release. Please test against
`main` or the most recent tag before reporting.

## Reporting a vulnerability

Please report security issues **privately**, not via a public issue:

- Use **GitHub → Security → Report a vulnerability** (private advisory) on this repo, or
- email the maintainer listed on the GitHub profile.

Include a description, affected version/commit, and a reproduction if possible. We aim
to acknowledge within a few days. Please give us a reasonable window to ship a fix
before any public disclosure.

## Dependencies

margin keeps its dependency tree intentionally tiny (stdlib + `goldmark` +
`modernc.org/sqlite` + `sergi/go-diff` + `golang.org/x/text`). CI runs `govulncheck`
considerations against the pinned Go toolchain; please flag any advisory you spot.
