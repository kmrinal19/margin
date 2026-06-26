// Command margin is a local, offline tool for reviewing AI-authored design docs:
// it renders Markdown to polished HTML, lets a human leave inline anchored
// comments, and exposes those comments via this CLI so an agent can revise and
// resolve them.
package main

import (
	"fmt"
	"os"
)

// version is the build version; override with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch cmd := os.Args[1]; cmd {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "docs":
		err = cmdDocs(os.Args[2:])
	case "comments":
		err = cmdComments(os.Args[2:])
	case "resolve":
		err = cmdResolve(os.Args[2:])
	case "reopen":
		err = cmdReopen(os.Args[2:])
	case "export":
		err = cmdExport(os.Args[2:])
	case "version", "-version", "--version":
		fmt.Println("margin", version)
		return
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "margin: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "margin: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `margin — review AI-authored design docs locally

usage:
  margin serve    [--port 8848] [--host 127.0.0.1] [--docs ./docs] [--data ./data]
  margin docs                                  list docs + open-comment counts
  margin comments <slug> [--open] [--json]     list comment threads (token-minimal with --json)
  margin resolve  <thread-id> [--note "…"]     resolve a thread (agent)
  margin reopen   <thread-id>                  reopen a resolved thread
  margin export   <slug> --inline              write a portable single-file HTML
  margin version

`)
}
