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

  // nested slugs (e.g. "payments/refunds") keep their "/" separators — escape per segment
  var apiDoc = "/api/comments/" + slug.split("/").map(encodeURIComponent).join("/");
  var threads = [];
  var filter = "open";
  var activeTid = null;
  var pending = null; // captured anchor awaiting a composer submit
  var lastFocus = null; // element to restore focus to when the composer closes
  var sidebarOpener = null; // focus to restore when the (modal) sidebar closes
  var drafts = {}; // tid -> uncommitted reply text, survives sidebar rebuilds
  var sending = {}; // in-flight guard per action key (no duplicate submits)
  var NARROW = 1100; // below this the sidebar is a modal overlay with a scrim
  var mqReduce = window.matchMedia("(prefers-reduced-motion: reduce)");
  var mqCoarse = window.matchMedia("(hover: none) and (pointer: coarse)");
  function scrollBehavior() { return mqReduce.matches ? "auto" : "smooth"; }
  // shared focus-trap: on Tab/Shift+Tab at an edge of the focusable list f, wrap.
  function trapTab(e, f) {
    if (e.key !== "Tab" || !f.length) return;
    var first = f[0], last = f[f.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }
  // whitespace set identical to Go's unicode.IsSpace (used by anchor.Normalize via
  // strings.Fields) — deliberately NOT JS \s, which includes U+FEFF and omits U+0085.
  var MG_WS = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]/;

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

  function fmtAbs(iso) {
    var d = new Date(iso);
    return isNaN(d.getTime()) ? "" : d.toLocaleString();
  }
  // a <time> that shows a relative label, the absolute time on hover, and is
  // re-labelled in place every minute so "just now" doesn't go stale.
  function timeEl(iso) {
    var e = el("time", { class: "mg-time", "data-iso": iso, title: fmtAbs(iso), text: rel(iso) });
    e.setAttribute("datetime", iso);
    return e;
  }
  function tickTimes() {
    document.querySelectorAll("time.mg-time[data-iso]").forEach(function (t) {
      t.textContent = rel(t.getAttribute("data-iso"));
    });
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

  var list = el("div", { class: "mg-list", id: "mg-list", role: "tabpanel", tabindex: "0" });
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
        id: "mg-tab-" + t[0],
        "data-filter": t[0],
        "aria-selected": "false",
        "aria-controls": "mg-list",
        tabindex: "-1",
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
  // WAI-ARIA tabs: Arrow/Home/End move between tabs (skipping a hidden orphaned tab)
  tabsWrap.addEventListener("keydown", function (e) {
    if (["ArrowLeft", "ArrowRight", "Home", "End"].indexOf(e.key) < 0) return;
    e.preventDefault();
    var order = ["open", "resolved", "orphaned"].filter(function (k) { return tabs[k].style.display !== "none"; });
    var i = order.indexOf(filter);
    if (e.key === "Home") i = 0;
    else if (e.key === "End") i = order.length - 1;
    else if (e.key === "ArrowLeft") i = (i - 1 + order.length) % order.length;
    else i = (i + 1) % order.length;
    setFilter(order[i]);
    tabs[order[i]].focus();
  });

  var closeBtn = el("button", { class: "mg-sb-close", type: "button", "aria-label": "Close comments", html: "&times;", onclick: closeSidebar });
  var sidebar = el(
    "aside",
    { class: "mg-sidebar", id: "mg-sidebar", "aria-label": "Comments", role: "complementary" },
    el("div", { class: "mg-sb-head" }, closeBtn, tabsWrap),
    list
  );
  document.body.appendChild(sidebar);
  // trap Tab inside the sidebar when it's a modal overlay (narrow screens)
  sidebar.addEventListener("keydown", function (e) {
    if (!sidebarModal) return;
    trapTab(e, Array.prototype.filter.call(
      sidebar.querySelectorAll('button, a[href], textarea, input, [tabindex="0"]'),
      function (x) { return x.offsetParent !== null; }
    ));
  });

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
  var docNoteBtn = document.getElementById("mg-docnote");
  if (docNoteBtn) docNoteBtn.addEventListener("click", openDocComposer);
  var downloadBtn = document.getElementById("mg-download");
  if (downloadBtn) downloadBtn.addEventListener("click", toggleDownloadMenu);

  // ── load + render ──────────────────────────────────────────────────
  function load() {
    fetch(apiDoc + "?status=all", { headers: { Accept: "application/json" } })
      .then(function (r) {
        return r.ok ? r.json() : { threads: [] };
      })
      .then(function (data) {
        threads = (data && data.threads) || [];
        render();
        maybeDeepLink();
      })
      .catch(function () {
        threads = [];
        render();
      });
  }

  // open a comment named in the URL hash (#mg-thread-N) once, on first load
  var deepLinked = false;
  function maybeDeepLink() {
    if (deepLinked) return;
    deepLinked = true;
    var m = /^#mg-thread-(\d+)$/.exec(location.hash || "");
    if (!m) return;
    var id = parseInt(m[1], 10);
    if (threadById(id)) { activate(id, true); flashCard(id); }
  }
  function flashCard(id) {
    var c = list.querySelector('.mg-card[data-tid="' + id + '"]');
    if (c) { c.classList.remove("flash"); void c.offsetWidth; c.classList.add("flash"); }
  }

  // a document-level note has the reserved "doc" block id and no text anchor
  function isDocThread(t) { return t.anchor && t.anchor.block_id === "doc"; }

  function render() {
    clearHighlights();
    threads.forEach(function (t) {
      if (!t.orphaned && !isDocThread(t)) paintHighlight(t);
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
  // offsets a Range needs.
  //
  // Coordinate-space invariant: this MUST mirror anchor.Normalize (Go) or a stored
  // quote won't match in the browser. Two subtleties: (1) NFC — the server hashes
  // and matches on NFC text, so normalize here too (a no-op for the common,
  // already-NFC doc; defensive otherwise). (2) the whitespace set must equal Go's
  // unicode.IsSpace, NOT JS \s — \s wrongly includes U+FEFF (BOM) and omits U+0085
  // (NEL); MG_WS below matches Go exactly.
  function collapse(raw) {
    raw = raw.normalize("NFC");
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
      if (MG_WS.test(cps[j])) {
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

  // Only the PRIMARY fragment of a (possibly multi-node) highlight is a tab stop /
  // AT target; the rest are aria-hidden so one comment = one announcement.
  function makeMark(t, primary) {
    var tentative = !t.orphaned && t.anchor.confidence > 0 && t.anchor.confidence < 1;
    var mark = el("mark", { class: "mg-hl" + (t.status === "resolved" ? " resolved" : "") + (tentative ? " tentative" : ""), "data-tid": String(t.id) });
    mark.addEventListener("click", function () { activate(t.id, true); });
    if (primary) {
      mark.setAttribute("role", "button");
      mark.setAttribute("tabindex", "0");
      mark.setAttribute("aria-label", (t.status === "resolved" ? "Resolved comment: " : "Comment: ") + t.anchor.quote_exact);
      mark.setAttribute("aria-details", "mg-thread-" + t.id); // W3C ARIA Annotations: mark -> thread
      mark.addEventListener("keydown", function (ev) {
        if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); activate(t.id, true); }
      });
    } else {
      mark.setAttribute("aria-hidden", "true");
    }
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
    var frags = [],
      node;
    while ((node = walker.nextNode())) {
      if (!range.intersectsNode(node)) continue;
      // skip pure-whitespace structural nodes between blocks (e.g. the newline
      // between two <li>s) so we don't wrap stray slivers
      if (node !== startNode && node !== endNode && !node.nodeValue.trim()) continue;
      var s = node === startNode ? startOff : 0;
      var e = node === endNode ? endOff : node.nodeValue.length;
      if (e > s) frags.push({ node: node, s: s, e: e });
    }
    // wrap last→first so earlier offsets stay valid; the first fragment is primary
    for (var i = frags.length - 1; i >= 0; i--) {
      var r = document.createRange();
      r.setStart(frags[i].node, frags[i].s);
      r.setEnd(frags[i].node, frags[i].e);
      try {
        r.surroundContents(makeMark(t, i === 0));
      } catch (err) {
        /* portion crosses an element boundary within one text node — skip */
      }
    }
  }

  // ── gutter markers ─────────────────────────────────────────────────
  function layoutGutter() {
    gutter.innerHTML = "";
    var arect = article.getBoundingClientRect();
    // group threads by the vertical line of their mark — or, if a thread couldn't
    // be painted (soft miss), its anchor block — so every counted comment gets a dot
    var rows = [];
    threads.forEach(function (t) {
      if (t.orphaned || isDocThread(t)) return; // doc-level notes have no gutter marker
      var anchorEl = article.querySelector('mark.mg-hl[data-tid="' + t.id + '"]') || document.getElementById(t.anchor.block_id);
      if (!anchorEl) return;
      var top = anchorEl.getBoundingClientRect().top - arect.top;
      var row = null;
      for (var i = 0; i < rows.length; i++) {
        if (Math.abs(rows[i].top - top) < 16) { row = rows[i]; break; }
      }
      if (!row) { row = { top: top, items: [] }; rows.push(row); }
      row.items.push(t);
    });

    rows.forEach(function (row) {
      var multi = row.items.length > 1;
      var resolvedN = row.items.filter(function (t) { return t.status === "resolved"; }).length;
      var allResolved = resolvedN === row.items.length;
      var label = multi
        ? row.items.length + " comments" + (resolvedN ? " (" + resolvedN + " resolved)" : "")
        : (allResolved ? "1 resolved comment" : "1 comment");
      var cycle = 0; // stacked markers cycle through their threads on repeated clicks
      var btn = el("button", {
        class: "mg-marker" + (allResolved ? " resolved" : "") + (multi ? " multi" : ""),
        type: "button",
        tabindex: "-1", // decorative; keyboard/AT reach comments via the in-body marks
        "data-tid": String(row.items[0].id),
        title: label,
        "aria-label": label,
        text: multi ? String(row.items.length) : "",
        onclick: function () {
          var t = row.items[cycle % row.items.length];
          cycle++;
          activate(t.id, true);
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

  function emptyState() {
    var box = el("div", { class: "mg-empty" });
    if (filter === "open") {
      if (threads.length > 0) { // there are threads, just none open → a clean manuscript
        box.appendChild(el("div", { class: "mg-empty-mark", html: "❧" }));
        box.appendChild(el("p", { class: "mg-empty-head", text: "The manuscript is clean." }));
        box.appendChild(el("p", { class: "mg-empty-sub", text: "Every open note has been resolved." }));
      } else {
        box.appendChild(el("div", { class: "mg-empty-mark", html: PENCIL }));
        box.appendChild(el("p", { class: "mg-empty-head", text: "Nothing in the margin yet." }));
        box.appendChild(el("p", { class: "mg-empty-sub", text: "Select any text in the document to leave a note." }));
        box.appendChild(el("button", { class: "mg-empty-hint", type: "button", text: "Press ? for keyboard shortcuts", onclick: openHelp }));
      }
    } else if (filter === "resolved") {
      box.appendChild(el("p", { class: "mg-empty-sub", text: "No resolved comments yet." }));
    } else {
      box.appendChild(el("p", { class: "mg-empty-sub", text: "No orphaned comments — every note still has its place." }));
    }
    return box;
  }

  // ── keyboard-shortcut help overlay (?) ─────────────────────────────
  var helpEl = null;
  function openHelp() {
    if (helpEl) return;
    var SHORTCUTS = [
      ["c", "Comment on the selection (or the whole doc)"],
      ["j / k", "Next / previous comment"],
      ["Enter", "Jump to the active comment’s anchor"],
      ["r", "Reply to the active comment"],
      ["e", "Resolve / reopen the active comment"],
      ["?", "Show this help"],
      ["Esc", "Close the composer, sidebar, or this help"],
    ];
    var rows = SHORTCUTS.map(function (s) {
      return el("div", { class: "mg-help-row" }, el("kbd", { text: s[0] }), el("span", { text: s[1] }));
    });
    var close = el("button", { class: "mg-sb-close", type: "button", "aria-label": "Close help", html: "&times;", onclick: closeHelp });
    var panel = el("div", { class: "mg-help", role: "dialog", "aria-modal": "true", "aria-label": "Keyboard shortcuts" },
      close,
      el("h2", { class: "mg-help-title", text: "Keyboard shortcuts" }));
    rows.forEach(function (r) { panel.appendChild(r); });
    helpEl = el("div", { class: "mg-help-scrim" }, panel);
    helpEl.addEventListener("click", function (e) { if (e.target === helpEl) closeHelp(); });
    helpEl.addEventListener("keydown", function (e) {
      if (e.key === "Escape") { e.stopPropagation(); closeHelp(); }
      else if (e.key === "Tab") { e.preventDefault(); close.focus(); } // single control: trap on it
    });
    document.body.appendChild(helpEl);
    requestAnimationFrame(function () { helpEl.classList.add("show"); });
    helpOpener = document.activeElement;
    close.focus();
  }
  var helpOpener = null;
  function closeHelp() {
    if (!helpEl) return;
    var node = helpEl; helpEl = null;
    node.classList.remove("show");
    setTimeout(function () { node.remove(); }, 180);
    if (helpOpener && document.contains(helpOpener) && helpOpener.focus) helpOpener.focus();
    helpOpener = null;
  }

  function renderSidebar() {
    var savedScroll = sidebar.scrollTop; // a full rebuild otherwise jumps to top
    list.innerHTML = "";
    var items = filtered();
    if (!items.length) {
      list.appendChild(emptyState());
      return;
    }
    // pin document-level notes in their own "On the document" group at the top
    var docs = [], anchored = [];
    items.forEach(function (t) { (isDocThread(t) ? docs : anchored).push(t); });
    if (docs.length) {
      list.appendChild(el("div", { class: "mg-group", text: "On the document" }));
      docs.forEach(function (t) { list.appendChild(card(t)); });
      if (anchored.length) list.appendChild(el("div", { class: "mg-group", text: "Anchored" }));
    }
    anchored.forEach(function (t) { list.appendChild(card(t)); });
    sidebar.scrollTop = savedScroll;
  }

  function card(t) {
    var docLevel = isDocThread(t);
    var tentative = !t.orphaned && !docLevel && t.anchor.confidence > 0 && t.anchor.confidence < 1;
    var c = el("div", {
      class: "mg-card" + (t.orphaned ? " orphan" : "") + (docLevel ? " doc" : "") + (tentative ? " tentative" : "") + (t.id === activeTid ? " active" : ""),
      "data-tid": String(t.id),
      id: "mg-thread-" + t.id,
      role: "comment",
      "aria-label": docLevel ? "Comment on the whole document" : "Comment thread on: " + t.anchor.quote_exact,
    });

    if (docLevel) {
      // no quote snippet, no highlight/marker — just a label
      c.appendChild(el("div", { class: "mg-doclabel", text: "On this document" }));
    } else {
      // hovering a card "peeks" its in-body highlight + gutter marker (and vice-versa)
      c.addEventListener("mouseenter", function () { peek(t.id, true); });
      c.addEventListener("mouseleave", function () { peek(t.id, false); });

      var qText = t.anchor.quote_exact;
      if (t.anchor.end_block_id && t.anchor.end_block_id !== t.anchor.block_id && t.anchor.quote_tail) {
        qText = t.anchor.quote_exact + " … " + t.anchor.quote_tail; // multi-block: head … tail
      }
      var quote = el("div", { class: "mg-quote", role: "button", tabindex: "0", text: qText, title: "Jump to this passage" });
      function jump() { activate(t.id, true, true); }
      quote.addEventListener("click", jump);
      quote.addEventListener("keydown", function (e) { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); jump(); } });
      c.appendChild(quote);
    }

    if (tentative) {
      c.appendChild(el("div", { class: "mg-tentative", text: "The text moved — this anchor was relocated. Check it still fits." }));
    }

    var msgs = el("div", { class: "mg-msgs" });
    (t.comments || []).forEach(function (m) {
      msgs.appendChild(messageEl(t, m));
    });
    c.appendChild(msgs);

    if (t.status === "resolved" && t.resolved_at) {
      c.appendChild(el("div", { class: "mg-resolved-note" },
        "Resolved" + (t.resolved_by ? " by " + authorLabel(t.resolved_by) : "") + " · ", timeEl(t.resolved_at)));
    }

    // reply box: auto-grows, Enter (Shift+Enter = newline) sends; button disabled when empty
    var input = el("textarea", { class: "mg-input mg-grow", rows: "1", placeholder: "Reply… (Enter to send)", "aria-label": docLevel ? "Reply to the document note" : "Reply to comment on: " + t.anchor.quote_exact });
    input.value = drafts[t.id] || "";
    var send = el("button", {
      class: "mg-btn primary",
      type: "button",
      text: "Reply",
      onclick: function () {
        var body = input.value.trim();
        if (body) reply(t.id, body);
      },
    });
    // the reply send button only appears once you're typing, so the resting footer
    // stays uncluttered (Enter still sends; placeholder says so)
    function syncSend() { send.hidden = input.value.trim() === ""; }
    function grow() { input.style.height = "auto"; input.style.height = Math.min(input.scrollHeight, 200) + "px"; }
    input.addEventListener("input", function () { drafts[t.id] = input.value; syncSend(); grow(); });
    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send.click(); }
    });
    syncSend();

    // Footer carries ONE prominent action (Resolve/Reopen); secondary + destructive
    // actions live in a quiet overflow menu so the row never crowds. The menu panel
    // is appended in-flow at the card's bottom (the card has overflow:hidden, so an
    // absolute popover would clip) — it expands the card like a small drawer.
    var more = moreMenu(t);
    c.appendChild(el("div", { class: "mg-reply" }, input,
      el("div", { class: "mg-card-foot" }, statusChip(t), spacer(), send, actions(t), more.btn)));
    c.appendChild(more.menu);
    return c;
  }

  // messageEl renders one message with a hover "Edit" affordance → inline editor.
  function messageEl(t, m) {
    var bodyEl = el("div", { class: "mg-body", text: m.body });
    var editBtn = el("button", { class: "mg-btn mg-edit", type: "button", text: "Edit", title: "Edit this message", onclick: function () { startEdit(); } });
    var meta = el(
      "div",
      { class: "mg-meta" },
      el("span", { class: "mg-author author-" + (m.author === "ai" ? "ai" : "human"), text: authorLabel(m.author) }),
      m.author === "ai" ? el("span", { class: "mg-ai", text: "ai" }) : null,
      timeEl(m.created_at),
      m.edited_at ? el("span", { class: "mg-edited", text: "edited", title: "This message was edited" }) : null,
      spacer(),
      editBtn
    );
    var wrap = el("div", { class: "mg-msg" }, meta, bodyEl);

    function startEdit() {
      var ta = el("textarea", { class: "mg-input mg-grow", rows: "1", "aria-label": "Edit message" });
      ta.value = m.body;
      function grow() { ta.style.height = "auto"; ta.style.height = Math.min(ta.scrollHeight, 240) + "px"; }
      function save() { var b = ta.value.trim(); if (b && b !== m.body) editComment(t.id, m.id, b); else cancel(); }
      function cancel() { editor.remove(); bodyEl.hidden = false; editBtn.hidden = false; editBtn.focus(); }
      var saveBtn = el("button", { class: "mg-btn primary", type: "button", text: "Save", onclick: save });
      var editor = el("div", { class: "mg-editor" }, ta, el("div", { class: "mg-editor-foot" }, spacer(),
        el("button", { class: "mg-btn ghost", type: "button", text: "Cancel", onclick: cancel }), saveBtn));
      ta.addEventListener("keydown", function (e) {
        if ((e.metaKey || e.ctrlKey) && e.key === "Enter") { e.preventDefault(); save(); }
        else if (e.key === "Escape") { e.preventDefault(); cancel(); }
      });
      ta.addEventListener("input", grow);
      bodyEl.hidden = true;
      editBtn.hidden = true;
      wrap.appendChild(editor);
      ta.focus();
      grow();
    }
    return wrap;
  }

  // moreMenu is the per-thread overflow: Copy link + a two-step Delete confirm.
  // Returns { btn } for the footer and { menu } appended in-flow at the card bottom.
  function moreMenu(t) {
    var btn = el("button", {
      class: "mg-btn ghost mg-more", type: "button", text: "⋯", title: "More actions",
      "aria-label": "More actions", "aria-haspopup": "true", "aria-expanded": "false",
    });
    var menu = el("div", { class: "mg-menu", role: "menu" });

    function onDocClick(e) { if (e.target !== btn && !menu.contains(e.target)) close(); }
    function close() {
      menu.classList.remove("open");
      btn.setAttribute("aria-expanded", "false");
      document.removeEventListener("click", onDocClick, true);
      buildDefault();
    }
    function open() {
      buildDefault();
      menu.classList.add("open");
      btn.setAttribute("aria-expanded", "true");
      document.addEventListener("click", onDocClick, true);
      var first = menu.querySelector("button"); if (first) first.focus();
    }
    btn.addEventListener("click", function (e) { e.stopPropagation(); menu.classList.contains("open") ? close() : open(); });
    menu.addEventListener("keydown", function (e) { if (e.key === "Escape") { close(); btn.focus(); } });

    function item(label, danger, onClick) {
      return el("button", { class: "mg-menu-item" + (danger ? " danger" : ""), type: "button", role: "menuitem", text: label, onclick: onClick });
    }
    function buildDefault() {
      menu.textContent = "";
      menu.appendChild(item("Copy link", false, function () { copyThreadLink(t.id); close(); }));
      menu.appendChild(item("Delete comment…", true, buildConfirm));
    }
    function buildConfirm() {
      menu.textContent = "";
      menu.appendChild(el("div", { class: "mg-menu-note", text: "Delete permanently? This can’t be undone." }));
      var row = el("div", { class: "mg-menu-confirm" },
        el("button", { class: "mg-btn ghost", type: "button", text: "Cancel", onclick: function () { close(); btn.focus(); } }),
        el("button", { class: "mg-btn danger", type: "button", text: "Delete", onclick: function () { close(); deleteThread(t.id); } }));
      menu.appendChild(row);
      row.querySelector(".danger").focus();
    }
    buildDefault();
    return { btn: btn, menu: menu };
  }

  // peek: emphasise a thread's highlight + gutter marker on card/marker hover
  function peek(tid, on) {
    article.querySelectorAll('mark.mg-hl[data-tid="' + tid + '"]').forEach(function (m) { m.classList.toggle("peek", on); });
    var mk = gutter.querySelector('.mg-marker[data-tid="' + tid + '"]');
    if (mk) mk.classList.toggle("peek", on);
  }

  function copyThreadLink(tid) {
    var url = location.origin + location.pathname + "#mg-thread-" + tid;
    if (navigator.clipboard) {
      navigator.clipboard.writeText(url).then(function () { toast("Link to comment copied"); }, function () { toast("Couldn’t copy link", { error: true }); });
    } else {
      toast("Clipboard unavailable", { error: true });
    }
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
      var sel = k === filter;
      tabs[k].setAttribute("aria-selected", sel ? "true" : "false");
      tabs[k].tabIndex = sel ? 0 : -1; // roving tabindex
    });
    list.setAttribute("aria-labelledby", "mg-tab-" + filter);
  }

  function setFilter(f) {
    filter = f;
    syncTabs();
    renderSidebar();
  }

  // ── activate / navigate ────────────────────────────────────────────
  function targetFilter(t) {
    if (!t) return filter;
    if (t.orphaned) return "orphaned";
    return t.status === "resolved" ? "resolved" : "open";
  }

  function activate(tid, openIt, focusIt) {
    activeTid = tid;
    var t = threadById(tid);
    if (t) {
      var tf = targetFilter(t);
      if (filter !== tf) { filter = tf; syncTabs(); } // switch tabs only if the target is hidden
    }
    if (openIt) openSidebar();
    renderSidebar();
    article.querySelectorAll("mark.mg-hl.active").forEach(function (m) { m.classList.remove("active"); });
    var marks = article.querySelectorAll('mark.mg-hl[data-tid="' + tid + '"]');
    marks.forEach(function (m) { m.classList.add("active"); });
    if (marks.length) {
      if (focusIt) marks[0].focus({ preventScroll: true }); // ring + AT focus for keyboard nav
      marks[0].scrollIntoView({ block: "center", behavior: scrollBehavior() });
    } else if (t) {
      // soft miss (no painted mark) — at least scroll the anchor block into view
      var blk = document.getElementById(t.anchor.block_id);
      if (blk) blk.scrollIntoView({ block: "center", behavior: scrollBehavior() });
    }
    var c = list.querySelector('.mg-card[data-tid="' + tid + '"]');
    if (c) c.scrollIntoView({ block: "nearest" });
  }

  function threadById(id) {
    for (var i = 0; i < threads.length; i++) if (threads[i].id === id) return threads[i];
    return null;
  }

  // ── sidebar open/close ─────────────────────────────────────────────
  var header = document.querySelector(".site-header");
  var toc = document.querySelector(".toc");
  var sidebarModal = false;

  // On narrow viewports the sidebar is a modal overlay: trap focus, inert the
  // background, lock scroll. On wide viewports it's a companion panel.
  function enterModal() {
    if (sidebarModal) return;
    sidebarModal = true;
    sidebar.setAttribute("role", "dialog");
    sidebar.setAttribute("aria-modal", "true");
    [article, header, toc].forEach(function (e) { if (e) e.setAttribute("inert", ""); });
    document.body.classList.add("sidebar-modal");
  }
  function exitModal() {
    if (!sidebarModal) return;
    sidebarModal = false;
    sidebar.setAttribute("role", "complementary");
    sidebar.removeAttribute("aria-modal");
    [article, header, toc].forEach(function (e) { if (e) e.removeAttribute("inert"); });
    document.body.classList.remove("sidebar-modal");
  }
  function syncSidebarMode() {
    if (!sidebar.classList.contains("open")) return;
    var narrow = window.innerWidth < NARROW;
    scrim.classList.toggle("show", narrow);
    if (narrow) enterModal();
    else exitModal();
  }
  function openSidebar() {
    var wasOpen = sidebar.classList.contains("open");
    if (!wasOpen) sidebarOpener = document.activeElement;
    sidebar.classList.add("open");
    document.body.classList.add("sidebar-open");
    if (toggle) toggle.setAttribute("aria-expanded", "true");
    syncSidebarMode();
    if (!wasOpen && sidebarModal) {
      var t = sidebar.querySelector('.mg-tab[aria-selected="true"]') || sidebar.querySelector(".mg-tab");
      if (t) t.focus();
    }
  }
  function closeSidebar() {
    if (!sidebar.classList.contains("open")) return;
    sidebar.classList.remove("open");
    document.body.classList.remove("sidebar-open");
    scrim.classList.remove("show");
    exitModal();
    if (toggle) toggle.setAttribute("aria-expanded", "false");
    if (sidebarOpener && document.contains(sidebarOpener) && sidebarOpener.focus) sidebarOpener.focus();
    else if (toggle) toggle.focus();
    sidebarOpener = null;
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
    var range = sel.getRangeAt(0);
    var rects = range.getClientRects();
    var rect = rects.length ? rects[0] : range.getBoundingClientRect(); // first line, not the union box
    if (!rect.width && !rect.height) return;
    pill.classList.add("show"); // show first so offsetHeight is measurable
    var ph = pill.offsetHeight || 32;
    var headerH = 60; // var(--header-h)
    pill.style.left = rect.left + rect.width / 2 + window.scrollX + "px";
    if (rect.top < ph + 8 + headerH) {
      // no room above (or it'd hide under the sticky header) → flip below the line
      pill.classList.add("below");
      pill.style.top = rect.bottom + window.scrollY + 8 + "px";
    } else {
      pill.classList.remove("below");
      pill.style.top = rect.top + window.scrollY - 8 + "px";
    }
  }
  function hidePill() {
    pill.classList.remove("show");
    pill.classList.remove("below");
  }

  // ── download menu (masthead) ───────────────────────────────────────
  function slugPath() { return slug.split("/").map(encodeURIComponent).join("/"); }
  var dlMenu = null;
  function closeDownloadMenu() {
    if (!dlMenu) return;
    dlMenu.remove();
    dlMenu = null;
    var b = document.getElementById("mg-download");
    if (b) b.setAttribute("aria-expanded", "false");
    document.removeEventListener("click", onDlDocClick, true);
    document.removeEventListener("keydown", onDlKey, true);
  }
  function onDlDocClick(e) {
    var b = document.getElementById("mg-download");
    if (dlMenu && !dlMenu.contains(e.target) && e.target !== b && !b.contains(e.target)) closeDownloadMenu();
  }
  function onDlKey(e) { if (e.key === "Escape") { closeDownloadMenu(); var b = document.getElementById("mg-download"); if (b) b.focus(); } }
  function toggleDownloadMenu() {
    if (dlMenu) { closeDownloadMenu(); return; }
    var btn = document.getElementById("mg-download");
    var inc = el("input", { type: "checkbox", id: "mg-dl-comments" });
    var label = el("label", { class: "mg-dl-toggle", for: "mg-dl-comments" }, inc, el("span", { text: "Include review comments" }));
    function c() { return inc.checked ? "1" : "0"; }
    function fileItem(name, fmt) {
      return el("button", { class: "mg-menu-item", type: "button", role: "menuitem", text: name,
        onclick: function () { downloadDoc(fmt, c()); closeDownloadMenu(); } });
    }
    dlMenu = el("div", { class: "mg-dlmenu", role: "menu" },
      label, el("div", { class: "mg-dl-sep" }),
      fileItem("Markdown", "md"), fileItem("HTML", "html"),
      el("button", { class: "mg-menu-item", type: "button", role: "menuitem", text: "PDF (print)",
        onclick: function () { var withC = inc.checked; closeDownloadMenu(); toPDF(withC); } }));
    document.body.appendChild(dlMenu);
    var r = btn.getBoundingClientRect();
    dlMenu.style.top = r.bottom + 6 + "px";
    dlMenu.style.right = Math.max(8, window.innerWidth - r.right) + "px";
    btn.setAttribute("aria-expanded", "true");
    setTimeout(function () { document.addEventListener("click", onDlDocClick, true); }, 0);
    document.addEventListener("keydown", onDlKey, true);
  }
  function downloadDoc(format, comments) {
    var a = el("a", { href: "/download/" + slugPath() + "?format=" + format + "&comments=" + comments });
    a.setAttribute("download", "");
    document.body.appendChild(a);
    a.click();
    a.remove();
  }
  function toPDF(withComments) {
    // clean doc → print the live page (the @media print sheet hides chrome/sidebar).
    // with comments → open the self-contained HTML export (comments inline) and print it.
    if (!withComments) { window.print(); return; }
    var w = window.open("/download/" + slugPath() + "?format=html&comments=1&inline=1", "_blank");
    if (w) w.addEventListener("load", function () { try { w.focus(); w.print(); } catch (e) {} });
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
      { class: "mg-pop show", id: "mg-composer", role: "dialog", "aria-modal": "true", "aria-label": "New comment" },
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
    var vh = window.innerHeight;
    var maxLeft = window.scrollX + vw - pop.offsetWidth - 10;
    var left = anchorRect.left + window.scrollX - 12; // align near the selection start
    pop.style.left = Math.max(window.scrollX + 10, Math.min(left, maxLeft)) + "px";
    // vertical: below the selection by default, but flip above / clamp so the whole
    // popover (incl. the Comment button) stays on screen even for low selections
    var h = pop.offsetHeight;
    var top;
    if (anchorRect.bottom + 8 + h <= vh) top = anchorRect.bottom + window.scrollY + 8;
    else if (anchorRect.top - 8 - h >= 0) top = anchorRect.top + window.scrollY - h - 8;
    else top = Math.max(window.scrollY + 8, window.scrollY + vh - h - 10);
    pop.style.top = top + "px";
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
      trapTab(e, pop.querySelectorAll("textarea, button"));
    });
  }
  // A centered composer for a document-level note (no text anchor). Opened from
  // the masthead "＋ Note" button or by pressing c with nothing selected.
  function openDocComposer() {
    closeComposer();
    hidePill();
    lastFocus = document.activeElement;
    var ta = el("textarea", { class: "mg-input", rows: "3", placeholder: "A note on the whole document…", "aria-label": "Note on this document" });
    function submit() { var body = ta.value.trim(); if (body) createDocNote(body); }
    var pop = el(
      "div",
      { class: "mg-pop center show", id: "mg-composer", role: "dialog", "aria-modal": "true", "aria-label": "Note on this document" },
      el(
        "div",
        { class: "mg-reply" },
        el("div", { class: "mg-doclabel", text: "On this document" }),
        ta,
        el(
          "div",
          { class: "mg-card-foot" },
          spacer(),
          el("button", { class: "mg-btn", type: "button", text: "Cancel", onclick: closeComposer }),
          el("button", { class: "mg-btn primary", type: "button", text: "Add note", onclick: submit })
        )
      )
    );
    document.body.appendChild(pop);
    ta.focus();
    ta.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") submit();
      else if (e.key === "Escape") closeComposer();
    });
    pop.addEventListener("keydown", function (e) {
      trapTab(e, pop.querySelectorAll("textarea, button"));
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

  function toastHost() {
    var h = document.getElementById("mg-toasts");
    if (!h) { h = el("div", { id: "mg-toasts", "aria-live": "polite" }); document.body.appendChild(h); }
    return h;
  }
  // toast(msg, {error, action:{label,onClick}, duration}) — a column host stacks them.
  // Errors announce assertively; an optional action button (e.g. Undo) keeps it longer.
  function toast(msg, opts) {
    opts = opts || {};
    var n = el("div", { class: "mg-toast" + (opts.error ? " error" : "") });
    if (opts.error) n.setAttribute("role", "alert");
    n.appendChild(el("span", { class: "mg-toast-msg", text: msg }));
    var timer;
    function close() { clearTimeout(timer); n.classList.remove("show"); setTimeout(function () { n.remove(); }, 200); }
    if (opts.action) {
      var b = el("button", { class: "mg-toast-act", type: "button", text: opts.action.label });
      b.addEventListener("click", function () { close(); opts.action.onClick(); });
      n.appendChild(b);
    }
    toastHost().appendChild(n);
    requestAnimationFrame(function () { n.classList.add("show"); });
    timer = setTimeout(close, opts.duration || (opts.error ? 6000 : opts.action ? 6000 : 3500));
    return { close: close };
  }
  window.__mgToast = function (m) { toast(m); }; // used by the shell chrome (e.g. copy-link)
  function composerError(msg) {
    var pop = document.getElementById("mg-composer");
    if (!pop) { toast(msg); return; }
    var box = pop.querySelector(".mg-reply");
    var e2 = box.querySelector(".mg-err");
    if (!e2) {
      e2 = el("div", { class: "mg-err", role: "alert", "aria-live": "assertive" });
      box.insertBefore(e2, box.querySelector(".mg-card-foot"));
    }
    e2.textContent = msg;
    var ta = pop.querySelector("textarea");
    if (ta) ta.focus(); // put the user back in position to retry (their text is kept)
  }

  // per-action in-flight guards so a rapid double-click can't double-submit
  function busy(key) { if (sending[key]) return false; sending[key] = true; return true; }
  function done(key) { delete sending[key]; }

  function createThread(anchor, body) {
    if (!busy("create")) return;
    postJSON(apiDoc, { anchor: anchor, body: body, author: "human" })
      .then(function (r) {
        if (!r.ok) {
          composerError("Couldn’t save (server " + r.status + "). Your text is kept — try again.");
          return;
        }
        closeComposer();
        hidePill();
        pending = null;
        return r.json().then(function (created) {
          // select the new comment so it's emphasised and r/e target it
          return loadThen().then(function () {
            if (created && created.id) activate(created.id, true);
            else { setFilter("open"); openSidebar(); }
          });
        });
      })
      .catch(function () { composerError("Couldn’t reach the server. Your text is kept — try again."); })
      .then(function () { done("create"); });
  }
  function createDocNote(body) {
    if (!busy("create")) return;
    postJSON(apiDoc, { scope: "doc", body: body, author: "human" })
      .then(function (r) {
        if (!r.ok) { composerError("Couldn’t save (server " + r.status + "). Your text is kept — try again."); return; }
        closeComposer();
        return r.json().then(function (created) {
          return loadThen().then(function () {
            if (created && created.id) activate(created.id, true);
            else { setFilter("open"); openSidebar(); }
          });
        });
      })
      .catch(function () { composerError("Couldn’t reach the server. Your text is kept — try again."); })
      .then(function () { done("create"); });
  }
  function reply(tid, body) {
    var key = "reply:" + tid;
    if (!busy(key)) return;
    postJSON("/api/threads/" + tid + "/replies", { body: body, author: "human" })
      .then(function (r) {
        if (!r.ok) { toast("Reply failed (server " + r.status + ")."); return; }
        delete drafts[tid];
        return loadThen(function () { activeTid = tid; }).then(function () {
          var ta = list.querySelector('.mg-card[data-tid="' + tid + '"] textarea');
          if (ta) ta.focus(); // keep the user in the thread they just replied to
        });
      })
      .catch(function () { toast("Reply failed — server unreachable."); })
      .then(function () { done(key); });
  }
  function setStatus(tid, status, isUndo) {
    var key = "status:" + tid;
    if (!busy(key)) return;
    // optimistic in-place cue: the live highlight fades amber→muted immediately
    // (the reload repaints to the same state), so resolve feels instant
    article.querySelectorAll('mark.mg-hl[data-tid="' + tid + '"]').forEach(function (m) {
      m.classList.toggle("resolved", status === "resolved");
    });
    postJSON("/api/threads/" + tid, { status: status, by: "human" }, "PATCH")
      .then(function (r) {
        // revert the optimistic highlight by re-syncing from the server on failure
        if (!r.ok) { toast("Update failed (server " + r.status + ").", { error: true }); return loadThen(); }
        // activate AFTER the reload/render so the thread follows to its new tab
        // (instead of silently vanishing) and stays the keyboard target
        return loadThen().then(function () {
          activate(tid, true);
          if (isUndo) { toast(status === "resolved" ? "Resolved" : "Reopened"); return; }
          var undoTo = status === "resolved" ? "open" : "resolved";
          toast(status === "resolved" ? "Resolved" : "Reopened", {
            action: { label: "Undo", onClick: function () { setStatus(tid, undoTo, true); } },
          });
          maybeCelebrate();
        });
      })
      .catch(function () { toast("Update failed — server unreachable.", { error: true }); })
      .then(function () { done(key); });
  }
  function editComment(tid, cid, body) {
    var key = "edit:" + cid;
    if (!busy(key)) return;
    postJSON("/api/threads/" + tid + "/comments/" + cid, { body: body }, "PATCH")
      .then(function (r) {
        if (!r.ok) { toast("Edit failed (server " + r.status + ").", { error: true }); return; }
        return loadThen(function () { activeTid = tid; }).then(function () { toast("Edited"); });
      })
      .catch(function () { toast("Edit failed — server unreachable.", { error: true }); })
      .then(function () { done(key); });
  }
  function deleteThread(tid) {
    var key = "del:" + tid;
    if (!busy(key)) return;
    postJSON("/api/threads/" + tid, null, "DELETE")
      .then(function (r) {
        if (!r.ok) { toast("Delete failed (server " + r.status + ").", { error: true }); return; }
        if (activeTid === tid) activeTid = null; // it's gone — drop the keyboard target
        return loadThen().then(function () { toast("Comment deleted"); });
      })
      .catch(function () { toast("Delete failed — server unreachable.", { error: true }); })
      .then(function () { done(key); });
  }

  // a quiet flourish when the last open thread is resolved — the emotional peak.
  // Reveal the clean state on the Open tab rather than following the thread away.
  function maybeCelebrate() {
    var open = threads.filter(function (t) { return !t.orphaned && t.status !== "resolved"; }).length;
    if (open === 0 && threads.length > 0) {
      filter = "open";
      activeTid = null; // the just-resolved thread is hidden on this tab; don't keep it as the j/k/e target
      syncTabs();
      renderSidebar(); // shows the "The manuscript is clean." empty state
    }
  }
  function loadThen(after) {
    // own terminal handling: a failed REFRESH must not reject into the mutation's
    // catch (which would falsely report the mutation itself failed)
    return fetch(apiDoc + "?status=all")
      .then(function (r) { if (!r.ok) throw new Error("reload failed"); return r.json(); })
      .then(function (data) {
        threads = (data && data.threads) || [];
        if (after) after();
        render();
      })
      .catch(function () { toast("Saved, but couldn’t refresh — reload to see the latest."); });
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
    activate(items[idx].id, true, true); // focusIt: move the focus ring with j/k
  }

  function toggleActive() {
    var t = threadById(activeTid);
    if (!t || t.orphaned) return;
    setStatus(t.id, t.status === "resolved" ? "open" : "resolved");
  }

  document.addEventListener("keydown", function (e) {
    var tag = (e.target.tagName || "").toLowerCase();
    var typing = tag === "textarea" || tag === "input";
    if (e.key === "Escape") {
      if (typing && !document.getElementById("mg-composer")) { e.target.blur(); return; } // blur a reply field, keep the panel
      if (document.getElementById("mg-composer")) { closeComposer(); return; }
      hidePill();
      if (sidebar.classList.contains("open")) closeSidebar();
      return;
    }
    if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
    if (document.getElementById("mg-composer")) return; // modal composer: no background shortcuts
    if (helpEl && e.key !== "?") return; // help overlay open: only ? is meaningful

    switch (e.key) {
      case "c": {
        var a = captureAnchor();
        if (a) {
          pending = a;
          showPill();
          openComposer();
        } else {
          openDocComposer(); // nothing selected → a note on the whole document
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
        if (activeTid == null) { toast("Select a comment first (use j/k)."); break; }
        var ta = list.querySelector('.mg-card[data-tid="' + activeTid + '"] textarea');
        if (!ta) { activate(activeTid, true); ta = list.querySelector('.mg-card[data-tid="' + activeTid + '"] textarea'); }
        if (ta) { openSidebar(); ta.focus(); e.preventDefault(); }
        break;
      }
      case "e":
        toggleActive();
        break;
      case "?":
        openHelp();
        break;
      case "Enter":
        if (activeTid != null) activate(activeTid, true, true);
        break;
    }
  });

  var reflowPending = false;
  window.addEventListener("resize", function () {
    syncSidebarMode(); // reconcile scrim / modal state across the breakpoint
    // Coalesce to the next frame so the gutter dots reposition in the SAME paint
    // as the text reflow (a debounce timer left them visibly drifted mid-resize).
    if (reflowPending) return;
    reflowPending = true;
    requestAnimationFrame(function () { reflowPending = false; layoutGutter(); });
  });
  window.addEventListener("scroll", function () {
    if (pill.classList.contains("show")) hidePill();
  }, { passive: true });

  // Touch: long-press selection fires selectionchange, not a reliable mouseup.
  if (mqCoarse.matches) {
    var selT;
    document.addEventListener("selectionchange", function () {
      clearTimeout(selT);
      selT = setTimeout(function () {
        if (document.getElementById("mg-composer")) return;
        var a = captureAnchor();
        if (a) { pending = a; showPill(); } else hidePill();
      }, 300);
    });
  }
  // Reflect comments made in another tab/session when this one is refocused.
  document.addEventListener("visibilitychange", function () {
    if (!document.hidden && !document.getElementById("mg-composer")) load();
  });

  setInterval(tickTimes, 60000); // keep relative timestamps fresh

  load();
})();
