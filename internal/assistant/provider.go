package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
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
	// geminiAPI is the native API, where the model list lives; geminiBase is the
	// OpenAI-compatible door the chat goes through.
	geminiAPI  = "https://generativelanguage.googleapis.com/v1beta"
	geminiBase = geminiAPI + "/openai"
	ollamaBase = "http://127.0.0.1:11434"

	providerName = "assistant.provider"
	ollamaURLKey = "assistant.ollama_url"
	geminiKey    = "assistant.key.gemini"
	claudeKey    = "assistant.key.claude-code"
	// geminiRetiredKey holds the model names Google refused for a key, so the panel stops
	// offering them and no message asks for one again.
	geminiRetiredKey = "assistant.gemini.retired"
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

// Claude Code is asked for a family, not for a model name: `claude --model sonnet` is
// resolved by the CLI to whatever Sonnet the subscription serves today. A full name
// written here would name a model the account may not include, and every message would
// fail on it; the CLI's own aliases are the names it knows and keeps current.
var claudeModels = []Model{
	{ID: "sonnet", Label: "Sonnet", Hint: "Everyday work, the default"},
	{ID: "opus", Label: "Opus", Hint: "Hardest incidents, highest cost"},
	{ID: "haiku", Label: "Haiku", Hint: "Quick checks, cheapest"},
	{ID: "fable", Label: "Fable"},
}

// geminiFallback is shown before a key is saved, and whenever Google cannot answer the
// list of what that key may use. It is a preview, not the truth: the real list comes
// from models.list (see geminiModelsFor). Google limits a retired family to keys that
// already used it, so these are the names it serves to a key made today.
var geminiFallback = []Model{
	{ID: "gemini-3.8-flash", Label: "Gemini 3.8 Flash", Hint: "Fast, Google's current default"},
	{ID: "gemini-3.5-flash-lite", Label: "Gemini 3.5 Flash Lite", Hint: "Cheapest, largest quota"},
	{ID: "gemini-4-argon", Label: "Gemini 4 Argon", Hint: "Deepest reasoning, paid tier only"},
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
			Model:  e.model(ctx, ProviderGemini),
			Models: geminiFallback,
		},
		{
			ID: ProviderClaudeCode, Label: "Claude Code", Kind: "cli", Badge: "subscription",
			Note:     "The Claude Code CLI on this server, on your Pro or Max subscription: no key, no per-use bill.",
			NeedsKey: true, KeyHint: hint(e.keyFor(ctx, ProviderClaudeCode), "sk-ant"),
			Models: claudeModels, Model: e.model(ctx, ProviderClaudeCode),
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
	// Which models the key can use is Google's answer, not ours; the curated names only
	// stand in before a key exists or when Google cannot be reached.
	models, live, modelHint := e.geminiModelsFor(ctx)
	list[1].Models, list[1].Hint = models, modelHint
	switch {
	case !list[1].Ready:
		// Nothing connected yet: the preview names are what the free key will offer.
		list[1].Hint = ""
	case live && firstModel(models, list[1].Model) != list[1].Model:
		// Google stopped offering the saved model to this key: the first one takes over,
		// and the panel asks for a choice instead of failing every message. The choice is
		// recorded, so a message sent before the panel is opened uses it too.
		list[1].Model = firstModel(models, "")
		list[1].Hint = "The model that was saved is no longer offered to this key. Pick one below."
		e.Store.SetSetting(ctx, "assistant.model."+ProviderGemini, list[1].Model)
	}

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

// geminiList is one page of models.list, on the native Gemini API. Google renamed the
// capability field once (supportedGenerationMethods, then supportedActions), so both
// are read.
type geminiList struct {
	Models []struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		SupportedActions           []string `json:"supportedActions"`
	} `json:"models"`
	NextPageToken string `json:"nextPageToken"`
}

// geminiNotChat are models that do answer generateContent but cannot stand in for a
// chat turn: they generate images or speech, or embed text, and would fail on the first
// tool call. Gemini names them plainly enough to leave them out.
var geminiNotChat = []string{
	"embedding", "aqa", "imagen", "veo", "image", "tts", "audio", "live", "computer-use",
}

// Only the gemini family is offered: this panel works through tool calls, and the other
// chat models Google lists (Gemma) do not do them, so they would answer once and then
// ignore every request to look at the server.

// geminiModelsFor lists the models the saved key can use. live says whether that list
// came from Google: when it did not, the curated names are shown and hint says why.
func (e *Engine) geminiModelsFor(ctx context.Context) (models []Model, live bool, hint string) {
	key := e.keyFor(ctx, ProviderGemini)
	if key == "" {
		return geminiFallback, false, ""
	}
	tag := keyTag(key)
	e.gmu.Lock()
	if tag == e.gemKey {
		ttl := 10 * time.Minute
		if !e.gemLive {
			ttl = time.Minute // a failure is retried sooner than a success
		}
		if time.Since(e.gemAt) < ttl {
			models, live, hint := e.gemList, e.gemLive, e.gemHint
			e.gmu.Unlock()
			return withoutRetired(models, e.retiredGemini(ctx, key)), live, hint
		}
	}
	e.gmu.Unlock()
	models, live, hint = e.fetchGeminiModels(ctx, key)
	if !live {
		models = geminiFallback
	}
	models = withoutRetired(models, e.retiredGemini(ctx, key))
	e.gmu.Lock()
	e.gemKey, e.gemAt, e.gemList, e.gemLive, e.gemHint = tag, time.Now(), models, live, hint
	e.gmu.Unlock()
	return models, live, hint
}

// retiredGemini is what Google refused for this key, recorded as "<key tag>/<model>" so
// that a refusal is remembered for the key that got it: Google serves some families to
// the keys that already used them and to no others.
func (e *Engine) retiredGemini(ctx context.Context, key string) map[string]bool {
	out := map[string]bool{}
	tag := keyTag(key)
	if tag == "" {
		return out
	}
	for _, entry := range strings.Split(e.Store.Setting(ctx, geminiRetiredKey, ""), ",") {
		if t, id, ok := strings.Cut(strings.TrimSpace(entry), "/"); ok && t == tag && id != "" {
			out[id] = true
		}
	}
	return out
}

// retireGemini records that Google does not serve this model to this key.
func (e *Engine) retireGemini(ctx context.Context, key, model string) {
	tag := keyTag(key)
	if tag == "" || model == "" {
		return
	}
	retired := e.retiredGemini(ctx, key)
	if retired[model] {
		return
	}
	retired[model] = true
	entries := make([]string, 0, len(retired))
	for id := range retired {
		entries = append(entries, tag+"/"+id)
	}
	sort.Strings(entries)
	if err := e.Store.SetSetting(ctx, geminiRetiredKey, strings.Join(entries, ",")); err != nil {
		slog.Warn("gemini retired model not recorded", "model", model, "err", err)
	}
}

// withoutRetired drops what Google refused for this key.
func withoutRetired(models []Model, retired map[string]bool) []Model {
	if len(retired) == 0 {
		return models
	}
	out := make([]Model, 0, len(models))
	for _, m := range models {
		if !retired[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// geminiNextModel retires the model Google just refused and returns the next one it
// serves this key, recorded as the choice. Google's catalogue still lists a family it
// has closed to new keys, so the cached list is not trusted here: the refusal itself is
// the only proof that the name cannot be used.
func (e *Engine) geminiNextModel(ctx context.Context, failed string) (string, bool) {
	key := e.keyFor(ctx, ProviderGemini)
	if key == "" {
		return "", false
	}
	e.retireGemini(ctx, key, failed)
	retired := e.retiredGemini(ctx, key)
	models, live, hint := e.fetchGeminiModels(ctx, key)
	if !live {
		if cached, ok := e.lastGeminiModels(ctx, key); ok {
			models = cached
		} else {
			models = geminiFallback
		}
	}
	models = withoutRetired(models, retired)
	if live {
		e.gmu.Lock()
		e.gemKey, e.gemAt, e.gemList, e.gemLive, e.gemHint = keyTag(key), time.Now(), models, true, hint
		e.gmu.Unlock()
	}
	next := firstModel(models, "")
	if next == "" {
		return "", false
	}
	if err := e.Store.SetSetting(ctx, "assistant.model."+ProviderGemini, next); err != nil {
		slog.Warn("gemini model switch not recorded", "model", next, "err", err)
	}
	return next, true
}

func keyTag(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// lastGeminiModels is the list from the previous fetch, without asking Google again:
// sending a message must not wait on a round trip that may take seconds.
func (e *Engine) lastGeminiModels(ctx context.Context, key string) ([]Model, bool) {
	if key == "" {
		return nil, false
	}
	e.gmu.Lock()
	if keyTag(key) != e.gemKey || !e.gemLive || time.Since(e.gemAt) > 10*time.Minute {
		e.gmu.Unlock()
		return nil, false
	}
	models := e.gemList
	e.gmu.Unlock()
	return withoutRetired(models, e.retiredGemini(ctx, key)), true
}

// fetchGeminiModels asks Google which models this key may use. Listing needs the key:
// without one Google refuses, which is exactly why the curated list is the fallback.
func (e *Engine) fetchGeminiModels(ctx context.Context, key string) ([]Model, bool, string) {
	base := geminiAPI
	if e.GeminiModelsURL != "" {
		base = strings.TrimRight(e.GeminiModelsURL, "/")
	}
	client := &http.Client{Timeout: 4 * time.Second}
	var found []Model
	pageToken := ""
	for page := 0; page < 3; page++ {
		u := base + "/models?pageSize=1000"
		if pageToken != "" {
			u += "&pageToken=" + url.QueryEscape(pageToken)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, false, "Gemini's model list could not be requested."
		}
		// The key travels in a header: a URL is written to logs and proxies.
		req.Header.Set("x-goog-api-key", key)
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, false, "Gemini's model list was interrupted."
			}
			return nil, false, "Google did not answer the model list. Check this server's connection, then press Check again."
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, false, geminiListError(resp.StatusCode, body)
		}
		if readErr != nil {
			return nil, false, "Google's model list was cut short. Press Check again."
		}
		var list geminiList
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, false, "Google answered the model list in a shape this panel does not know."
		}
		for _, m := range list.Models {
			caps := append(m.SupportedGenerationMethods, m.SupportedActions...)
			if !hasString(caps, "generateContent") || !chatCapable(m.Name) {
				continue
			}
			id := strings.TrimPrefix(m.Name, "models/")
			if id == "" || !modelRe.MatchString(id) {
				continue
			}
			label := m.DisplayName
			if label == "" {
				label = id
			}
			found = append(found, Model{ID: id, Label: label})
		}
		pageToken = list.NextPageToken
		if pageToken == "" {
			break
		}
	}
	if len(found) == 0 {
		return nil, false, "This key may not use any Gemini chat model. Check that the key belongs to a project with the Gemini API enabled."
	}
	return orderGemini(found), true, ""
}

// orderGemini keeps the familiar names on top, with the hints written for them, and
// puts everything else the key can reach after, alphabetically.
func orderGemini(found []Model) []Model {
	known := map[string]bool{}
	out := make([]Model, 0, len(found))
	for _, m := range found {
		known[m.ID] = true
	}
	for _, c := range geminiFallback {
		if known[c.ID] {
			out = append(out, c)
		}
	}
	var rest []Model
	for _, m := range found {
		if own := findModel(geminiFallback, m.ID); own != nil {
			continue
		}
		if strings.Contains(m.ID, "preview") || strings.Contains(m.ID, "exp") {
			m.Hint = "Preview"
		}
		rest = append(rest, m)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].ID < rest[j].ID })
	return append(out, rest...)
}

func findModel(models []Model, id string) *Model {
	for i := range models {
		if models[i].ID == id {
			return &models[i]
		}
	}
	return nil
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func chatCapable(name string) bool {
	low := strings.ToLower(strings.TrimPrefix(name, "models/"))
	if !strings.HasPrefix(low, "gemini-") {
		return false
	}
	for _, s := range geminiNotChat {
		if strings.Contains(low, s) {
			return false
		}
	}
	return true
}

// geminiListError turns a refused models.list into one sentence the panel can show.
func geminiListError(status int, body []byte) string {
	msg := errorBodyMessage(body)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Gemini refused this API key when listing models. Save the key again."
	case http.StatusBadRequest:
		if strings.Contains(strings.ToLower(msg), "api key") {
			return "Gemini does not accept this API key. Save it again from AI Studio."
		}
	}
	if msg == "" {
		return fmt.Sprintf("Google answered %d when listing models.", status)
	}
	return "Google answered: " + msg
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
		def = geminiFallback[0].ID
	case ProviderClaudeCode:
		def = "sonnet"
	}
	got := e.Store.Setting(ctx, "assistant.model."+id, def)
	if id == ProviderClaudeCode {
		// An installation from before the aliases has a full name saved: keep its family.
		return claudeAlias(got)
	}
	return got
}

// claudeAlias turns a saved full model name into the family alias the CLI resolves.
func claudeAlias(name string) string {
	low := strings.ToLower(name)
	for _, m := range claudeModels {
		if strings.Contains(low, m.ID) {
			return m.ID
		}
	}
	return name
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
// choice. Claude Code is limited to the families the CLI knows; Gemini and Ollama accept
// what the provider reports.
func (e *Engine) SetModel(ctx context.Context, provider, model string) error {
	model = strings.TrimSpace(model)
	if provider == "" {
		provider = e.ProviderID(ctx)
	}
	switch provider {
	case ProviderClaudeCode:
		for _, m := range claudeModels {
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
