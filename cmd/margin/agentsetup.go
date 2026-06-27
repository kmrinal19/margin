package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// cmdAgentSetup registers `margin mcp` with whichever coding agents are installed,
// writing ONLY to each agent's own config (never the user's repo / instruction
// files). Agents with their own CLI (Claude Code, Codex) are configured by
// shelling out to it; file-based agents (Cursor, Gemini, Windsurf) get an
// idempotent JSON merge. Anything that can't be done safely is printed as a
// copy-paste snippet. --print-only changes nothing.
func cmdAgentSetup(args []string) error {
	fs := flag.NewFlagSet("agent-setup", flag.ExitOnError)
	printOnly := fs.Bool("print-only", false, "show what would be registered, change nothing")
	_ = fs.Parse(args)

	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "margin"
	}
	exe, _ = filepath.Abs(exe)

	ctx, stop := clientCtx()
	defer stop()

	home, _ := os.UserHomeDir()
	var did, manual []string

	for _, a := range knownAgents(home, exe) {
		if !a.installed() {
			continue
		}
		if *printOnly {
			did = append(did, "• "+a.name+" — "+a.describe())
			continue
		}
		if err := a.register(ctx); err != nil {
			manual = append(manual, fmt.Sprintf("• %s: automatic setup failed (%v).\n    Do it manually: %s", a.name, err, a.manual()))
			continue
		}
		did = append(did, "✓ "+a.name+" — "+a.describe())
	}

	if len(did) == 0 && len(manual) == 0 {
		fmt.Println("No supported agents detected on this machine.")
		fmt.Println("Once you install one (Claude Code, Codex, Cursor, Gemini CLI, Windsurf), re-run:")
		fmt.Println("  margin agent-setup")
		fmt.Println("\nOr register margin manually with any MCP client using:")
		fmt.Printf("  command: %s\n  args:    [\"mcp\"]\n", exe)
		return nil
	}
	if *printOnly {
		fmt.Println("Would register margin with these agents (run without --print-only to apply):")
	} else {
		fmt.Println("Registered margin's MCP tools with your agents:")
	}
	for _, l := range did {
		fmt.Println("  " + l)
	}
	for _, l := range manual {
		fmt.Println("  " + l)
	}
	fmt.Println("\nNothing in your repo was modified — only each agent's own config.")
	fmt.Println("Restart the agent (or start a new session) to pick up the margin tools.")
	return nil
}

type agent struct {
	name      string
	installed func() bool
	register  func(ctx context.Context) error
	describe  func() string
	manual    func() string
}

func knownAgents(home, exe string) []agent {
	// CLI-based agents: let their own tool write their own config (safest).
	cli := func(name, bin string, addArgs []string) agent {
		return agent{
			name:      name,
			installed: func() bool { _, err := exec.LookPath(bin); return err == nil },
			register: func(ctx context.Context) error {
				full := append(append([]string{}, addArgs...), "--", exe, "mcp")
				out, err := exec.CommandContext(ctx, bin, full...).CombinedOutput()
				if err != nil && !strings.Contains(strings.ToLower(string(out)), "already") {
					return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
				}
				return nil
			},
			describe: func() string { return "via `" + bin + " " + strings.Join(addArgs, " ") + "`" },
			manual:   func() string { return bin + " " + strings.Join(addArgs, " ") + " -- " + exe + " mcp" },
		}
	}
	// File-based agents: idempotent JSON merge into their own config file.
	file := func(name, dir, path, key string) agent {
		return agent{
			name:      name,
			installed: func() bool { _, err := os.Stat(dir); return err == nil },
			register:  func(_ context.Context) error { return mergeMCPConfig(path, key, exe) },
			describe:  func() string { return "in " + short(home, path) },
			manual:    func() string { return "add margin to " + short(home, path) + " under \"" + key + "\"" },
		}
	}

	return []agent{
		cli("Claude Code", "claude", []string{"mcp", "add", "--scope", "user", "margin"}),
		cli("Codex", "codex", []string{"mcp", "add", "margin"}),
		file("Cursor", filepath.Join(home, ".cursor"), filepath.Join(home, ".cursor", "mcp.json"), "mcpServers"),
		file("Gemini CLI", filepath.Join(home, ".gemini"), filepath.Join(home, ".gemini", "settings.json"), "mcpServers"),
		file("Windsurf", filepath.Join(home, ".codeium", "windsurf"), filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), "mcpServers"),
	}
}

// mergeMCPConfig adds (idempotently) a "margin" stdio server under key in the
// JSON config at path, preserving every other key. Writes atomically. If the
// existing file isn't plain JSON (e.g. JSONC with comments), it errors so the
// caller can fall back to printing a manual snippet rather than corrupting it.
func mergeMCPConfig(path, key, exe string) error {
	root := map[string]any{}
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &root); err != nil {
			return fmt.Errorf("existing config isn't plain JSON")
		}
	}
	servers, _ := root[key].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["margin"] = map[string]any{"command": exe, "args": []string{"mcp"}}
	root[key] = servers

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.margin-%d.tmp", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func short(home, p string) string {
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
