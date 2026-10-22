package assistant

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Nothing in these tests should reach Google or Anthropic: the model list points at a
	// closed port, and the Claude Code CLI, when it is installed, at a fake that accepts a
	// token. A test that needs its own endpoint sets it on the engine it builds.
	e := &Engine{Deps: Deps{Store: st}, Box: box, GeminiModelsURL: "http://127.0.0.1:1", ClaudeURL: claudeAnswers(t)}
	e.Init(context.Background())
	return e
}

// claudeAnswers is a fake Anthropic endpoint that accepts a token: one streamed text
// message, which is all the CLI needs to report a working credential.
func claudeAnswers(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "count_tokens") {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"input_tokens":1}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range textEvents("pong") {
			raw, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev["type"], raw)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const geminiListPayload = `{"models":[
  {"name":"models/gemini-2.5-flash","displayName":"Gemini 2.5 Flash","supportedGenerationMethods":["generateContent","countTokens"]},
  {"name":"models/gemini-2.5-pro","displayName":"Gemini 2.5 Pro","supportedActions":["generateContent"]},
  {"name":"models/gemini-2.5-flash-preview-09-2025","displayName":"Gemini 2.5 Flash Preview","supportedGenerationMethods":["generateContent"]},
  {"name":"models/gemini-embedding-001","displayName":"Gemini Embedding","supportedGenerationMethods":["embedContent"]},
  {"name":"models/gemini-2.5-flash-image","displayName":"Gemini 2.5 Flash Image","supportedGenerationMethods":["generateContent"]},
  {"name":"models/imagen-4.0-generate-001","displayName":"Imagen 4","supportedGenerationMethods":["predict"]},
  {"name":"models/gemma-3-27b-it","displayName":"Gemma 3 27B","supportedGenerationMethods":["generateContent"]}
]}`

// geminiListServer stands in for generativelanguage.googleapis.com. It refuses anything
// but the expected key, counts the calls, and fails the test if the key is sent in the
// URL instead of the header.
func geminiListServer(t *testing.T, payload string, status int, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("key") != "" {
			t.Errorf("the API key travelled in the URL: %s", r.URL.String())
		}
		if r.Header.Get("x-goog-api-key") != "AIzaGood" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key."}}`)
			return
		}
		if calls != nil {
			*calls++
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"code":500,"message":"backend error"}}`)
			return
		}
		io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func geminiEngine(t *testing.T, key, url string) (*Engine, context.Context) {
	t.Helper()
	e := testEngine(t)
	e.GeminiModelsURL = url
	ctx := context.Background()
	if err := e.Store.SetSetting(ctx, providerName, ProviderGemini); err != nil {
		t.Fatal(err)
	}
	if key != "" {
		if err := e.Store.SetSetting(ctx, geminiKey, e.Box.Seal(key)); err != nil {
			t.Fatal(err)
		}
	}
	return e, ctx
}

// What the panel offers is what Google says this key can use, not a list compiled into
// the binary months ago.
func TestGeminiModelsComeFromGoogle(t *testing.T) {
	calls := 0
	srv := geminiListServer(t, geminiListPayload, http.StatusOK, &calls)
	e, ctx := geminiEngine(t, "AIzaGood", srv.URL)
	p := e.Settings(ctx)
	if !p.Configured || p.Provider != ProviderGemini {
		t.Fatalf("a saved key should read as connected: %+v", p)
	}
	var gemini Provider
	for _, x := range p.Providers {
		if x.ID == ProviderGemini {
			gemini = x
		}
	}
	var ids []string
	for _, m := range gemini.Models {
		ids = append(ids, m.ID)
	}
	// Image models cannot chat, and Gemma cannot call the panel's tools.
	want := []string{"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.5-flash-preview-09-2025"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("the list should be Google's chat models, familiar ones first: %v", ids)
	}
	if gemini.Models[0].Hint != "Fast, balanced free quota" {
		t.Fatalf("the hint written for a known model should survive: %+v", gemini.Models[0])
	}
	if gemini.Models[2].Hint != "Preview" {
		t.Fatalf("a preview should say so: %+v", gemini.Models[2])
	}
	if gemini.Model != "gemini-2.5-flash" {
		t.Fatalf("the default model should stay selected: %q", gemini.Model)
	}
	// One round trip per key change, not one per panel refresh.
	e.Settings(ctx)
	if calls != 1 {
		t.Fatalf("the list should be cached, Google was asked %d times", calls)
	}
}

// Before a key exists the panel still shows what the free tier offers, and asks nobody.
func TestGeminiWithoutKeyShowsTheCuratedList(t *testing.T) {
	e, ctx := geminiEngine(t, "", "http://127.0.0.1:1")
	p := e.Settings(ctx)
	if p.Configured {
		t.Fatalf("without a key the provider is not connected: %+v", p)
	}
	for _, x := range p.Providers {
		if x.ID != ProviderGemini {
			continue
		}
		if len(x.Models) != len(geminiFallback) || x.Models[0].ID != "gemini-2.5-flash" {
			t.Fatalf("the preview list should stand in: %+v", x.Models)
		}
		if x.Hint != "" {
			t.Fatalf("nothing should be wrong yet, got %q", x.Hint)
		}
	}
}

// Google down or unreachable leaves the panel usable: the curated names stand in and the
// reason is shown, rather than an empty menu.
func TestGeminiListFailureKeepsTheCuratedList(t *testing.T) {
	srv := geminiListServer(t, "", http.StatusInternalServerError, nil)
	e, ctx := geminiEngine(t, "AIzaGood", srv.URL)
	p := e.Settings(ctx)
	if !strings.Contains(p.Hint, "backend error") {
		t.Fatalf("the panel should explain the failure, got %q", p.Hint)
	}
	for _, x := range p.Providers {
		if x.ID == ProviderGemini && len(x.Models) != len(geminiFallback) {
			t.Fatalf("the curated list should stand in: %+v", x.Models)
		}
	}
}

func TestGeminiRefusedKeyWhenListing(t *testing.T) {
	srv := geminiListServer(t, geminiListPayload, http.StatusOK, nil)
	e, ctx := geminiEngine(t, "AIzaWrong", srv.URL)
	p := e.Settings(ctx)
	if !strings.Contains(p.Hint, "API key") {
		t.Fatalf("a refused key should be named as such, got %q", p.Hint)
	}
}

// A model Google no longer offers must not brick every message: the first available one
// takes over, in the panel and on the send path.
func TestGeminiRetiredModelIsReplaced(t *testing.T) {
	srv := geminiListServer(t, geminiListPayload, http.StatusOK, nil)
	e, ctx := geminiEngine(t, "AIzaGood", srv.URL)
	if err := e.SetModel(ctx, ProviderGemini, "gemini-1.0-pro"); err != nil {
		t.Fatal(err)
	}
	p := e.Settings(ctx)
	if p.Model != "gemini-2.5-flash" {
		t.Fatalf("a retired model should give way to one the key can use: %q", p.Model)
	}
	if !strings.Contains(p.Hint, "no longer offered") {
		t.Fatalf("the panel should ask for a new choice, got %q", p.Hint)
	}
	if got := e.Store.Setting(ctx, "assistant.model."+ProviderGemini, ""); got != "gemini-2.5-flash" {
		t.Fatalf("the replacement should be recorded, the store still says %q", got)
	}
	target, err := e.resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if target.Model != "gemini-2.5-flash" {
		t.Fatalf("the conversation would still ask for the retired model: %q", target.Model)
	}
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
	if err := e.SetModel(ctx, ProviderClaudeCode, "sonnet"); err != nil {
		t.Fatal(err)
	}
	if err := e.SetModel(ctx, ProviderGemini, "gemini-2.5-pro"); err != nil {
		t.Fatal(err)
	}
	if got := e.model(ctx, ProviderClaudeCode); got != "sonnet" {
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

// Claude Code is asked for a family. An installation that saved a full name before the
// aliases keeps that family instead of sending a name its plan may not have.
func TestClaudeFullNameKeepsItsFamily(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	if err := e.Store.SetSetting(ctx, "assistant.model."+ProviderClaudeCode, "claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	if got := e.model(ctx, ProviderClaudeCode); got != "opus" {
		t.Fatalf("a saved full name should become its family: %q", got)
	}
	if err := e.Store.SetSetting(ctx, "assistant.model."+ProviderClaudeCode, "claude-sonnet-5-5"); err != nil {
		t.Fatal(err)
	}
	if got := e.model(ctx, ProviderClaudeCode); got != "sonnet" {
		t.Fatalf("a saved full name should become its family: %q", got)
	}
	if err := e.SetModel(ctx, ProviderClaudeCode, "claude-sonnet-5-5"); err == nil {
		t.Fatal("a full name is not one of the families on offer")
	}
}
