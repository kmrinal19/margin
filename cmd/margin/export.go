package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kmrinal19/margin/internal/anchor"
	"github.com/kmrinal19/margin/internal/reanchor"
	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
	"github.com/kmrinal19/margin/web"
)

// cmdExport writes a portable, server-free single-file HTML: the rendered doc
// with inlined CSS plus a read-only snapshot of its review comments. It reads
// the comment store directly (read-only) so it works without a running server.
func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	docsDir := fs.String("docs", "./docs", "directory of Markdown docs")
	dataDir := fs.String("data", "./data", "directory holding the comment store")
	out := fs.String("out", "", "output file (default <slug>.html)")
	_ = fs.Bool("inline", true, "produce a self-contained single file (always on)")
	rest := parseArgs(fs, args)
	if len(rest) < 1 {
		return errors.New(`usage: margin export <slug> [--out file.html]`)
	}
	slug := rest[0]
	if !render.ValidSlug(slug) {
		return fmt.Errorf("invalid slug %q", slug)
	}

	src, err := os.ReadFile(filepath.Join(*docsDir, filepath.FromSlash(slug)+".md"))
	if err != nil {
		return fmt.Errorf("read doc: %w", err)
	}

	rnd, err := render.New()
	if err != nil {
		return err
	}
	art, err := rnd.RenderArticle(src)
	if err != nil {
		return err
	}
	if art.Title == "" {
		art.Title = slug
	}

	ctx, stop := clientCtx() // honor SIGINT/SIGTERM like the other subcommands
	defer stop()
	st, err := store.Open(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	threads, err := st.ListThreads(ctx, slug, store.FilterAll)
	if err != nil {
		return err
	}

	// Re-resolve anchors with the SAME logic the live server uses (shared
	// reanchor package), so the export's statuses — including orphaned, and the
	// never-orphan rule for document-level and multi-block notes — match exactly.
	doc := anchor.NewDoc(rnd.Blocks(src))
	var ets []render.ExportThread
	for _, t := range threads {
		_, orphaned := reanchor.Resolve(doc, t.Anchor)
		ets = append(ets, toExportThread(t, !orphaned))
	}

	css, err := web.Static.ReadFile("design-system.css")
	if err != nil {
		return err
	}

	outPath := *out
	if outPath == "" {
		// flatten a nested slug into a single filename (payments/refunds -> payments-refunds.html)
		outPath = strings.ReplaceAll(slug, "/", "-") + ".html"
	}
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	if err := rnd.ExportDoc(f, art, string(css), ets); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("exported %s → %s (%d comment thread(s))\n", slug, outPath, len(ets))
	return nil
}

func toExportThread(t store.Thread, anchored bool) render.ExportThread {
	class, label := "info", "Open"
	switch {
	case !anchored:
		class, label = "warn", "Orphaned — could not be re-located in the current doc"
	case t.Status == store.StatusResolved:
		class, label = "good", "Resolved"
	}
	msgs := make([]render.ExportMessage, 0, len(t.Comments))
	for _, c := range t.Comments {
		msgs = append(msgs, render.ExportMessage{
			Author: c.Author,
			Body:   c.Body,
			When:   c.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return render.ExportThread{
		Quote:        t.Anchor.QuoteExact,
		CalloutClass: class,
		StatusLabel:  label,
		Messages:     msgs,
	}
}
