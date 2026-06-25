# margin

A local, offline, single-binary tool for reviewing AI-authored design docs.

`margin` renders Markdown docs as beautiful HTML, lets a human leave GitHub-style
inline anchored comments (threads, replies, resolve), and exposes those comments
through a CLI/REST API so an AI agent can read them, revise the doc, and resolve
them — a tight human-↔-AI review loop, served over `http://localhost`, no SaaS.

## Status

**Spec only — no code yet.** The full build spec lives in **[PRD.md](./PRD.md)**
(architecture, design system, anchoring algorithm, the AI round-trip, milestones,
and the open decisions to settle at build start).

## Planned shape

- **Language:** Go (single static binary via `go build`; assets embedded with `embed`).
- **Stack (recommended):** stdlib `net/http` (1.22+ `ServeMux`), `goldmark` (Markdown),
  `modernc.org/sqlite` (pure-Go SQLite). See PRD §8 / §14.
- **Run:** `margin serve` → open `http://127.0.0.1:8848`.
- **Review loop:** human comments in the browser → `margin comments <slug> --open --json`
  → agent revises → `margin resolve <thread-id>`.

## Next step

Start a build session from this repo and work through the milestones in PRD §13,
resolving the open decisions in PRD §14 first.
