package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/server"
	"github.com/kmrinal19/margin/internal/store"
)

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	host := fs.String("host", "127.0.0.1", "bind host (offline-only; do not expose publicly)")
	port := fs.Int("port", 8848, "listen port")
	docs := fs.String("docs", "./docs", "directory of Markdown docs")
	data := fs.String("data", "./data", "directory for the SQLite comment store")
	debug := fs.Bool("debug", false, "expose pprof/expvar on loopback")
	verbose := fs.Bool("verbose", false, "verbose (debug-level) logging")
	allowRemote := fs.Bool("insecure-allow-remote", false, "allow a non-loopback --host (exposes the unauthenticated comment API)")
	_ = fs.Parse(args)

	// Offline-only invariant: refuse non-loopback binds unless explicitly overridden.
	if !*allowRemote && !server.IsLoopbackHost(*host) {
		return fmt.Errorf("refusing to bind non-loopback host %q (margin is offline-only); pass --insecure-allow-remote to override", *host)
	}

	level := new(slog.LevelVar)
	if *verbose {
		level.Set(slog.LevelDebug)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	if *allowRemote && !server.IsLoopbackHost(*host) {
		log.Warn("binding a NON-LOOPBACK host — the unauthenticated comment API is exposed on the network", "host", *host)
	}

	rnd, err := render.New()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *data)
	if err != nil {
		return err
	}
	// Closed after Run returns (i.e. after graceful shutdown) so it checkpoints
	// the WAL cleanly and no in-flight comment write is lost.
	defer func() { _ = st.Close() }()

	srv := server.New(server.Config{
		Host:    *host,
		Port:    *port,
		DocsDir: *docs,
		DataDir: *data,
		Debug:   *debug,
	}, log, rnd, st)

	return srv.Run(ctx)
}
