package assistant

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

// TestOllamaLive talks to a real Ollama on this machine. It is opt-in because it needs
// a model pulled: VPS_ADMIN_OLLAMA_TEST=1 go test ./internal/assistant -run OllamaLive
// The model defaults to qwen3:1.7b and the address to http://127.0.0.1:11434.
func TestOllamaLive(t *testing.T) {
	if os.Getenv("VPS_ADMIN_OLLAMA_TEST") == "" {
		t.Skip("set VPS_ADMIN_OLLAMA_TEST=1 to run against a real Ollama")
	}
	base := os.Getenv("VPS_ADMIN_OLLAMA_URL")
	if base == "" {
		base = ollamaBase
	}
	model := os.Getenv("VPS_ADMIN_OLLAMA_MODEL")
	if model == "" {
		model = "qwen3:1.7b"
	}
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, err := secret.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Deps: Deps{Store: st}, Box: box}
	e.Init(context.Background())
	ctx := context.Background()
	st.SetSetting(ctx, providerName, ProviderOllama)
	st.SetSetting(ctx, ollamaURLKey, base)
	st.SetSetting(ctx, "assistant.model.ollama", model)

	providers := e.Providers(ctx)
	if len(providers) == 0 || !providers[0].Ready {
		t.Fatalf("Ollama is not ready: %+v", providers)
	}
	t.Logf("Ollama %s, model %s", providers[0].URL, providers[0].Model)

	var streamed strings.Builder
	ch, stop := e.Subscribe("")
	defer stop()
	go func() {
		for ev := range ch {
			if ev.Type == "text" {
				if d, ok := ev.Data.(map[string]any); ok {
					if s, ok := d["text"].(string); ok {
						streamed.WriteString(s)
					}
				}
			}
		}
	}()

	conv, err := e.NewConversation(ctx, "Call the list_apps tool, then tell me in one short sentence what it returned.", "live-test")
	if err != nil {
		t.Fatal(err)
	}
	ch2, stop2 := e.Subscribe(conv.ID)
	defer stop2()
	go func() {
		for ev := range ch2 {
			if ev.Type == "text" {
				if d, ok := ev.Data.(map[string]any); ok {
					if s, ok := d["text"].(string); ok {
						streamed.WriteString(s)
					}
				}
			}
		}
	}()

	deadline := time.Now().Add(4 * time.Minute)
	for {
		c, err := st.Conversation(ctx, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		if c.State == "awaiting" { // approve whatever the local model asked for
			calls, _ := st.ToolCalls(ctx, conv.ID)
			for _, call := range calls {
				if call.Status == "pending" {
					if _, err := e.Decide(ctx, call.ID, true, "live-test"); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		if c.State == "error" {
			t.Fatalf("conversation failed: %s", c.Error)
		}
		if c.State == "idle" && !e.Running(conv.ID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no answer after 4 minutes, state %s", c.State)
		}
		time.Sleep(200 * time.Millisecond)
	}

	msgs, _ := st.Messages(ctx, conv.ID)
	if len(msgs) < 2 {
		t.Fatalf("want at least two messages, got %d", len(msgs))
	}
	calls, _ := st.ToolCalls(ctx, conv.ID)
	found := false
	for _, c := range calls {
		if c.Name == "list_apps" && c.Status == "done" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the model did not run list_apps: %+v", calls)
	}
	if strings.TrimSpace(streamed.String()) == "" {
		t.Fatal("nothing was streamed to the panel")
	}
	t.Logf("answer: %s", msgs[len(msgs)-1].Content)
}
