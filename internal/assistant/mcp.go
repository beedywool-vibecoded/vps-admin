package assistant

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/beedywool/vps-admin/internal/store"
)

// The Claude Code CLI is the model runtime for the subscription provider. It ships its
// own tools (Bash, Edit, Read…), which would run outside the panel's policy, so they are
// switched off when it starts. The panel's tools are handed to it through a small MCP
// server on loopback, created for the duration of one turn and reachable with a token
// that only this turn knows. A change waits inside the tool call until the administrator
// answers the card in the panel, exactly like a tool call made through the API.

// bridge serves one conversation's tools to one CLI process.
type bridge struct {
	e      *Engine
	convID string
	token  string
	url    string
	ln     net.Listener
	srv    *http.Server

	mu  sync.Mutex
	ord int
}

// newBridge starts the MCP endpoint on an ephemeral loopback port.
func (e *Engine) newBridge(convID string) (*bridge, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not open a local port for the Claude Code tools: %w", err)
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		ln.Close()
		return nil, err
	}
	b := &bridge{e: e, convID: convID, token: hex.EncodeToString(buf), ln: ln}
	b.url = "http://" + ln.Addr().String() + "/mcp"
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", b.handle)
	b.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := b.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Warn("assistant mcp server", "err", err)
		}
	}()
	return b, nil
}

func (b *bridge) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b.srv.Shutdown(ctx)
}

// config is the JSON handed to `claude --mcp-config`.
func (b *bridge) config() string {
	cfg := map[string]any{"mcpServers": map[string]any{
		"vps": map[string]any{
			"type":    "http",
			"url":     b.url,
			"headers": map[string]string{"Authorization": "Bearer " + b.token},
		},
	}}
	raw, _ := json.Marshal(cfg)
	return string(raw)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (b *bridge) handle(w http.ResponseWriter, r *http.Request) {
	want := "Bearer " + b.token
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodPost:
	case http.MethodGet, http.MethodDelete:
		// No server-initiated stream and no session to delete: the endpoint lives as long
		// as the turn.
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}
	if req.ID == nil { // a notification, eg. notifications/initialized
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		version := p.ProtocolVersion
		if version == "" {
			version = "2025-06-18"
		}
		writeRPC(w, req.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "vps-admin", "version": "1.0.0"},
		})
	case "ping":
		writeRPC(w, req.ID, map[string]any{})
	case "tools/list":
		writeRPC(w, req.ID, map[string]any{"tools": mcpTools()})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeRPCError(w, req.ID, -32602, "invalid params")
			return
		}
		text, isErr := b.call(r.Context(), p.Name, p.Arguments)
		writeRPC(w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isErr,
		})
	default:
		writeRPCError(w, req.ID, -32601, "unknown method "+req.Method)
	}
}

func writeRPC(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": msg},
	})
}

// mcpTools describes the panel tools in the MCP dialect: same names, same arguments,
// plain JSON Schema.
func mcpTools() []map[string]any {
	out := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		required := make([]string, 0, len(d.props))
		for k := range d.props {
			required = append(required, k)
		}
		out = append(out, map[string]any{
			"name":        d.name,
			"description": d.description,
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           d.props,
				"required":             required,
				"additionalProperties": false,
			},
		})
	}
	return out
}

// call runs one panel tool for the CLI, recording it so the panel shows the same card
// and the same audit line as a tool call made through the API.
func (b *bridge) call(ctx context.Context, name string, args json.RawMessage) (string, bool) {
	d, c, err := parse(name, string(args))
	if err != nil {
		return err.Error() + ". Call the tool again with complete, valid arguments.", true
	}
	kind, why := kindOf(d, c)
	summary := Display(d, c)
	if kind == Blocked {
		return "Blocked by the panel policy (" + why + "). Do not retry; give the administrator the command to run over SSH if it is really needed.", true
	}
	autoRead := b.e.Store.Setting(ctx, "assistant.auto_read", "true") == "true"
	b.mu.Lock()
	b.ord++
	ord := b.ord
	b.mu.Unlock()
	tc := &store.ToolCall{
		ID: "mcp_" + newID(), ConvID: b.convID, Batch: 0, Ord: ord, Name: name,
		Input: string(args), Summary: summary, Kind: string(kind), Status: "pending",
		CreatedAt: time.Now(),
	}
	if kind == Read && autoRead {
		tc.Status = "running"
	}
	if err := b.e.Store.AddToolCall(ctx, tc); err != nil {
		return "The panel could not record this action: " + err.Error(), true
	}
	b.e.emit(b.convID, "tool", tc)

	if tc.Status == "pending" {
		approved, err := b.e.waitForDecision(ctx, tc.ID)
		if err != nil {
			b.e.Store.FinishToolCall(context.WithoutCancel(ctx), tc.ID, "denied", err.Error(), true, "")
			tc.Status, tc.Result, tc.IsError = "denied", err.Error(), true
			b.e.emit(b.convID, "tool", tc)
			return err.Error(), true
		}
		if !approved {
			res := "The administrator denied this action."
			tc.Status, tc.Result, tc.IsError = "denied", res, true
			b.e.Store.FinishToolCall(context.WithoutCancel(ctx), tc.ID, tc.Status, tc.Result, true, "")
			b.e.emit(b.convID, "tool", tc)
			return res, true
		}
		tc.Status = "running"
		b.e.emit(b.convID, "tool", tc)
	}

	res, err := b.e.execute(ctx, d, c)
	if err != nil {
		res, tc.IsError = truncate(strings.TrimSpace(res+"\n"+err.Error())), true
	} else {
		res = truncate(res)
	}
	tc.Status, tc.Result = "done", res
	b.e.Store.FinishToolCall(context.WithoutCancel(ctx), tc.ID, tc.Status, tc.Result, tc.IsError, "")
	b.e.emit(b.convID, "tool", tc)
	return res, tc.IsError
}

// waitForDecision blocks until the administrator answers the card in the panel, the
// conversation is stopped, or the wait times out. The CLI process simply waits: its
// turn cannot go on without the tool result.
func (e *Engine) waitForDecision(ctx context.Context, callID string) (bool, error) {
	ch := make(chan bool, 1)
	e.mu.Lock()
	if e.waiters == nil {
		e.waiters = map[string]chan bool{}
	}
	e.waiters[callID] = ch
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.waiters, callID)
		e.mu.Unlock()
	}()
	select {
	case ok := <-ch:
		return ok, nil
	case <-ctx.Done():
		return false, errStopped
	case <-time.After(30 * time.Minute):
		return false, fmt.Errorf("the administrator did not answer this action in time")
	}
}

// waiting reports whether a CLI turn is blocked on this call.
func (e *Engine) waiting(callID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.waiters[callID] != nil
}

// signalDecision hands the administrator's answer to a waiting tool call. It reports
// whether a CLI turn owns that call.
func (e *Engine) signalDecision(callID string, approve bool) bool {
	e.mu.Lock()
	ch := e.waiters[callID]
	e.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- approve:
	default:
	}
	return true
}
