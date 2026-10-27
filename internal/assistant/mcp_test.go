package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/system"
)

// Token, tool list and policy of the MCP endpoint, without starting the CLI.
func TestMCPBridge(t *testing.T) {
	e := testEngine(t)
	e.Deps.DataDir = t.TempDir()
	e.Deps.Host = system.Host{Hostname: "test-box", OS: "Debian", Kernel: "6.1", Arch: "amd64", Cores: 2}
	if err := e.Store.AddConversation(context.Background(), "conv-1", "test"); err != nil {
		t.Fatal(err)
	}
	b, err := e.newBridge("conv-1")
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()

	call := func(token, body string) (int, map[string]any) {
		req, err := http.NewRequest(http.MethodPost, b.url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}

	if code, _ := call("wrong", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != http.StatusUnauthorized {
		t.Fatalf("a wrong token must be refused, got %d", code)
	}

	code, init := call(b.token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if code != http.StatusOK {
		t.Fatalf("initialize answered %d", code)
	}
	if res, _ := init["result"].(map[string]any); res["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize did not echo the protocol version: %+v", init)
	}

	_, list := call(b.token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools, _ := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) != len(defs) {
		t.Fatalf("expected %d tools, got %d", len(defs), len(tools))
	}
	first, _ := tools[0].(map[string]any)
	if first["name"] != defs[0].name {
		t.Fatalf("tools are not the panel's: %+v", first)
	}

	// A blocked command never runs, whoever approves it.
	_, blocked := call(b.token, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_command","arguments":{"command":"rm -rf /","purpose":"no"}}}`)
	res, _ := blocked["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("a blocked command must come back as an error: %+v", blocked)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Blocked by the panel policy") {
		t.Fatalf("the refusal should name the policy: %q", text)
	}

	// A read-only command runs at once and is stored like any other call.
	_, ran := call(b.token, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run_command","arguments":{"command":"uptime","purpose":"see the load"}}}`)
	out, _ := ran["result"].(map[string]any)
	if out["isError"] == true {
		t.Fatalf("uptime should have run: %#v", ran)
	}
	if s := out["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(s, "load average") {
		t.Fatalf("uptime output missing: %q", s)
	}
	calls, err := e.Store.ToolCalls(context.Background(), "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	// The blocked call is never stored: nothing ran, so there is nothing to show.
	if len(calls) != 1 || calls[0].Status != "done" || calls[0].Kind != string(Read) {
		t.Fatalf("the call was not recorded: %+v", calls)
	}
}

func TestClaudeCodeNeedsATokenAndTheBinary(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	if err := e.SetProvider(ctx, ProviderClaudeCode); err != nil {
		t.Fatal(err)
	}
	if _, err := e.resolve(ctx); err == nil {
		t.Fatal("Claude Code must not be ready without a token")
	}
	if err := e.SetKey(ctx, ProviderClaudeCode, "not-a-token"); err == nil {
		t.Fatal("a token that is neither setup-token nor an API key must be refused")
	}
	if err := e.SetKey(ctx, ProviderClaudeCode, "sk-ant-oat01-example"); err != nil {
		t.Fatal(err)
	}
	var provider Provider
	for _, p := range e.Providers(ctx) {
		if p.ID == ProviderClaudeCode {
			provider = p
		}
	}
	if provider.Label != "Claude Code" || provider.Badge != "subscription" || provider.KeyHint == "" {
		t.Fatalf("the provider is not described: %+v", provider)
	}
	// Without the binary installed, Ready must stay false.
	if _, _, err := e.claudeInfo(ctx); err != nil && provider.Ready {
		t.Fatalf("a missing claude binary must make the provider not ready: %+v", provider)
	}
}
