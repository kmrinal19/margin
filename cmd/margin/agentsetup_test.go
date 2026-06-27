package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeMCPConfigPreservesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	// pre-existing config with another server + an unrelated user setting
	const existing = `{"mcpServers":{"other":{"command":"foo","args":["bar"]}},"keepMe":true}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mergeMCPConfig(path, "mcpServers", "/usr/local/bin/margin"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// merging twice must not duplicate or change anything further
	if err := mergeMCPConfig(path, "mcpServers", "/usr/local/bin/margin"); err != nil {
		t.Fatalf("merge (2nd): %v", err)
	}

	var root map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if root["keepMe"] != true {
		t.Error("unrelated user setting was dropped")
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("pre-existing 'other' server was dropped")
	}
	m, ok := servers["margin"].(map[string]any)
	if !ok {
		t.Fatal("margin server not added")
	}
	if m["command"] != "/usr/local/bin/margin" {
		t.Errorf("margin command = %v", m["command"])
	}
	if args, _ := m["args"].([]any); len(args) != 1 || args[0] != "mcp" {
		t.Errorf("margin args = %v", m["args"])
	}
}

func TestMergeMCPConfigCreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	if err := mergeMCPConfig(path, "mcpServers", "/bin/margin"); err != nil {
		t.Fatalf("merge into new file: %v", err)
	}
	var root map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["mcpServers"].(map[string]any)["margin"]; !ok {
		t.Error("margin not registered in a fresh config")
	}
}

func TestMergeMCPConfigRefusesNonJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	// JSONC with a comment — must NOT be clobbered; caller falls back to a snippet.
	if err := os.WriteFile(path, []byte("{\n  // a comment\n  \"mcpServers\": {}\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mergeMCPConfig(path, "mcpServers", "/bin/margin"); err == nil {
		t.Error("expected an error on non-plain-JSON config (so we don't corrupt it)")
	}
	// the original file must be untouched
	b, _ := os.ReadFile(path)
	if want := "// a comment"; !contains(string(b), want) {
		t.Error("the non-JSON config was modified")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
