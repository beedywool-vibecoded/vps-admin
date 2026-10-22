package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Providers the panel can talk to. Claude Code runs the official CLI on a subscription;
// Gemini and Ollama speak the OpenAI chat protocol, so one adapter covers free and local
// models. The paid Anthropic API provider was removed: premium means the subscription.
const (
	ProviderGemini = "gemini"
	ProviderOllama = "ollama"
	// ProviderClaudeCode runs the official CLI installed on the server, on the
	// administrator's Claude subscription instead of a billed API key.
	ProviderClaudeCode = "claude-code"

	// legacyAnthropic is the provider that used an Anthropic API key. It is only read, to
	// move an installation that still points at it to the subscription or to Ollama.
	legacyAnthropic = "anthropic"
)

const (
	// Gemini's OpenAI-compatible endpoint and the free tier's default model.
	geminiBase = "https://generativelanguage.googleapis.com/v1beta/openai"
	ollamaBase = "http://127.0.0.1:11434"

	providerName = "assistant.provider"
	ollamaURLKey = "assistant.ollama_url"
	geminiKey    = "assistant.key.gemini"
	claudeKey    = "assistant.key.claude-code"
)

const ollamaInstall = `docker run -d --name ollama --restart unless-stopped \
  -p 127.0.0.1:11434:11434 -v ollama:/root/.ollama ollama/ollama
docker exec ollama ollama pull qwen3:4b`

// Model is one choice in a provider.
type Model struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// Provider is what the panel shows for one AI: whether it is connected, which model
// is selected, and what to do when it is not ready.
type Provider struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Kind     string  `json:"kind"`  // anthropic | openai, which adapter calls it
	Badge    string  `json:"badge"` // local · free, online · free, subscription
	Note     string  `json:"note"`
	NeedsKey bool    `json:"needsKey"`
	KeyHint  string  `json:"keyHint,omitempty"`
	Ready    bool    `json:"ready"`
	Active   bool    `json:"active"`
	Hint     string  `json:"hint,omitempty"`
	Model    string  `json:"model"`
	Models   []Model `json:"models"`
	URL      string  `json:"url,omitempty"`     // Ollama only
	Install  string  `json:"install,omitempty"` // Ollama only, shown when it is not answering
}

var anthropicModels = []Model{
	{ID: "claude-opus-5-5", Label: "Claude Opus 5.5", Hint: "Hardest incidents, highest cost"},
	{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5", Hint: "Everyday work"},
	{ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5", Hint: "Quick checks, cheapest"},
	{ID: "claude-fable-5-1", Label: "Claude Fable 5.1"},
}

// Gemini names are a curated list: the free tier changes often, and an unknown model
// should read as "pick another one", not as a broken request.
var geminiModels = []Model{
	{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash", Hint: "Fast, balanced free quota"},
	{ID: "gemini-2.5-flash-lite", Label: "Gemini 2.5 Flash Lite", Hint: "Largest free quota"},
	{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro", Hint: "Best reasoning, smaller quota"},
}

var modelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,79}$`)

// Providers describes every AI the panel can use, in the order the sidebar shows them:
// the free ones first, the subscription last.
func (e *Engine) Providers(ctx context.Context) []Provider {
	active := e.ProviderID(ctx)
	list := []Provider{
		{
			ID: ProviderOllama, Label: "Ollama", Kind: "openai", Badge: "local · free",
			Note: "Runs on this server. Nothing leaves it, no account, no limit.",
			URL:  e.Store.Setting(ctx, ollamaURLKey, ollamaBase),
		},
		{
			ID: ProviderGemini, Label: "Gemini", Kind: "openai", Badge: "online · free",
			Note:     "Google's free tier. A key from AI Studio, no card needed.",
			NeedsKey: true, KeyHint: hint(e.keyFor(ctx, ProviderGemini), "AIza"),
			Models: geminiModels, Model: e.model(ctx, ProviderGemini),
		},
		{
			ID: ProviderClaudeCode, Label: "Claude Code", Kind: "cli", Badge: "subscription",
			Note:     "The Claude Code CLI on this server, on your Pro or Max subscription: no key, no per-use bill.",
			NeedsKey: true, KeyHint: hint(e.keyFor(ctx, ProviderClaudeCode), "sk-ant"),
			Models: anthropicModels, Model: e.model(ctx, ProviderClaudeCode),
		},
	}
	list[0].Models, list[0].Ready, list[0].Hint = e.ollamaModels(ctx, list[0].URL, e.model(ctx, ProviderOllama))
	if list[0].Ready {
		list[0].Model = firstModel(list[0].Models, e.model(ctx, ProviderOllama))
	}
	if !list[0].Ready && len(list[0].Models) == 0 {
		list[0].Install = ollamaInstall
	}
	list[1].Ready = list[1].KeyHint != ""

	// Claude Code needs the binary as well as a token, and says which one is missing.
	_, cliVersion, cliErr := e.claudeInfo(ctx)
	list[2].Ready = list[2].KeyHint != "" && cliErr == nil
	switch {
	case cliErr != nil:
		list[2].Hint = "The claude command is not installed on this server. Install it with the two commands below."
		list[2].Install = claudeInstall
	case list[2].KeyHint == "":
		list[2].Hint = "Paste a token from `claude setup-token`, run on your own computer."
	default:
		list[2].Note = "Claude Code " + cliVersion + " on this server, on your Pro or Max subscription."
	}
	for i := range list {
		list[i].Active = list[i].ID == active
	}
	return list
}

func firstModel(models []Model, want string) string {
	for _, m := range models {
		if m.ID == want {
			return want
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}

func hint(key, prefix string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return prefix + "…"
	}
	return prefix + "…" + key[len(key)-4:]
}

// ProviderID is the provider new messages go to. Anything unknown falls back to
// Ollama, the only one that needs no account.
func (e *Engine) ProviderID(ctx context.Context) string {
	switch id := e.Store.Setting(ctx, providerName, ""); id {
	case ProviderGemini, ProviderOllama, ProviderClaudeCode:
		return id
	case legacyAnthropic:
		// The API key provider is gone. An installation that still pointed at it moves to
		// the subscription when a token is already saved, and to Ollama otherwise.
		if e.keyFor(ctx, ProviderClaudeCode) != "" {
			return ProviderClaudeCode
		}
	}
	return ProviderOllama
}

func (e *Engine) model(ctx context.Context, id string) string {
	def := ""
	switch id {
	case ProviderGemini:
		def = "gemini-2.5-flash"
	case ProviderClaudeCode:
		def = "claude-sonnet-5-5"
	}
	return e.Store.Setting(ctx, "assistant.model."+id, def)
}

func (e *Engine) keyName(id string) string {
	switch id {
	case ProviderGemini:
		return geminiKey
	case ProviderClaudeCode:
		return claudeKey
	}
	return ""
}

func (e *Engine) keyFor(ctx context.Context, id string) string {
	name := e.keyName(id)
	if name == "" {
		return ""
	}
	k, _ := e.Box.Open(e.Store.Setting(ctx, name, ""))
	return k
}

// SetProvider switches the AI every new message goes to.
func (e *Engine) SetProvider(ctx context.Context, id string) error {
	switch id {
	case ProviderGemini, ProviderOllama, ProviderClaudeCode:
		return e.Store.SetSetting(ctx, providerName, id)
	}
	return errors.New("unknown provider")
}

func (e *Engine) ollamaEndpoint(ctx context.Context) string {
	raw := strings.TrimSpace(e.Store.Setting(ctx, ollamaURLKey, ollamaBase))
	u, err := parseEndpoint(raw)
	if err != nil {
		return ollamaBase
	}
	return u
}

// parseEndpoint accepts an http(s) URL without credentials, and nothing else: the
// panel is the one making the request.
func parseEndpoint(raw string) (string, error) {
	if raw == "" {
		return ollamaBase, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", errors.New("the address must be an http:// or https:// URL")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// SetOllamaURL points the panel at another Ollama, for a model that does not fit on
// this server. It is checked before being saved.
func (e *Engine) SetOllamaURL(ctx context.Context, raw string) error {
	base, err := parseEndpoint(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(base + "/api/tags")
	if err != nil {
		return fmt.Errorf("nothing answers at %s: %w", base, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d, not Ollama", base, resp.StatusCode)
	}
	return e.Store.SetSetting(ctx, ollamaURLKey, base)
}

type ollamaTags struct {
	Models []struct {
		Name    string `json:"name"`
		Details struct {
			Parameters string `json:"parameter_size"`
		} `json:"details"`
	} `json:"models"`
}

// ollamaModels lists installed models, and says plainly when Ollama is absent or has
// nothing to run yet. The answer is kept for a few seconds: the settings panel asks
// for it after every change, and a stopped Ollama would otherwise cost 4 s each time.
func (e *Engine) ollamaModels(ctx context.Context, base, want string) ([]Model, bool, string) {
	e.pmu.Lock()
	if base == e.tagsKey && time.Since(e.tagsAt) < 5*time.Second {
		models, ok, hint := e.tagsList, e.tagsOK, e.tagsHint
		e.pmu.Unlock()
		pick := firstModel(models, want)
		if pick == "" && len(models) > 0 {
			return models, false, "Pick one of the installed models."
		}
		return models, ok, hint
	}
	e.pmu.Unlock()
	models, ok, hint := e.fetchOllama(base)
	e.pmu.Lock()
	e.tagsKey, e.tagsAt, e.tagsList, e.tagsOK, e.tagsHint = base, time.Now(), models, ok, hint
	e.pmu.Unlock()
	if ok && firstModel(models, want) == "" {
		return models, false, "Pick one of the installed models."
	}
	return models, ok, hint
}

func (e *Engine) fetchOllama(base string) ([]Model, bool, string) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(base + "/api/tags")
	if err != nil {
		return nil, false, "No Ollama at " + base + ". Install it with the two commands below."
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Sprintf("The address answered %d, not Ollama.", resp.StatusCode)
	}
	var tags ollamaTags
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, false, "Ollama answered something unexpected."
	}
	models := make([]Model, 0, len(tags.Models))
	for _, m := range tags.Models {
		label := m.Name
		if m.Details.Parameters != "" {
			label += " (" + m.Details.Parameters + ")"
		}
		models = append(models, Model{ID: m.Name, Label: label})
	}
	if len(models) == 0 {
		return nil, false, "Ollama runs, but has no model yet. Pull one with the second command."
	}
	return models, true, ""
}

// SetModel records the model for one provider, so switching back and forth keeps the
// choice. Anthropic is limited to the models the panel knows the parameters of;
// Gemini and Ollama accept what the provider reports.
func (e *Engine) SetModel(ctx context.Context, provider, model string) error {
	model = strings.TrimSpace(model)
	if provider == "" {
		provider = e.ProviderID(ctx)
	}
	switch provider {
	case ProviderClaudeCode:
		for _, m := range anthropicModels {
			if m.ID == model {
				return e.Store.SetSetting(ctx, "assistant.model."+provider, model)
			}
		}
		return errors.New("unknown model")
	case ProviderGemini:
		if !modelRe.MatchString(model) {
			return errors.New("unknown model")
		}
	case ProviderOllama:
		if !modelRe.MatchString(model) {
			return errors.New("unknown model")
		}
	default:
		return errors.New("unknown provider")
	}
	return e.Store.SetSetting(ctx, "assistant.model."+provider, model)
}
