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
	// Keep tests off the network: the Gemini model list points at a closed port and the
	// Claude Code CLI at a fake.
	e := &Engine{Deps: Deps{Store: st}, Box: box, GeminiModelsURL: "http://127.0.0.1:1", ClaudeURL: claudeAnswers(t)}
	e.Init(context.Background())
	return e
}

// claudeAnswers is a fake Anthropic endpoint that accepts any token.
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
  {"name":"models/gemini-flash-lite-latest","displayName":"Gemini Flash Lite Latest","supportedGenerationMethods":["generateContent","countTokens"]},
  {"name":"models/gemini-3.8-flash","displayName":"Gemini 3.8 Flash","supportedGenerationMethods":["generateContent","countTokens"]},
  {"name":"models/gemini-2.5-flash","displayName":"Gemini 2.5 Flash","supportedGenerationMethods":["generateContent","countTokens"]},
  {"name":"models/gemini-2.5-pro","displayName":"Gemini 2.5 Pro","supportedActions":["generateContent"]},
  {"name":"models/gemini-2.5-flash-preview-09-2025","displayName":"Gemini 2.5 Flash Preview","supportedGenerationMethods":["generateContent"]},
  {"name":"models/gemini-embedding-001","displayName":"Gemini Embedding","supportedGenerationMethods":["embedContent"]},
  {"name":"models/gemini-2.5-flash-image","displayName":"Gemini 2.5 Flash Image","supportedGenerationMethods":["generateContent"]},
  {"name":"models/imagen-4.0-generate-001","displayName":"Imagen 4","supportedGenerationMethods":["predict"]},
  {"name":"models/gemma-3-27b-it","displayName":"Gemma 3 27B","supportedGenerationMethods":["generateContent"]}
]}`

// geminiListServer fakes the Gemini API. It fails the test if the key is sent in the URL.
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

// The model list comes from Google, not from the binary.
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
	want := []string{
		"gemini-flash-lite-latest", "gemini-3.8-flash", "gemini-2.5-flash",
		"gemini-2.5-flash-preview-09-2025", "gemini-2.5-pro",
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("the list should be Google's chat models, familiar ones first: %v", ids)
	}
	if gemini.Models[0].Hint != "Cheapest, largest quota, the default here" {
		t.Fatalf("the hint written for a known model should survive: %+v", gemini.Models[0])
	}
	if gemini.Models[3].Hint != "Preview" {
		t.Fatalf("a preview should say so: %+v", gemini.Models[3])
	}
	if gemini.Model != geminiFlashLiteLatest {
		t.Fatalf("a fresh key should start on the cheap model: %q", gemini.Model)
	}
	e.Settings(ctx)
	if calls != 1 {
		t.Fatalf("the list should be cached, Google was asked %d times", calls)
	}
}

// Without a key the curated list is shown and nothing is fetched.
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
		if len(x.Models) != len(geminiFallback) || x.Models[0].ID != geminiFlashLiteLatest {
			t.Fatalf("the preview list should stand in: %+v", x.Models)
		}
		if x.Hint != "" {
			t.Fatalf("nothing should be wrong yet, got %q", x.Hint)
		}
	}
}

// If Google is unreachable, the curated names stand in and the reason is shown.
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

func TestGeminiKeepsTheChosenModel(t *testing.T) {
	srv := geminiListServer(t, geminiListPayload, http.StatusOK, nil)
	e, ctx := geminiEngine(t, "AIzaGood", srv.URL)
	if err := e.SetModel(ctx, ProviderGemini, "gemini-2.5-pro"); err != nil {
		t.Fatal(err)
	}
	p := e.Settings(ctx)
	if p.Model != "gemini-2.5-pro" || p.Hint != "" {
		t.Fatalf("the chosen model should stand, without a warning: %+v", p)
	}
	for _, x := range p.Providers {
		if x.ID == ProviderGemini && x.Model != "gemini-2.5-pro" {
			t.Fatalf("the provider list should agree with the choice: %q", x.Model)
		}
	}
}

// A key without access to the lite model starts on the first one it can use.
func TestGeminiStartsOnTheFirstModelWithoutTheLiteOne(t *testing.T) {
	const without = `{"models":[
  {"name":"models/gemini-3.8-flash","displayName":"Gemini 3.8 Flash","supportedGenerationMethods":["generateContent"]},
  {"name":"models/gemini-2.5-pro","displayName":"Gemini 2.5 Pro","supportedGenerationMethods":["generateContent"]}
]}`
	srv := geminiListServer(t, without, http.StatusOK, nil)
	e, ctx := geminiEngine(t, "AIzaGood", srv.URL)
	p := e.Settings(ctx)
	if p.Model != "gemini-3.8-flash" {
		t.Fatalf("the first model this key can use should be selected: %q", p.Model)
	}
	if p.Hint != "" {
		t.Fatalf("nothing was retired, the panel should stay quiet: %q", p.Hint)
	}
	if got := e.Store.Setting(ctx, "assistant.model."+ProviderGemini, ""); got != "" {
		t.Fatalf("a default the person did not pick should not be recorded: %q", got)
	}
}

// A retired model is replaced, in the panel and on the send path.
func TestGeminiRetiredModelIsReplaced(t *testing.T) {
	srv := geminiListServer(t, geminiListPayload, http.StatusOK, nil)
	e, ctx := geminiEngine(t, "AIzaGood", srv.URL)
	if err := e.SetModel(ctx, ProviderGemini, "gemini-1.0-pro"); err != nil {
		t.Fatal(err)
	}
	p := e.Settings(ctx)
	if p.Model != geminiFlashLiteLatest {
		t.Fatalf("a retired model should give way to one the key can use: %q", p.Model)
	}
	if !strings.Contains(p.Hint, "no longer offered") {
		t.Fatalf("the panel should ask for a new choice, got %q", p.Hint)
	}
	if got := e.Store.Setting(ctx, "assistant.model."+ProviderGemini, ""); got != geminiFlashLiteLatest {
		t.Fatalf("the replacement should be recorded, the store still says %q", got)
	}
	target, err := e.resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if target.Model != geminiFlashLiteLatest {
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

// With no saved choice, the conversation uses the first installed Ollama model.
func TestOllamaWithoutAChoiceUsesTheFirstInstalledModel(t *testing.T) {
	srv := tagsServer(t, `{"models":[{"name":"qwen3:4b","details":{"parameter_size":"4.0B"}}]}`)
	e := testEngine(t)
	ctx := context.Background()
	e.Store.SetSetting(ctx, ollamaURLKey, srv.URL)
	p := e.Settings(ctx)
	if !p.Configured || p.Model != "qwen3:4b" {
		t.Fatalf("the only installed model should be shown and used: %+v", p)
	}
	if got := e.Store.Setting(ctx, "assistant.model.ollama", ""); got != "qwen3:4b" {
		t.Fatalf("the model the panel shows must be the one recorded, got %q", got)
	}
	tgt, err := e.resolve(ctx)
	if err != nil {
		t.Fatalf("a conversation must start on the model the panel shows: %v", err)
	}
	if tgt.Model != "qwen3:4b" {
		t.Fatalf("the conversation would ask for %q", tgt.Model)
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

// The persona is sent to every provider, and clearing it stops it.
func TestPersonaReachesEveryPrompt(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	if err := e.SetPersona(ctx, "  Speak like a pirate, briefly.  "); err != nil {
		t.Fatal(err)
	}
	if got := e.Settings(ctx).Persona; got != "Speak like a pirate, briefly." {
		t.Fatalf("the prompt should be trimmed: %q", got)
	}
	if p := e.systemPrompt(ctx); !strings.Contains(p, "Speak like a pirate, briefly.") {
		t.Fatal("the personality prompt is not in the system prompt")
	}
	if p := e.panelPrompt(ctx); !strings.Contains(p, "Speak like a pirate, briefly.") {
		t.Fatal("the personality prompt is not in the Claude Code prompt")
	}
	if err := e.SetPersona(ctx, strings.Repeat("x", maxPersona+1)); err == nil {
		t.Fatal("an overlong prompt should be refused")
	}
	if err := e.SetPersona(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if p := e.systemPrompt(ctx); strings.Contains(p, "Speak like a pirate") {
		t.Fatal("a cleared prompt should no longer be sent")
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

// A stopped Ollama must return [] rather than null.
func TestAProviderWithNoModelsSendsAnArray(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	e.Store.SetSetting(ctx, ollamaURLKey, "http://127.0.0.1:1")
	body, err := json.Marshal(e.Settings(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"models":null`) {
		t.Fatalf("a provider with no models must send [], not null: %s", body)
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

// The legacy API provider falls back to the subscription when a token exists, else Ollama.
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

// A saved full Claude model name maps to its family alias.
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
