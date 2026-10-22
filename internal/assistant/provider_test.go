package assistant

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
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
	e := &Engine{Deps: Deps{Store: st}, Box: box}
	e.Init(context.Background())
	return e
}

func tagsServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProvidersListOllamaModels(t *testing.T) {
	srv := tagsServer(t, `{"models":[{"name":"qwen3:4b","details":{"parameter_size":"4.0B"}},{"name":"llama3.2:3b","details":{"parameter_size":"3.2B"}}]}`)
	e := testEngine(t)
	ctx := context.Background()
	if err := e.Store.SetSetting(ctx, ollamaURLKey, srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := e.SetModel(ctx, ProviderOllama, "llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	p := e.Settings(ctx)
	if p.Provider != ProviderOllama || !p.Configured || p.Model != "llama3.2:3b" {
		t.Fatalf("ollama should be ready with the chosen model: %+v", p)
	}
	var ollama Provider
	for _, x := range p.Providers {
		if x.ID == ProviderOllama {
			ollama = x
		}
	}
	if len(ollama.Models) != 2 || ollama.Models[0].Label != "qwen3:4b (4.0B)" || !ollama.Active {
		t.Fatalf("installed models are not shown: %+v", ollama)
	}
	if _, err := e.resolve(ctx); err != nil {
		t.Fatalf("resolve should accept a configured Ollama: %v", err)
	}
}

func TestOllamaWithoutModelIsNotReady(t *testing.T) {
	srv := tagsServer(t, `{"models":[]}`)
	e := testEngine(t)
	ctx := context.Background()
	e.Store.SetSetting(ctx, ollamaURLKey, srv.URL)
	p := e.Settings(ctx)
	if p.Configured {
		t.Fatalf("Ollama without a model must not be ready: %+v", p)
	}
	if p.Hint == "" || !strings.Contains(p.Hint, "no model") {
		t.Fatalf("the panel should say a model is missing, got %q", p.Hint)
	}
	if _, err := e.resolve(ctx); err == nil {
		t.Fatal("a conversation must not start without a model")
	}
}

func TestOllamaDownKeepsTheInstallHint(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	e.Store.SetSetting(ctx, ollamaURLKey, "http://127.0.0.1:1")
	p := e.Settings(ctx)
	if p.Configured || !strings.Contains(p.Hint, "No Ollama") {
		t.Fatalf("a stopped Ollama should be explained: %+v", p)
	}
	for _, x := range p.Providers {
		if x.ID == ProviderOllama && x.Install == "" {
			t.Fatal("the install commands should be offered")
		}
	}
}

func TestSetOllamaURLChecksTheServer(t *testing.T) {
	srv := tagsServer(t, `{"models":[]}`)
	e := testEngine(t)
	ctx := context.Background()
	if err := e.SetOllamaURL(ctx, srv.URL+"/"); err != nil {
		t.Fatalf("a real Ollama should be accepted: %v", err)
	}
	if got := e.Store.Setting(ctx, ollamaURLKey, ""); got != srv.URL {
		t.Fatalf("address not saved: %q", got)
	}
	if err := e.SetOllamaURL(ctx, "http://127.0.0.1:1"); err == nil {
		t.Fatal("an address that does not answer should be refused")
	}
	if err := e.SetOllamaURL(ctx, "ftp://127.0.0.1"); err == nil {
		t.Fatal("a non http address should be refused")
	}
}

func TestKeysArePerProvider(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	if err := e.Store.SetSetting(ctx, geminiKey, e.Box.Seal("AIzaTest1234")); err != nil {
		t.Fatal(err)
	}
	if got := e.keyFor(ctx, ProviderGemini); got != "AIzaTest1234" {
		t.Fatalf("gemini key lost: %q", got)
	}
	if got := e.keyFor(ctx, ProviderClaudeCode); got != "" {
		t.Fatalf("the gemini key must not leak to Claude Code: %q", got)
	}
	p := e.Settings(ctx)
	if p.Provider != ProviderOllama {
		t.Fatalf("without choosing, the free local provider is the default: %s", p.Provider)
	}
	if p.KeyHint != "" {
		t.Fatalf("the key of another provider must not be shown: %q", p.KeyHint)
	}
	for _, x := range p.Providers {
		if x.ID == ProviderGemini && x.KeyHint != "AIza…1234" {
			t.Fatalf("gemini should read as connected: %+v", x)
		}
	}
}

// The paid API provider is gone: an installation that still points at it falls back to
// the subscription when a token is saved, and to Ollama otherwise.
func TestLegacyAnthropicChoiceMovesToTheSubscription(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	if err := e.Store.SetSetting(ctx, providerName, legacyAnthropic); err != nil {
		t.Fatal(err)
	}
	if got := e.ProviderID(ctx); got != ProviderOllama {
		t.Fatalf("without a token it should fall back to Ollama, got %s", got)
	}
	if err := e.SetKey(ctx, ProviderClaudeCode, "sk-ant-oat01-example"); err != nil {
		t.Fatal(err)
	}
	if got := e.ProviderID(ctx); got != ProviderClaudeCode {
		t.Fatalf("with a token it should move to the subscription, got %s", got)
	}
	if err := e.SetProvider(ctx, legacyAnthropic); err == nil {
		t.Fatal("the removed provider must not be selectable again")
	}
}

func TestModelChoiceIsPerProvider(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	if err := e.SetModel(ctx, ProviderClaudeCode, "claude-sonnet-5-5"); err != nil {
		t.Fatal(err)
	}
	if err := e.SetModel(ctx, ProviderGemini, "gemini-2.5-pro"); err != nil {
		t.Fatal(err)
	}
	if got := e.model(ctx, ProviderClaudeCode); got != "claude-sonnet-5-5" {
		t.Fatalf("Claude Code model lost: %s", got)
	}
	if got := e.model(ctx, ProviderGemini); got != "gemini-2.5-pro" {
		t.Fatalf("gemini model lost: %s", got)
	}
	if err := e.SetModel(ctx, ProviderClaudeCode, "gemini-2.5-pro"); err == nil {
		t.Fatal("a model of another provider must be refused for Claude Code")
	}
	if err := e.SetProvider(ctx, "openai"); err == nil {
		t.Fatal("unknown provider accepted")
	}
}
