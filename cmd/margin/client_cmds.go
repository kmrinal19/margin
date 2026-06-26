package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/kmrinal19/margin/internal/client"
	"github.com/kmrinal19/margin/internal/store"
)

const defaultServer = "http://127.0.0.1:8848"

func clientCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// parseArgs parses flags that may appear before OR after positional arguments
// (Go's flag package otherwise stops at the first positional), returning the
// positionals. This lets `comments welcome --json` and `resolve 42 --note x` work.
func parseArgs(fs *flag.FlagSet, args []string) []string {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return positionals // ExitOnError already handled real errors
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		positionals = append(positionals, args[0])
		args = args[1:]
	}
	return positionals
}

// emitJSON writes compact JSON to stdout (token-minimal for an agent). Logs and
// errors go to stderr (via main), keeping stdout pure machine output.
func emitJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func cmdDocs(args []string) error {
	fs := flag.NewFlagSet("docs", flag.ExitOnError)
	server := fs.String("server", defaultServer, "margin server URL")
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	ctx, stop := clientCtx()
	defer stop()
	docs, err := client.New(*server).Docs(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		return emitJSON(docs)
	}
	if len(docs) == 0 {
		fmt.Println("no docs found in ./docs")
		return nil
	}
	for _, d := range docs {
		suffix := ""
		if d.OpenCount > 0 {
			suffix = fmt.Sprintf("  · %d open", d.OpenCount)
		}
		fmt.Printf("%-24s %s%s\n", d.Slug, d.Title, suffix)
	}
	return nil
}

// compactThread is the token-minimal shape an agent consumes (Appendix C):
// t=thread id, b=block id, q=exact quote, ctx=[prefix,suffix], c=latest comment,
// s=status. The (b,q) pair lets the agent locate the span without re-reading the doc.
// With --full, msgs carries the whole thread (author + body) so the agent never
// mistakes its own prior note for new human feedback, and qt/relocated expose a
// multi-block tail and a drifted anchor.
type compactMsg struct {
	A string `json:"a"` // author: human | ai
	B string `json:"b"` // body
}

type compactThread struct {
	T         int64        `json:"t"`
	B         string       `json:"b"`
	Q         string       `json:"q"`
	Qt        string       `json:"qt,omitempty"` // multi-block tail quote
	Ctx       [2]string    `json:"ctx"`
	C         string       `json:"c"`
	S         string       `json:"s"`
	N         int          `json:"n,omitempty"`         // message count
	Msgs      []compactMsg `json:"msgs,omitempty"`      // full history (--full)
	Mine      bool         `json:"mine,omitempty"`      // latest message is the agent's own
	Relocated bool         `json:"relocated,omitempty"` // anchor was fuzzy-healed (verify the span)
	Orphaned  bool         `json:"orphaned,omitempty"`
}

func toCompact(threads []store.Thread, full bool) []compactThread {
	out := make([]compactThread, 0, len(threads))
	for _, t := range threads {
		latest, lastAuthor := "", ""
		if n := len(t.Comments); n > 0 {
			latest = t.Comments[n-1].Body
			lastAuthor = t.Comments[n-1].Author
		}
		ct := compactThread{
			T:         t.ID,
			B:         t.Anchor.BlockID,
			Q:         t.Anchor.QuoteExact,
			Qt:        t.Anchor.QuoteTail,
			Ctx:       [2]string{t.Anchor.QuotePrefix, t.Anchor.QuoteSuffix},
			C:         latest,
			S:         t.Status,
			N:         len(t.Comments),
			Mine:      lastAuthor == "ai",
			Relocated: !t.Orphaned && t.Anchor.Confidence > 0 && t.Anchor.Confidence < 1,
			Orphaned:  t.Orphaned,
		}
		if full {
			ct.Msgs = make([]compactMsg, 0, len(t.Comments))
			for _, m := range t.Comments {
				ct.Msgs = append(ct.Msgs, compactMsg{A: m.Author, B: m.Body})
			}
		}
		out = append(out, ct)
	}
	return out
}

func cmdComments(args []string) error {
	fs := flag.NewFlagSet("comments", flag.ExitOnError)
	server := fs.String("server", defaultServer, "margin server URL")
	open := fs.Bool("open", true, "only open threads (the default)")
	all := fs.Bool("all", false, "include resolved threads")
	asJSON := fs.Bool("json", false, "token-minimal JSON for an agent")
	full := fs.Bool("full", false, "with --json: include full thread history (author + body)")
	rest := parseArgs(fs, args)
	if len(rest) < 1 {
		return errors.New(`usage: margin comments <slug> [--open|--all] [--json [--full]]`)
	}
	// --all includes resolved; --open=false also means "show all".
	status := "open"
	if *all || !*open {
		status = "all"
	}

	ctx, stop := clientCtx()
	defer stop()
	threads, err := client.New(*server).Comments(ctx, rest[0], status)
	if err != nil {
		return err
	}
	if *asJSON {
		return emitJSON(toCompact(threads, *full))
	}
	printThreads(threads, rest[0])
	return nil
}

func cmdReply(args []string) error {
	fs := flag.NewFlagSet("reply", flag.ExitOnError)
	server := fs.String("server", defaultServer, "margin server URL")
	note := fs.String("note", "", "reply body (posted as an 'ai' comment; thread stays open)")
	rest := parseArgs(fs, args)
	if len(rest) < 1 || *note == "" {
		return errors.New(`usage: margin reply <thread-id> --note "…"`)
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid thread id %q", rest[0])
	}
	ctx, stop := clientCtx()
	defer stop()
	if _, err := client.New(*server).Reply(ctx, id, "ai", *note); err != nil {
		return err
	}
	fmt.Printf("replied to thread #%d\n", id)
	return nil
}

func printThreads(threads []store.Thread, slug string) {
	if len(threads) == 0 {
		fmt.Printf("no comments on %q\n", slug)
		return
	}
	for _, t := range threads {
		tag := t.Status
		if t.Orphaned {
			tag = "orphaned"
		}
		fmt.Printf("#%d [%s]  “%s”\n", t.ID, tag, ellipsis(t.Anchor.QuoteExact, 70))
		for _, c := range t.Comments {
			fmt.Printf("    %s: %s\n", c.Author, ellipsis(oneLine(c.Body), 100))
		}
		fmt.Printf("    ↳ block %s\n", t.Anchor.BlockID)
	}
}

func cmdResolve(args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	server := fs.String("server", defaultServer, "margin server URL")
	note := fs.String("note", "", "optional resolution note (posted as an 'ai' reply)")
	rest := parseArgs(fs, args)
	if len(rest) < 1 {
		return errors.New(`usage: margin resolve <thread-id> [--note "…"]`)
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid thread id %q", rest[0])
	}
	ctx, stop := clientCtx()
	defer stop()
	th, err := client.New(*server).SetStatus(ctx, id, store.StatusResolved, "ai", *note)
	if err != nil {
		return err
	}
	fmt.Printf("resolved thread #%d\n", th.ID)
	return nil
}

func cmdReopen(args []string) error {
	fs := flag.NewFlagSet("reopen", flag.ExitOnError)
	server := fs.String("server", defaultServer, "margin server URL")
	rest := parseArgs(fs, args)
	if len(rest) < 1 {
		return errors.New("usage: margin reopen <thread-id>")
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid thread id %q", rest[0])
	}
	ctx, stop := clientCtx()
	defer stop()
	th, err := client.New(*server).SetStatus(ctx, id, store.StatusOpen, "ai", "")
	if err != nil {
		return err
	}
	fmt.Printf("reopened thread #%d\n", th.ID)
	return nil
}

func ellipsis(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
