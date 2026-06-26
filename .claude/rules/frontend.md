---
paths:
  - "web/**"
---

# Front-end: editorial/manuscript aesthetic (no deps, no web fonts, no JS highlighter)

Design tokens & primitives live in `design-system.css`; the comment layer lives in
`widget.js` (vanilla, no framework). **Aesthetic = an editor's manuscript** (do NOT
regress to generic slate+indigo sans):
- **Type:** a literary SYSTEM serif (`--serif`: Iowan Old Style / Palatino / Charter /
  Georgia) for display AND body; `--sans` only for chrome/labels (small-caps, letter-
  spaced); `--mono` for code. Old-style numerals (`onum`). No web fonts (offline).
- **Color:** warm paper in light, deep warm ink in dark; ONE accent — a copy-editor's
  **red pencil** (`--accent`). Status colors functional only. Keep a faint paper grain.
- **Motifs:** ruled TOC margin, a red "tick" before `h2`, the `margin.` wordmark with a
  red period, a journal **dateline** (status · date) under the title, highlights drawn
  as a goldenrod wash under a red-pencil underline, gutter **annotation dots**, and
  "In the margin" index-card notes in the sidebar.
- Dim the chrome (header/TOC quieter than body); obsess over vertical alignment.

## Select-to-comment
- On `mouseup` over a non-empty selection in `<article>`, float ONE pill **Comment**
  button (~28px, `var(--radius)`, `var(--shadow)`) **above** and centered on the
  selection's `getBoundingClientRect` (never occlude the text). Fade in 120–150ms
  (opacity + 4px translateY, ease-out). Dismiss on collapse/Esc/scroll. `c` key = same.
- On submit capture the W3C anchor: containing block's `data-bid`, a TextQuoteSelector
  `{prefix(≤32), exact, suffix(≤32)}`, and **block-relative** `{start,end}`; POST it.

## Highlights & gutter markers
- Status-tinted `<mark>`, **not color-alone** (WCAG 1.4.1): OPEN = soft amber fill
  (`--hl-open`) + 1px accent underline; RESOLVED = faint neutral (`--hl-resolved`), no
  underline, recede it; ORPHANED = **no live in-body highlight** (text may be wrong) —
  sidebar only. `border-radius:2px` + `.05em` padding so multi-line marks aren't blocks.
- Right-gutter markers: small bubble glyph (14–16px, `--accent`) aligned to the range's
  `getClientRects()[0].top` (first line, not block top). On row hover, brighten/scale the
  matching marker (1.0→1.08, 120ms). N>1 on a line → ONE marker + count badge; click
  expands a stacked picker (never overlap glyphs). Markers are focusable `<button>`s.

## Sidebar & threads
- Right panel, tabs **Open | Resolved | Orphaned** with live count chips. Orphaned tab is
  **hidden when count 0** (its appearance is itself the "doc drifted" signal). Card = muted
  left-border quote snippet → messages → status chip + actions. Click card → scroll +
  `:target` flash the anchor.
- **Opening the 320px sidebar must NOT reflow the article measure** — on wide viewports
  shift into existing right whitespace; on narrow, overlay with a scrim + focus trap.
- Threads flat (GitHub model), hairline divider + 2px accent rail. One always-visible
  Reply input (expands on focus); Resolve/Reopen top-right; Edit/Delete/Copy-link hover.
  **Badge AI-authored replies** with a distinct monospace `ai` tag.
- Orphans (honest, Hypothesis model): WARN/amber palette (expected, not an error), last-known
  quote as muted italic blockquote + reason + relative time. Medium-confidence matches are a
  *suggested* re-anchor the human confirms — never auto-attach.

## Motion & a11y
- 120–200ms for all comment-layer interactions; animate **only** transform + opacity
  (sidebar = `translateX`, never width/left). ease-out `cubic-bezier(0.4,0,0.2,1)`. Use
  motion to signal state (resolve fades amber→muted). No pulsing/looping. Short > long.
- Gate ALL motion behind `@media (prefers-reduced-motion: reduce)` (functional transitions
  become instant, not broken).
- W3C **ARIA Annotations**: link each `<mark>` to its thread via `aria-details`; thread
  container `role="comment"` + accessible name. Every interactive piece is a focusable
  control with a visible `:focus-visible` ring. Keyboard map: `c` comment, `j`/`k`
  next/prev thread, `r` reply, `e` resolve/reopen, `Enter` jump-to-anchor, `Esc` close;
  popover traps focus and restores it to the originating marker on Esc.
- Typography: serif body ~1.18rem/1.72, `--measure` ~40rem; serif headings with tight
  leading; `--sans` reserved for uppercase letter-spaced labels/chips.
