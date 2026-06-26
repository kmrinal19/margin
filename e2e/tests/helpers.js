// Shared helpers for margin E2E tests.

// selectText programmatically selects `sub` anywhere within the article, then
// dispatches mouseup so the widget shows its "Comment" pill — mirroring what a
// real text selection does. The gutter layer is excluded from the search.
async function selectText(page, sub) {
  await page.evaluate((sub) => {
    const root = document.getElementById("mg-article");
    if (!root) throw new Error("no #mg-article");
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode(n) {
        return n.parentElement && n.parentElement.closest(".mg-gutter")
          ? NodeFilter.FILTER_REJECT
          : NodeFilter.FILTER_ACCEPT;
      },
    });
    // Build the full text + a list of (node, start) to map an index back.
    let full = "";
    const map = [];
    let n;
    while ((n = walker.nextNode())) {
      map.push({ node: n, start: full.length });
      full += n.nodeValue;
    }
    const idx = full.indexOf(sub);
    if (idx < 0) throw new Error("substring not found: " + sub);
    const end = idx + sub.length;
    function at(offset) {
      for (let i = map.length - 1; i >= 0; i--) {
        if (map[i].start <= offset) return { node: map[i].node, off: offset - map[i].start };
      }
      return { node: map[0].node, off: 0 };
    }
    const a = at(idx),
      b = at(end);
    const range = document.createRange();
    range.setStart(a.node, a.off);
    range.setEnd(b.node, b.off);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
    document.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
  }, sub);
}

module.exports = { selectText };
