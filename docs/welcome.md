---
title: Welcome to margin
date: 2026-06-25
status: draft
---

margin renders this Markdown file as the page you are reading. Select any text to
leave an inline comment — a reviewer's note travels with the words it's attached to,
and an AI agent can read it back, revise the source, and resolve the thread.

## What this page demonstrates

This document exists so the renderer and the comment layer have something real to
work against: headings (which build the table of contents on the left), callouts,
tables, code, and lists. Try selecting a sentence in this paragraph.

### Headings build the TOC

Every `##` and `###` heading gets a stable anchor and a slot in the sidebar table of
contents. Clicking a TOC entry smooth-scrolls to it and flashes the target.

## Callouts

:::info
Callouts are authored with a `:::type … :::` fence. Supported types are `info`,
`good`, `warn`, and `danger` — each maps to a tinted box from the design system.
:::

:::warn
Comments are the one piece of untrusted input on an otherwise static page, so every
comment body is HTML-escaped on render.
:::

## A table

| Tier | Strategy | When it wins |
|------|----------|--------------|
| 1 | Block-id hit | The block is unchanged |
| 2 | Position-in-block | Text shifted within the block |
| 3 | Context fuzzy | The quote moved; prefix/suffix still nearby |
| 4 | Quote-only fuzzy | The block is gone entirely |

## Code

Fenced code renders plain — styled by CSS, no JavaScript highlighter:

```go
func blockID(text string) string {
    sum := sha256.Sum256([]byte(normalize(text)))
    return "b-" + hex.EncodeToString(sum[:])[:8]
}
```

## A short list

- Local and offline — served from `http://127.0.0.1`.
- A single static binary, assets embedded.
- Token-efficient: an agent fetches a compact list of open comments instead of
  re-reading the whole document.

## Footnotes and definitions

Footnotes render as numbered endnotes with a back-reference,[^anchor] and
definition lists get a proper `<dl>`:

Anchor
: A comment's attachment to a span of text — a block id, a quoted exact, and a
  block-relative offset.

Orphan
: An anchor whose text can no longer be found with confidence. Surfaced in the
  sidebar, never silently mis-attached.

[^anchor]: A first-party goldmark extension — no new dependency, still a single
static binary.

> "Prefer an honest orphan over a confident mis-anchor."
