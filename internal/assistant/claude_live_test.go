package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"
)

// TestClaudeCodeLive drives the real `claude` binary against a fake Anthropic endpoint:
// the CLI parses the panel's MCP tools, calls one, and the call goes through the panel's
// policy, its approval card, and its audit: the whole subscription path, without an
// account.
//
//	VPS_ADMIN_CLAUDE_TEST=1 go test ./internal/assistant -run ClaudeCodeLive -v
func TestClaudeCodeLive(t *testing.T) {
	if os.Getenv("VPS_ADMIN_CLAUDE_TEST") == "" {
		t.Skip("set VPS_ADMIN_CLAUDE_TEST=1 to run against the real claude CLI")
	}
	if _, err := findClaude(""); err != nil {
		t.Skip("the claude binary is not installed")
	}

	t.Run("read tool runs without asking", func(t *testing.T) {
		fake := &fakeAnthropic{tool: "run_command", args: `{"command":"uptime","purpose":"see the load"}`}
		e, conv := claudeConversation(t, fake)
		waitIdle(t, e, conv)
		calls, err := e.Store.ToolCalls(context.Background(), conv)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 || calls[0].Status != "done" || calls[0].Kind != string(Read) {
			t.Fatalf("the read tool should have run and been recorded: %+v", calls)
		}
		if !strings.Contains(calls[0].Result, "load average") {
			t.Fatalf("the command output did not come back: %q", calls[0].Result)
		}
		if !fake.sawToolResult("load average") {
			t.Fatal("the CLI never sent the tool result back to the model")
		}
		assertAnswerStored(t, e, conv)
	})

	t.Run("a change waits for the approval", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "approved")
		fake := &fakeAnthropic{tool: "run_command",
			args: fmt.Sprintf(`{"command":"touch %s","purpose":"prove the approval works"}`, file)}
		e, conv := claudeConversation(t, fake)
		call := waitPending(t, e, conv)
		if call.Kind != string(Change) {
			t.Fatalf("touch must be a change: %+v", call)
		}
		if _, err := os.Stat(file); err == nil {
			t.Fatal("the command ran before the approval")
		}
		if _, err := e.Decide(context.Background(), call.ID, true, "test-device"); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, e, conv)
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("the approved command did not run: %v", err)
		}
		done, err := e.Store.ToolCall(context.Background(), call.ID)
		if err != nil || done.Status != "done" {
			t.Fatalf("the call should be done: %+v %v", done, err)
		}
		if !fake.sawToolResult("exit status") {
			t.Fatal("the CLI did not get the command result")
		}
		assertAnswerStored(t, e, conv)
	})

	t.Run("a denied change stays undone", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "denied")
		fake := &fakeAnthropic{tool: "run_command",
			args: fmt.Sprintf(`{"command":"touch %s","purpose":"must not run"}`, file)}
		e, conv := claudeConversation(t, fake)
		call := waitPending(t, e, conv)
		if _, err := e.Decide(context.Background(), call.ID, false, "test-device"); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, e, conv)
		if _, err := os.Stat(file); err == nil {
			t.Fatal("a denied command ran anyway")
		}
		done, _ := e.Store.ToolCall(context.Background(), call.ID)
		if done == nil || done.Status != "denied" {
			t.Fatalf("the call should be denied: %+v", done)
		}
		if !fake.sawToolResult("denied this action") {
			t.Fatal("the refusal never reached the model")
		}
	})
}

// fakeAnthropic answers like the Messages API: one tool call, then the final text once
// the tool result is in the conversation.
type fakeAnthropic struct {
	tool string
	args string

	mu   sync.Mutex
	body []string
}

func (f *fakeAnthropic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	f.mu.Lock()
	f.body = append(f.body, body)
	f.mu.Unlock()

	if strings.Contains(r.URL.Path, "count_tokens") {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"input_tokens":12}`)
		return
	}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	json.Unmarshal(raw, &req)

	// The CLI asks for a conversation title before the real turn; answer in plain text.
	wantsTool := f.tool != "" &&
		strings.Contains(body, "mcp__vps__") &&
		!strings.Contains(body, "tool_result") &&
		!strings.Contains(body, "Write the title")
	if wantsTool {
		f.writeSSE(w, req.Model, toolUseEvents("mcp__vps__"+f.tool, f.args))
		return
	}
	f.writeSSE(w, req.Model, textEvents("C'est fait, tout va bien."))
}

func (f *fakeAnthropic) sawToolResult(needle string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.body {
		if strings.Contains(b, "tool_result") && strings.Contains(b, needle) {
			return true
		}
	}
	return false
}

func (f *fakeAnthropic) writeSSE(w http.ResponseWriter, model string, events []map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, ev := range events {
		raw, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev["type"], raw)
	}
}

func startEvent(model string) map[string]any {
	return map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_fake", "type": "message", "role": "assistant", "model": model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 11, "output_tokens": 1},
	}}
}

func toolUseEvents(name, args string) []map[string]any {
	return []map[string]any{
		startEvent("claude-sonnet-5-5"),
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{
			"type": "tool_use", "id": "toolu_fake", "name": name, "input": map[string]any{}}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]any{
			"type": "input_json_delta", "partial_json": args}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": 9}},
		{"type": "message_stop"},
	}
}

func textEvents(text string) []map[string]any {
	return []map[string]any{
		startEvent("claude-sonnet-5-5"),
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": 7}},
		{"type": "message_stop"},
	}
}

// claudeConversation builds an engine on the subscription provider, pointed at the fake
// endpoint, and sends one message.
func claudeConversation(t *testing.T, fake *fakeAnthropic) (*Engine, string) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secret.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Deps: Deps{Store: st, DataDir: dir, Host: system.Host{Hostname: "test-box", OS: "Debian", Kernel: "6.1", Arch: "amd64", Cores: 2}}, Box: box, ClaudeURL: srv.URL}
	e.Init(context.Background())
	ctx := context.Background()
	st.SetSetting(ctx, providerName, ProviderClaudeCode)
	st.SetSetting(ctx, "assistant.model.claude-code", "claude-sonnet-5-5")
	if err := e.SetKey(ctx, ProviderClaudeCode, "sk-ant-oat01-test-token"); err != nil {
		t.Fatal(err)
	}
	conv, err := e.NewConversation(ctx, "Regarde le serveur et dis-moi ce que tu vois.", "test-device")
	if err != nil {
		t.Fatal(err)
	}
	return e, conv.ID
}

// waitIdle waits for the turn to start, then for it to end: a conversation is idle when
// its message is stored, before the model is even called.
func waitIdle(t *testing.T, e *Engine, convID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for !e.Running(convID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	for e.Running(convID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	c, err := e.Store.Conversation(context.Background(), convID)
	if err != nil {
		t.Fatal(err)
	}
	switch c.State {
	case "idle":
	case "error":
		t.Fatalf("the turn failed: %s", c.Error)
	default:
		t.Fatalf("the turn did not finish: %s", c.State)
	}
}

func waitPending(t *testing.T, e *Engine, convID string) *store.ToolCall {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		calls, _ := e.Store.ToolCalls(context.Background(), convID)
		for i := range calls {
			if calls[i].Status == "pending" && e.waiting(calls[i].ID) {
				return &calls[i]
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no tool call ever asked for an approval")
	return nil
}

func assertAnswerStored(t *testing.T, e *Engine, convID string) {
	t.Helper()
	msgs, err := e.Store.Messages(context.Background(), convID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("nothing was stored")
	}
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content, "tout va bien") {
		t.Fatalf("the answer was not stored: %s %s", last.Role, last.Content)
	}
}
