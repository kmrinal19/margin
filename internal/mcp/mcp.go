// Package mcp is a tiny, dependency-free Model Context Protocol server over stdio.
//
// It implements just enough of MCP for a tool-only server: the initialize
// handshake, tools/list, tools/call, and ping, speaking newline-delimited
// JSON-RPC 2.0 on stdin/stdout (the MCP stdio transport). Keeping it hand-rolled
// preserves margin's single-static-binary / pure-Go invariant — no SDK, no deps.
//
// stdout is the protocol channel: handlers MUST NOT write to it. All diagnostics
// go to stderr (the caller's logger).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// protocolVersion is the MCP revision this server implements. On initialize we
// echo the client's requested version when present (a tool-only server is
// compatible across recent revisions) and fall back to this otherwise.
const protocolVersion = "2025-06-18"

// Tool is one callable tool. InputSchema is a JSON Schema object describing the
// arguments; Handler receives the raw arguments object and returns text content.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Handler     func(ctx context.Context, args json.RawMessage) (string, error)
}

// Server is a tool-only MCP server.
type Server struct {
	Name         string
	Version      string
	Instructions string // surfaced to the model on initialize; the onboarding copy
	Tools        []Tool
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads JSON-RPC messages from in and writes responses to out until in is
// exhausted (the client closed the pipe) or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // allow large tool payloads
	enc := json.NewEncoder(out)

	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Can't recover an id from an unparseable message; per JSON-RPC, reply
			// with a null-id parse error.
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		resp, isNotification := s.dispatch(ctx, &req)
		if isNotification {
			continue // notifications get no response
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) dispatch(ctx context.Context, req *rpcRequest) (rpcResponse, bool) {
	isNotification := len(req.ID) == 0
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		ver := protocolVersion
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &p) == nil && p.ProtocolVersion != "" {
			ver = p.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
			"instructions":    s.Instructions,
		}
	case "notifications/initialized", "notifications/cancelled":
		return resp, true // notifications: no reply
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.toolList()}
	case "tools/call":
		resp.Result, resp.Error = s.callTool(ctx, req.Params)
	default:
		if isNotification {
			return resp, true // ignore unknown notifications
		}
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp, isNotification
}

func (s *Server) toolList() []map[string]any {
	out := make([]map[string]any, 0, len(s.Tools))
	for _, t := range s.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": schema,
		})
	}
	return out
}

// callTool runs a tools/call request. Per MCP, a tool's own failure is reported
// as a successful result with isError:true (so the model sees the message),
// while a malformed request or unknown tool is a protocol-level error.
func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid tools/call params"}
	}
	for _, t := range s.Tools {
		if t.Name != p.Name {
			continue
		}
		text, err := t.Handler(ctx, p.Arguments)
		if err != nil {
			return toolResult(fmt.Sprintf("error: %v", err), true), nil
		}
		return toolResult(text, false), nil
	}
	return nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
}

func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
}
