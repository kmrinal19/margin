---
paths:
  - "cmd/**/*.go"
  - "internal/client/**/*.go"
---

# CLI + token-minimal JSON (the AI round-trip)

## Surface
```
margin serve [--port 8848] [--host 127.0.0.1] [--docs ./docs] [--data ./data]
margin docs                                  # list docs + open-comment counts
margin comments <slug> [--open] [--json]     # default --open; token-minimal JSON with --json
margin resolve <thread-id> [--note "…"]      # PATCH status=resolved, author=ai, optional reply
margin reopen  <thread-id>
margin export  <slug> --inline               # portable single-file HTML
```
- stdlib `flag` + a manual subcommand switch. Zero deps (keeps the true single binary).
- Thin client over REST: `internal/client` with `http.Client{Timeout: 10s}`, ctx from `signal.NotifyContext`. **Writes always go through the API.** `comments` MAY fall back to read-only SQLite if the server is down; writes never do.

## Token-minimal shape (`comments <slug> --open --json`)
Array of compact objects — omit resolved threads and all position math:
```json
[{ "t": 42, "b": "b-9e1c44a2", "q": "MAIN semester fee",
   "ctx": ["…paid the ", " before arrival."],
   "c": "Should this be the advance fee?", "s": "open" }]
```
`t`=thread id, `b`=block id, `q`=exact quote (search handle), `ctx`=[prefix,suffix],
`c`=latest comment body, `s`=status. The `(b,q)` pair lets the agent locate the span by
searching the `.md` — **no need to re-read the whole doc** (~350 tokens vs 3k–15k).

## I/O discipline
- Machine JSON → **stdout** only. Logs/diagnostics → **stderr**. Never interleave, or
  the agent's `--json` parse breaks. Exit non-zero on error with a one-line stderr message.
- `resolve` optionally POSTs an AI reply note (`author=ai`), then PATCHes status. Badge
  AI-authored content so the human can tell agent replies from their own.
