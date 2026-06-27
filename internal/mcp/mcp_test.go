package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// drive runs the server over a set of newline-delimited request lines and returns
// the decoded responses (in order; notifications produce none).
func drive(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resps []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("bad response line %q: %v", ln, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func testServer() *Server {
	return &Server{
		Name: "margin", Version: "test", Instructions: "do the thing",
		Tools: []Tool{{
			Name: "echo", Description: "echo back",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
			Handler: func(_ context.Context, args json.RawMessage) (string, error) {
				var a struct {
					Msg string `json:"msg"`
				}
				_ = json.Unmarshal(args, &a)
				if a.Msg == "boom" {
					return "", errTest
				}
				return "you said: " + a.Msg, nil
			},
		}},
	}
}

var errTest = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "kaboom" }

func TestInitializeEchoesProtocolAndInstructions(t *testing.T) {
	r := drive(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	res := r[0]["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", res["protocolVersion"])
	}
	if res["instructions"] != "do the thing" {
		t.Errorf("instructions = %v", res["instructions"])
	}
	if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("missing tools capability")
	}
}

func TestNotificationProducesNoResponse(t *testing.T) {
	r := drive(t, testServer(),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	if len(r) != 1 {
		t.Fatalf("want 1 response (notification suppressed), got %d", len(r))
	}
	if r[0]["id"].(float64) != 7 {
		t.Errorf("expected the ping response, got id %v", r[0]["id"])
	}
}

func TestToolsListAndCall(t *testing.T) {
	r := drive(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`)
	tools := r[0]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "echo" {
		t.Fatalf("tools/list = %v", tools)
	}
	call := r[1]["result"].(map[string]any)
	if call["isError"].(bool) {
		t.Error("echo should not be an error")
	}
	text := call["content"].([]any)[0].(map[string]any)["text"]
	if text != "you said: hi" {
		t.Errorf("tool text = %v", text)
	}
}

func TestToolFailureIsResultNotProtocolError(t *testing.T) {
	// a handler error must surface as a result with isError:true (so the model
	// sees it), NOT a JSON-RPC error.
	r := drive(t, testServer(),
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"boom"}}}`)
	if _, isErr := r[0]["error"]; isErr {
		t.Fatal("tool failure must not be a JSON-RPC error")
	}
	res := r[0]["result"].(map[string]any)
	if !res["isError"].(bool) {
		t.Error("expected isError:true on handler failure")
	}
}

func TestUnknownMethodAndToolAreProtocolErrors(t *testing.T) {
	r := drive(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"does/notexist"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ghost","arguments":{}}}`)
	if r[0]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Errorf("unknown method should be -32601, got %v", r[0]["error"])
	}
	if _, ok := r[1]["error"].(map[string]any); !ok {
		t.Error("unknown tool should be a protocol error")
	}
}

func TestParseErrorYieldsNullIDError(t *testing.T) {
	r := drive(t, testServer(), `{not json`)
	if r[0]["id"] != nil {
		t.Errorf("parse error id should be null, got %v", r[0]["id"])
	}
	if r[0]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Errorf("want parse error -32700, got %v", r[0]["error"])
	}
}
