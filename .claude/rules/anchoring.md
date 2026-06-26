---
paths:
  - "internal/anchor/**/*.go"
---

# Re-anchoring cascade (server-side)

Comments must survive edits. On render and when serializing threads for the API,
re-resolve each open thread via a 4-tier cascade, **first validated hit wins**.

## normalize() — shared, byte-identical
The single most dangerous bug: a normalization mismatch silently orphans *every*
comment. Use ONE `normalize()` (NFC + collapse runs of whitespace to single space,
trim) shared between `internal/render` (block-id stamp) and `internal/anchor`
(re-resolve). Block ID = `"b-" + hex(sha256(normalize(blockText)))[:8]`.

## The cascade
1. **Block-id hit** — element with matching `data-bid`; `text[start:end] == exact`. Fast path.
2. **Position-in-block** — block found, text shifted; validate the sliced substring at `{start,end}`; if it differs, search within the block.
3. **Context fuzzy** — block found, `exact` moved; fuzzy-search `prefix`/`suffix` near the expected offset (hint = `pos.start`).
4. **Quote-only fuzzy** — block gone; fuzzy-search `exact` across the whole doc (Bitap), conservative threshold.

## Rules
- **Re-validate the sliced substring against stored `exact` after any structural hit.**
- **Heal** (rewrite the stored anchor: block_id, prefix/suffix, start/end, confidence) whenever tiers 2–4 succeed, so drift doesn't compound across renders.
- **Prefer orphan over mis-anchor.** A too-loose match pointing the agent at the wrong span is worse than an honest orphan. Mark failed threads `orphaned`; surface them with their last-known quote. Never auto-attach a low-confidence fuzzy match.
- Tuning (diff-match-patch style): `MatchThreshold ≈ 0.3–0.4` (tighter than the 0.5 default → prefer orphan), `MatchDistance ≈ 1000`. Keep `exact` ≤ ~32 chars (the real constraint is the Bitap int bitmask `1<<(len-1)`).
- Fuzzy impl: hand-rolled Bitap (zero-dep, preferred) or `github.com/sergi/go-diff/diffmatchpatch` `MatchMain(text, pattern, loc)` with `dmp.MatchThreshold`/`MatchDistance` set as above.
- Anchor offsets are **block-relative**, never document-global (editing block 3 must not shift block 40's comments).
- The cascade returns an `anchorStatus` per thread so both the widget and the AI see the same freshly-resolved state.

## Coordinate space (widget ↔ server) — must stay unified
- The widget (`web/widget.js`) and the Go cascade resolve in the SAME space: **whitespace-collapsed, code-point (rune) offsets**. The widget's `collapse()` mirrors `anchor.Normalize`'s whitespace handling and records a normalized-codepoint → raw-UTF-16 map, so it captures/searches in normalized space and maps back to raw DOM offsets only to paint `<mark>`. Never store or compare raw UTF-16 offsets against the server's rune offsets. (NFC is assumed already applied in real docs — the one documented edge case.)
- **Heal rewrites `quote_exact`** to the freshly-sliced span (`anchor.Result.Exact`, persisted by `UpdateAnchorResolutions`). This keeps `quote_exact == text[char_start:char_end]` after a heal, so the widget can re-locate it and the next resolve hits tier 1 (no write-on-GET storm). Persist heals only on a real field-level change, batched in one writer transaction.
- Fuzzy tiers (2 & 4) must bound the matched span **within a single block** (no crossing the `\n` block separator in `whole`); reject degenerate/out-of-range spans → orphan. The `MatchThreshold` (~0.35) is what enforces "prefer orphan over mis-anchor" for fuzzy.
- Byte-identical repeated blocks get order-disambiguated ids (`b-x`, `b-x-2`); reordering identical blocks may relocate a highlight among indistinguishable occurrences — cosmetic only (same text, no data loss).
