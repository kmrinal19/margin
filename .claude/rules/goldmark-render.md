---
paths:
  - "internal/render/**/*.go"
---

# Markdown rendering (goldmark)

## Init
```go
md := goldmark.New(
    goldmark.WithExtensions(extension.GFM),                  // tables, strikethrough, linkify, tasklist
    goldmark.WithParserOptions(
        parser.WithAutoHeadingID(),                          // heading anchors for TOC + :target
        parser.WithASTTransformers(util.Prioritized(blockIDTransformer{}, 100)),
    ),
    goldmark.WithRendererOptions(html.WithUnsafe()),         // we control the source docs; comment text is escaped separately
)
```
- `extension.GFM` is one extender bundling Table/Strikethrough/Linkify/TaskList.
- Fenced code renders **plain** (CSS-styled). No JS highlighter, no web fonts (PRD non-goals).
- YAML front-matter (`title`/`date`/`status`): `yuin/goldmark-meta` (first-party) if needed.

## Block-ID transformer
- Implement `parser.ASTTransformer`; assert `var _ parser.ASTTransformer = (*blockIDTransformer)(nil)`.
- `ast.Walk` the tree; for each **block-level** node compute the content hash from its
  normalized source text and attach `node.SetAttributeString("id", []byte(id))` so the
  HTML renderer emits `id="b-…"`.
- Use the **shared** `normalize()` (see `internal/anchor`) — NFC + collapse whitespace.
  A mismatch here orphans every comment. `crypto/sha256` + `encoding/hex`, no dep.
- goldmark v1.8+ exposes position info on AST nodes (segments → source byte offsets),
  which gives the block's source text for hashing.

## Callouts (`:::warn … :::`)
- Hand-rolled goldmark block parser / AST transformer (no maintained 3rd-party ext
  matches both the bare `:::warn` syntax AND the `.callout`/`.c-title` design markup).
- Map `:::note|info|good|warn|danger` → `<div class="callout warn"><div class="c-title">…</div>…</div>`.

## Output shell
- Wrap rendered HTML in `web/shell.html.tmpl` (header / sticky TOC built from headings /
  reading-progress / centered article). Reference embedded `design-system.css` + `widget.js`.
- Render path: reuse a `sync.Pool` of `*bytes.Buffer` (always `Reset()` before reuse).
- v1 has **no render cache** (render-on-read). Add an ETag/`If-None-Match` 304 path on
  `GET /doc/{slug}` (ETag = hash of file mtime+size + comment-set version) — the human
  reloads the same doc repeatedly, so 304 skips re-render+transfer.
