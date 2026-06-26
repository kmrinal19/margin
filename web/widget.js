/* margin comment widget — vanilla JS, no dependencies.
   Loaded on doc pages. Fetches a doc's comment threads, paints status-tinted
   highlights + gutter markers, drives select-to-comment, and manages the
   threads sidebar (open / resolved / orphaned). Comment text is the one
   untrusted input, so it is only ever inserted via textContent (never HTML). */
(function () {
  "use strict";

  var slug = document.body.dataset.slug;
  var article = document.getElementById("mg-article");
  if (!slug || !article) return; // not a doc page
  article.tabIndex = -1; // focusable programmatically (for focus restore)

  var apiDoc = "/api/docs/" + encodeURIComponent(slug) + "/comments";
  var threads = [];
  var filter = "open";
  var activeTid = null;
  var pending = null; // captured anchor awaiting a composer submit
  var lastFocus = null; // element to restore focus to when the composer closes

  // ── tiny DOM helper ────────────────────────────────────────────────
  function el(tag, attrs) {
    var n = document.createElement(tag);
    if (attrs) {
      for (var k in attrs) {
        var v = attrs[k];
        if (v == null) continue;
        if (k === "class") n.className = v;
        else if (k === "text") n.textContent = v;
        else if (k === "html") n.innerHTML = v; // only ever trusted markup
        else if (k.slice(0, 2) === "on") n.addEventListener(k.slice(2), v);
        else n.setAttribute(k, v);
      }
    }
    for (var i = 2; i < arguments.length; i++) {
      var c = arguments[i];
      if (c == null) continue;
      n.appendChild(c.nodeType ? c : document.createTextNode(c));
    }
    return n;
  }

  var PENCIL =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>';

  function rel(iso) {
    var t = new Date(iso).getTime();
    if (!t) return "";
    var s = (Date.now() - t) / 1000;
    if (s < 45) return "just now";
    if (s < 3600) return Math.max(1, Math.floor(s / 60)) + "m ago";
    if (s < 86400) return Math.floor(s / 3600) + "h ago";
    if (s < 2592000) return Math.floor(s / 86400) + "d ago";
    return new Date(iso).toLocaleDateString();
  }

  function authorLabel(a) {
    return a === "ai" ? "assistant" : a;
  }

  // ── build chrome (sidebar, scrim, pill, composer) ──────────────────
  var gutter = el("div", { class: "mg-gutter", "aria-hidden": "true" });
  article.appendChild(gutter);

  var scrim = el("div", { class: "mg-scrim" });
  scrim.addEventListener("click", closeSidebar);
  document.body.appendChild(scrim);

  var list = el("div", { class: "mg-list", id: "mg-list" });
  var tabsWrap = el("div", { class: "mg-tabs", role: "tablist", "aria-label": "Filter comments" });
  var tabs = {};
  [
    ["open", "Open", ""],
    ["resolved", "Resolved", ""],
    ["orphaned", "Orphaned", "warn"],
  ].forEach(function (t) {
    var chip = el("span", { class: "chip", text: "0" });
    var btn = el(
      "button",
      {
        class: "mg-tab" + (t[2] ? " " + t[2] : ""),
        role: "tab",
        type: "button",
        "data-filter": t[0],
        "aria-selected": "false",
        onclick: function () {
          setFilter(t[0]);
        },
      },
      t[1] + " ",
      chip
    );
    btn._chip = chip;
    tabs[t[0]] = btn;
    tabsWrap.appendChild(btn);
  });

  var sidebar = el(
    "aside",
    { class: "mg-sidebar", id: "mg-sidebar", "aria-label": "Comments", role: "complementary" },
    el("div", { class: "mg-sb-head" }, tabsWrap),
    list
  );
  document.body.appendChild(sidebar);

  var pill = el("button", {
    class: "mg-add",
    type: "button",
    "aria-label": "Comment on selection",
    html: PENCIL + "<span>Comment</span>",
    onclick: openComposer,
  });
  document.body.appendChild(pill);

  var toggle = document.getElementById("mg-toggle");
  var openCountPill = document.getElementById("mg-open-count");
  if (toggle) toggle.addEventListener("click", toggleSidebar);

  // ── load + render ──────────────────────────────────────────────────
  function load() {
    fetch(apiDoc + "?status=all", { headers: { Accept: "application/json" } })
      .then(function (r) {
        return r.ok ? r.json() : { threads: [] };
      })
      .then(function (data) {
        threads = (data && data.threads) || [];
        render();
      })
      .catch(function () {
        threads = [];
        render();
      });
  }

  function render() {
    clearHighlights();
    threads.forEach(function (t) {
      if (!t.orphaned) paintHighlight(t);
    });
    layoutGutter();
    renderSidebar();
    updateCounts();
  }

  // ── highlights ─────────────────────────────────────────────────────
  function clearHighlights() {
    var marks = article.querySelectorAll("mark.mg-hl");
    marks.forEach(function (m) {
      var parent = m.parentNode;
      while (m.firstChild) parent.insertBefore(m.firstChild, m);
      parent.removeChild(m);
      parent.normalize();
    });
    gutter.innerHTML = "";
  }

  // collapse mirrors the server's anchor.Normalize whitespace handling (collapse
  // runs of whitespace to a single space, trim) and records, for each normalized
  // code point, its raw UTF-16 offset in `raw`. That lets us match in the same
  // normalized/code-point space the Go cascade uses, then map back to the raw DOM
  // offsets a Range needs. (NFC is assumed already applied, as in real docs.)
  function collapse(raw) {
    var cps = Array.from(raw);
    var off = 0,
      starts = new Array(cps.length);
    for (var i = 0; i < cps.length; i++) {
      starts[i] = off;
      off += cps[i].length;
    }
    var out = [],
      n2r = [],
      prevSpace = true; // suppress leading whitespace
    for (var j = 0; j < cps.length; j++) {
      if (/\s/.test(cps[j])) {
        if (!prevSpace) {
          out.push(" ");
          n2r.push(starts[j]);
          prevSpace = true;
        }
      } else {
        out.push(cps[j]);
        n2r.push(starts[j]);
        prevSpace = false;
      }
    }
    while (out.length && out[out.length - 1] === " ") {
      out.pop();
      n2r.pop();
    }
    return { cps: out, n2r: n2r, rawLen: off };
  }

  // rawToNorm maps a raw UTF-16 offset to a normalized code-point index.
  function rawToNorm(c, rawOff) {
    for (var i = 0; i < c.n2r.length; i++) {
      if (c.n2r[i] >= rawOff) return i;
    }
    return c.cps.length;
  }

  // nearestMatch finds the start index of needle (code-point array) within hay
  // nearest to hint, or -1. Disambiguates repeated quotes by the stored position.
  function nearestMatch(hay, needle, hint) {
    if (!needle.length) return -1;
    var best = -1,
      bestDist = Infinity;
    for (var i = 0; i + needle.length <= hay.length; i++) {
      var ok = true;
      for (var k = 0; k < needle.length; k++) {
        if (hay[i + k] !== needle[k]) {
          ok = false;
          break;
        }
      }
      if (ok) {
        var d = Math.abs(i - hint);
        if (d < bestDist) {
          bestDist = d;
          best = i;
        }
      }
    }
    return best;
  }

  // raw offset of a quote inside a block: locate (normalized) then map to UTF-16.
  function quoteRawRange(block, quoteText, hint) {
    var c = collapse(block.textContent);
    var qc = Array.from(quoteText);
    var ns = nearestMatch(c.cps, qc, hint || 0);
    if (ns < 0) return null;
    var ne = ns + qc.length;
    return { start: c.n2r[ns], end: ne < c.n2r.length ? c.n2r[ne] : c.rawLen };
  }

  function paintHighlight(t) {
    var a = t.anchor;
    var startBlock = document.getElementById(a.block_id);
    if (!startBlock) return; // server flags true orphans; this is a soft miss

    if (!a.end_block_id || a.end_block_id === a.block_id) {
      var rng = quoteRawRange(startBlock, a.quote_exact, a.char_start);
      if (!rng) return;
      var sp = locateTextPos(startBlock, rng.start);
      var ep = locateTextPos(startBlock, rng.end);
      if (sp && ep) wrapDomRange(sp.node, sp.offset, ep.node, ep.offset, t);
      return;
    }

    // multi-block: head in the start block, tail in the end block.
    var endBlock = document.getElementById(a.end_block_id);
    if (!endBlock) return;
    var hr = quoteRawRange(startBlock, a.quote_exact, a.char_start);
    var tr = quoteRawRange(endBlock, a.quote_tail, 0);
    if (!hr || !tr) return;
    var s = locateTextPos(startBlock, hr.start);
    var e = locateTextPos(endBlock, tr.end);
    if (s && e) wrapDomRange(s.node, s.offset, e.node, e.offset, t);
  }

  // locateTextPos maps a raw UTF-16 offset within a block to a {node, offset}.
  function locateTextPos(block, rawOffset) {
    var walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT, null);
    var pos = 0,
      last = null,
      node;
    while ((node = walker.nextNode())) {
      var len = node.nodeValue.length;
      if (rawOffset <= pos + len) return { node: node, offset: rawOffset - pos };
      pos += len;
      last = node;
    }
    return last ? { node: last, offset: last.nodeValue.length } : null;
  }

  function makeMark(t) {
    var mark = el("mark", {
      class: "mg-hl" + (t.status === "resolved" ? " resolved" : ""),
      "data-tid": String(t.id),
      role: "button",
      tabindex: "0",
      "aria-label": (t.status === "resolved" ? "Resolved comment: " : "Comment: ") + t.anchor.quote_exact,
      "aria-details": "mg-thread-" + t.id, // W3C ARIA Annotations: link mark -> thread
    });
    mark.addEventListener("click", function () { activate(t.id, true); });
    mark.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" || ev.key === " ") {
        ev.preventDefault();
        activate(t.id, true);
      }
    });
    return mark;
  }

  // wrapDomRange wraps every text-node slice between two DOM points in a <mark>,
  // even across block boundaries (multi-block selections). Wrapping a single text
  // node never crosses an element boundary, so surroundContents is safe.
  function wrapDomRange(startNode, startOff, endNode, endOff, t) {
    var range = document.createRange();
    try {
      range.setStart(startNode, startOff);
      range.setEnd(endNode, endOff);
    } catch (e) {
      return;
    }
    if (range.collapsed) return;
    var root = range.commonAncestorContainer;
    if (root.nodeType === 3) root = root.parentNode;
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, null);
    var nodes = [],
      node;
    while ((node = walker.nextNode())) {
      if (range.intersectsNode(node)) nodes.push(node);
    }
    for (var i = nodes.length - 1; i >= 0; i--) {
      var n = nodes[i];
      // skip pure-whitespace structural nodes between blocks (e.g. the newline
      // between two <li>s) so we don't wrap stray slivers
      if (n !== startNode && n !== endNode && !n.nodeValue.trim()) continue;
      var s = n === startNode ? startOff : 0;
      var e = n === endNode ? endOff : n.nodeValue.length;
      if (e <= s) continue;
      var r = document.createRange();
      r.setStart(n, s);
      r.setEnd(n, e);
      try {
        r.surroundContents(makeMark(t));
      } catch (err) {
        /* portion crosses an element boundary within one text node — skip */
      }
    }
  }

  // ── gutter markers ─────────────────────────────────────────────────
  function layoutGutter() {
    gutter.innerHTML = "";
    var arect = article.getBoundingClientRect();
    // group threads by the vertical line of their first highlight
    var rows = [];
    threads.forEach(function (t) {
      if (t.orphaned) return;
      var m = article.querySelector('mark.mg-hl[data-tid="' + t.id + '"]');
      if (!m) return;
      var top = m.getBoundingClientRect().top - arect.top;
      var row = null;
      for (var i = 0; i < rows.length; i++) {
        if (Math.abs(rows[i].top - top) < 16) {
          row = rows[i];
          break;
        }
      }
      if (!row) {
        row = { top: top, items: [] };
        rows.push(row);
      }
      row.items.push(t);
    });

    rows.forEach(function (row) {
      var first = row.items[0];
      var allResolved = row.items.every(function (t) {
        return t.status === "resolved";
      });
      var multi = row.items.length > 1;
      var label = multi ? row.items.length + " comments" : "1 comment";
      var btn = el("button", {
        class: "mg-marker" + (allResolved ? " resolved" : "") + (multi ? " multi" : ""),
        type: "button",
        "data-tid": String(first.id),
        title: label,
        "aria-label": label,
        text: multi ? String(row.items.length) : "", // a margin annotation dot; count when stacked
        onclick: function () {
          activate(first.id, true);
        },
      });
      btn.style.top = row.top + "px";
      gutter.appendChild(btn);
    });
  }

  // ── sidebar ────────────────────────────────────────────────────────
  function filtered() {
    return threads.filter(function (t) {
      if (filter === "orphaned") return t.orphaned;
      if (filter === "resolved") return t.status === "resolved" && !t.orphaned;
      return t.status === "open" && !t.orphaned;
    });
  }

  function renderSidebar() {
    list.innerHTML = "";
    var items = filtered();
    if (!items.length) {
      list.appendChild(
        el("p", {
          class: "empty",
          text:
            filter === "open"
              ? "No open comments. Select text in the doc to add one."
              : "Nothing here.",
        })
      );
      return;
    }
    items.forEach(function (t) {
      list.appendChild(card(t));
    });
  }

  function card(t) {
    var c = el("div", {
      class: "mg-card" + (t.orphaned ? " orphan" : "") + (t.id === activeTid ? " active" : ""),
      "data-tid": String(t.id),
      id: "mg-thread-" + t.id,
      role: "group",
      "aria-label": "Comment thread on: " + t.anchor.quote_exact,
    });

    var qText = t.anchor.quote_exact;
    if (t.anchor.end_block_id && t.anchor.end_block_id !== t.anchor.block_id && t.anchor.quote_tail) {
      qText = t.anchor.quote_exact + " … " + t.anchor.quote_tail; // multi-block: head … tail
    }
    var quote = el("div", { class: "mg-quote", text: qText });
    quote.addEventListener("click", function () {
      activate(t.id, true);
    });
    c.appendChild(quote);

    var msgs = el("div", { class: "mg-msgs" });
    (t.comments || []).forEach(function (m) {
      var meta = el(
        "div",
        { class: "mg-meta" },
        el("span", { class: "mg-author", text: authorLabel(m.author) }),
        m.author === "ai" ? el("span", { class: "mg-ai", text: "ai" }) : null,
        el("span", { text: rel(m.created_at) })
      );
      msgs.appendChild(el("div", { class: "mg-msg" }, meta, el("div", { class: "mg-body", text: m.body })));
    });
    c.appendChild(msgs);

    // reply box
    var input = el("textarea", { class: "mg-input", rows: "1", placeholder: "Reply…", "aria-label": "Reply" });
    var send = el("button", {
      class: "mg-btn primary",
      type: "button",
      text: "Reply",
      onclick: function () {
        var body = input.value.trim();
        if (body) reply(t.id, body);
      },
    });
    input.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") send.click();
    });
    c.appendChild(el("div", { class: "mg-reply" }, input, el("div", { class: "mg-card-foot" }, statusChip(t), spacer(), actions(t), send)));
    return c;
  }

  function spacer() {
    return el("span", { class: "spacer" });
  }

  function statusChip(t) {
    if (t.orphaned) return el("span", { class: "badge warn", text: "orphaned" });
    if (t.status === "resolved") return el("span", { class: "badge good", text: "resolved" });
    return el("span", { class: "badge", text: "open" });
  }

  function actions(t) {
    if (t.status === "resolved") {
      return el("button", { class: "mg-btn", type: "button", text: "Reopen", onclick: function () { setStatus(t.id, "open"); } });
    }
    return el("button", { class: "mg-btn", type: "button", text: "Resolve", onclick: function () { setStatus(t.id, "resolved"); } });
  }

  function updateCounts() {
    var open = 0, resolved = 0, orph = 0;
    threads.forEach(function (t) {
      if (t.orphaned) orph++;
      else if (t.status === "resolved") resolved++;
      else open++;
    });
    tabs.open._chip.textContent = String(open);
    tabs.resolved._chip.textContent = String(resolved);
    tabs.orphaned._chip.textContent = String(orph);
    tabs.orphaned.style.display = orph > 0 ? "" : "none";
    if (openCountPill) {
      openCountPill.textContent = String(open);
      openCountPill.hidden = open === 0;
    }
    if (filter === "orphaned" && orph === 0) setFilter("open");
    else syncTabs();
  }

  function syncTabs() {
    Object.keys(tabs).forEach(function (k) {
      tabs[k].setAttribute("aria-selected", k === filter ? "true" : "false");
    });
  }

  function setFilter(f) {
    filter = f;
    syncTabs();
    renderSidebar();
  }

  // ── activate / navigate ────────────────────────────────────────────
  function activate(tid, openIt) {
    activeTid = tid;
    var t = threadById(tid);
    if (t) {
      if (t.orphaned) filter = "orphaned";
      else if (t.status === "resolved") filter = "resolved";
      else filter = "open";
      syncTabs();
    }
    if (openIt) openSidebar();
    renderSidebar();
    article.querySelectorAll("mark.mg-hl.active").forEach(function (m) { m.classList.remove("active"); });
    var marks = article.querySelectorAll('mark.mg-hl[data-tid="' + tid + '"]');
    marks.forEach(function (m) { m.classList.add("active"); });
    if (marks.length) marks[0].scrollIntoView({ block: "center", behavior: "smooth" });
    var c = list.querySelector('.mg-card[data-tid="' + tid + '"]');
    if (c) c.scrollIntoView({ block: "nearest" });
  }

  function threadById(id) {
    for (var i = 0; i < threads.length; i++) if (threads[i].id === id) return threads[i];
    return null;
  }

  // ── sidebar open/close ─────────────────────────────────────────────
  function openSidebar() {
    sidebar.classList.add("open");
    document.body.classList.add("sidebar-open");
    if (window.innerWidth < 1100) scrim.classList.add("show");
    if (toggle) toggle.setAttribute("aria-expanded", "true");
  }
  function closeSidebar() {
    sidebar.classList.remove("open");
    document.body.classList.remove("sidebar-open");
    scrim.classList.remove("show");
    if (toggle) toggle.setAttribute("aria-expanded", "false");
  }
  function toggleSidebar() {
    if (sidebar.classList.contains("open")) closeSidebar();
    else openSidebar();
  }

  // ── select-to-comment ──────────────────────────────────────────────
  function blockOf(node) {
    var eln = node.nodeType === 3 ? node.parentElement : node;
    while (eln && eln !== article) {
      if (eln.id) return eln;
      eln = eln.parentElement;
    }
    return null;
  }

  function offsetInBlock(block, node, off) {
    var r = document.createRange();
    r.setStart(block, 0);
    r.setEnd(node, off);
    return r.toString().length;
  }

  function captureAnchor() {
    var sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;
    var range = sel.getRangeAt(0);
    if (!article.contains(range.startContainer) || !article.contains(range.endContainer)) return null;
    var startBlock = blockOf(range.startContainer);
    if (!startBlock) return null;
    var endBlock = blockOf(range.endContainer) || startBlock;

    // All offsets are computed in the normalized code-point space the server uses.
    var sc = collapse(startBlock.textContent);
    var nStart = rawToNorm(sc, offsetInBlock(startBlock, range.startContainer, range.startOffset));

    if (endBlock === startBlock) {
      var nEnd = rawToNorm(sc, offsetInBlock(startBlock, range.endContainer, range.endOffset));
      if (nEnd <= nStart) return null;
      var exact = sc.cps.slice(nStart, nEnd).join("");
      if (!exact.trim()) return null;
      return {
        block_id: startBlock.id,
        quote_exact: exact,
        quote_prefix: sc.cps.slice(Math.max(0, nStart - 32), nStart).join(""),
        quote_suffix: sc.cps.slice(nEnd, nEnd + 32).join(""),
        char_start: nStart,
        char_end: nEnd,
      };
    }

    // Multi-block: head = start-block portion (nStart→end); tail = end-block
    // portion (0→nEnd). Each endpoint re-resolves independently server-side.
    var ec = collapse(endBlock.textContent);
    var nEnd2 = rawToNorm(ec, offsetInBlock(endBlock, range.endContainer, range.endOffset));
    var head = sc.cps.slice(nStart).join("");
    var tail = ec.cps.slice(0, nEnd2).join("");
    if (head.trim() && tail.trim()) {
      return {
        block_id: startBlock.id,
        end_block_id: endBlock.id,
        quote_exact: head,
        quote_tail: tail,
        quote_prefix: sc.cps.slice(Math.max(0, nStart - 32), nStart).join(""),
        quote_suffix: ec.cps.slice(nEnd2, nEnd2 + 32).join(""),
        char_start: nStart,
        char_end: nEnd2,
      };
    }
    // Selection ended right on a block boundary — anchor the start-block head only.
    if (!head.trim()) return null;
    return {
      block_id: startBlock.id,
      quote_exact: head,
      quote_prefix: sc.cps.slice(Math.max(0, nStart - 32), nStart).join(""),
      quote_suffix: "",
      char_start: nStart,
      char_end: sc.cps.length,
    };
  }

  function showPill() {
    var sel = window.getSelection();
    if (!sel || sel.rangeCount === 0) return;
    var rect = sel.getRangeAt(0).getBoundingClientRect();
    if (!rect.width && !rect.height) return;
    pill.style.left = rect.left + rect.width / 2 + window.scrollX + "px";
    pill.style.top = rect.top + window.scrollY - 8 + "px";
    pill.classList.add("show");
  }
  function hidePill() {
    pill.classList.remove("show");
  }

  function openComposer() {
    if (!pending) return;
    closeComposer();
    lastFocus = document.activeElement;
    // Capture the pill's position BEFORE hiding it — a display:none element has a
    // zero rect, which previously pinned the composer to the top-left corner.
    var anchorRect = pill.getBoundingClientRect();
    if (!anchorRect.width && !anchorRect.height) {
      var sel0 = window.getSelection();
      if (sel0 && sel0.rangeCount) anchorRect = sel0.getRangeAt(0).getBoundingClientRect();
    }
    hidePill();
    var ta = el("textarea", { class: "mg-input", rows: "3", placeholder: "Add a comment…", "aria-label": "New comment" });
    var pop = el(
      "div",
      { class: "mg-pop show", id: "mg-composer", role: "dialog", "aria-label": "New comment" },
      el(
        "div",
        { class: "mg-reply" },
        el("div", { class: "mg-quote", text: pending.quote_exact }),
        ta,
        el(
          "div",
          { class: "mg-card-foot" },
          spacer(),
          el("button", { class: "mg-btn", type: "button", text: "Cancel", onclick: closeComposer }),
          el("button", {
            class: "mg-btn primary",
            type: "button",
            text: "Comment",
            onclick: function () {
              var body = ta.value.trim();
              if (body) createThread(pending, body);
            },
          })
        )
      )
    );
    document.body.appendChild(pop);
    var vw = document.documentElement.clientWidth;
    var maxLeft = window.scrollX + vw - pop.offsetWidth - 10;
    var left = anchorRect.left + window.scrollX - 12; // align near the selection start
    pop.style.left = Math.max(window.scrollX + 10, Math.min(left, maxLeft)) + "px";
    pop.style.top = anchorRect.bottom + window.scrollY + 8 + "px";
    ta.focus();
    ta.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        var body = ta.value.trim();
        if (body) createThread(pending, body);
      } else if (e.key === "Escape") {
        closeComposer();
      }
    });
    // trap Tab within the dialog
    pop.addEventListener("keydown", function (e) {
      if (e.key !== "Tab") return;
      var f = pop.querySelectorAll("textarea, button");
      if (!f.length) return;
      var first = f[0],
        last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    });
  }
  function closeComposer() {
    var p = document.getElementById("mg-composer");
    if (!p) return;
    p.remove();
    if (lastFocus && document.contains(lastFocus) && lastFocus !== pill) {
      lastFocus.focus();
    } else {
      article.focus();
    }
    lastFocus = null;
  }

  // ── API mutations ──────────────────────────────────────────────────
  function postJSON(url, body, method) {
    return fetch(url, {
      method: method || "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  }

  function toast(msg) {
    var n = el("div", { class: "mg-toast", role: "status", text: msg });
    document.body.appendChild(n);
    requestAnimationFrame(function () { n.classList.add("show"); });
    setTimeout(function () { n.remove(); }, 4000);
  }
  function composerError(msg) {
    var pop = document.getElementById("mg-composer");
    if (!pop) { toast(msg); return; }
    var box = pop.querySelector(".mg-reply");
    var e2 = box.querySelector(".mg-err");
    if (!e2) { e2 = el("div", { class: "mg-err" }); box.appendChild(e2); }
    e2.textContent = msg;
  }

  function createThread(anchor, body) {
    postJSON(apiDoc, { anchor: anchor, body: body, author: "human" })
      .then(function (r) {
        if (!r.ok) {
          // keep the composer + the user's text; never silently lose a comment
          composerError("Couldn’t save (server " + r.status + "). Your text is kept — try again.");
          return null;
        }
        closeComposer();
        hidePill();
        pending = null;
        return loadThen(function () { openSidebar(); setFilter("open"); });
      })
      .catch(function () {
        composerError("Couldn’t reach the server. Your text is kept — try again.");
      });
  }
  function reply(tid, body) {
    postJSON("/api/threads/" + tid + "/replies", { body: body, author: "human" })
      .then(function (r) {
        if (!r.ok) { toast("Reply failed (server " + r.status + ")."); return null; }
        return loadThen(function () { activeTid = tid; });
      })
      .catch(function () { toast("Reply failed — server unreachable."); });
  }
  function setStatus(tid, status) {
    postJSON("/api/threads/" + tid, { status: status, by: "human" }, "PATCH")
      .then(function (r) {
        if (!r.ok) { toast("Update failed (server " + r.status + ")."); return null; }
        return loadThen(function () { activeTid = tid; });
      })
      .catch(function () { toast("Update failed — server unreachable."); });
  }
  function loadThen(after) {
    return fetch(apiDoc + "?status=all")
      .then(function (r) { return r.json(); })
      .then(function (data) {
        threads = (data && data.threads) || [];
        if (after) after();
        render();
      });
  }

  // ── events ─────────────────────────────────────────────────────────
  document.addEventListener("mouseup", function (e) {
    if (pill.contains(e.target)) return;
    setTimeout(function () {
      var a = captureAnchor();
      if (a) {
        pending = a;
        showPill();
      } else {
        hidePill();
      }
    }, 0);
  });

  document.addEventListener("mousedown", function (e) {
    if (!pill.contains(e.target)) hidePill();
    var comp = document.getElementById("mg-composer");
    if (comp && !comp.contains(e.target) && !pill.contains(e.target)) closeComposer();
  });

  function navigate(delta) {
    var items = filtered();
    if (!items.length) return;
    var idx = -1;
    for (var i = 0; i < items.length; i++) {
      if (items[i].id === activeTid) {
        idx = i;
        break;
      }
    }
    idx = idx < 0 ? (delta > 0 ? 0 : items.length - 1) : Math.min(items.length - 1, Math.max(0, idx + delta));
    activate(items[idx].id, true);
  }

  function toggleActive() {
    var t = threadById(activeTid);
    if (!t || t.orphaned) return;
    setStatus(t.id, t.status === "resolved" ? "open" : "resolved");
  }

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") {
      if (document.getElementById("mg-composer")) {
        closeComposer();
        return;
      }
      hidePill();
      if (sidebar.classList.contains("open")) closeSidebar();
      return;
    }
    var tag = (e.target.tagName || "").toLowerCase();
    if (tag === "textarea" || tag === "input" || e.metaKey || e.ctrlKey || e.altKey) return;
    if (document.getElementById("mg-composer")) return; // modal open: no background shortcuts

    switch (e.key) {
      case "c": {
        var a = captureAnchor();
        if (a) {
          pending = a;
          showPill();
          openComposer();
        }
        break;
      }
      case "j":
        navigate(1);
        break;
      case "k":
        navigate(-1);
        break;
      case "r": {
        var ta = list.querySelector('.mg-card[data-tid="' + activeTid + '"] textarea');
        if (ta) {
          openSidebar();
          ta.focus();
          e.preventDefault();
        }
        break;
      }
      case "e":
        toggleActive();
        break;
      case "Enter":
        if (activeTid != null) activate(activeTid, true);
        break;
    }
  });

  var reflow;
  window.addEventListener("resize", function () {
    clearTimeout(reflow);
    reflow = setTimeout(layoutGutter, 120);
  });
  window.addEventListener("scroll", function () {
    if (pill.classList.contains("show")) hidePill();
  }, { passive: true });

  load();
})();
