package assistant

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

const maxSteps = 30 // per user message, caps the bill of a confused loop

var (
	ErrNoKey   = errors.New("connect an AI first, in the assistant settings")
	ErrRunning = errors.New("the assistant is still working on this conversation")
)

// Event is pushed to the panel over server-sent events.
type Event struct {
	Type string `json:"type"` // text | thinking | tool | state | error | message
	Data any    `json:"data"`
}

type Engine struct {
	Deps
	Box *secret.Box
	// Test-only endpoint overrides.
	BaseURL         string
	GeminiURL       string
	GeminiModelsURL string
	ClaudeURL       string

	mu      sync.Mutex
	running map[string]context.CancelFunc
	subs    map[string]map[chan Event]struct{}
	// waiters are Claude Code tool calls blocked on the admin's answer. Guarded by mu.
	waiters map[string]chan bool

	cmu   sync.Mutex
	cpath string
	cver  string
	cerr  error
	cat   time.Time

	pmu      sync.Mutex
	tagsKey  string
	tagsAt   time.Time
	tagsList []Model
	tagsOK   bool
	tagsHint string

	// Gemini's model list is cached longer: it only changes when Google ships models.
	gmu     sync.Mutex
	gemKey  string
	gemAt   time.Time
	gemList []Model
	gemLive bool
	gemHint string
}

func (e *Engine) Init(ctx context.Context) {
	e.running = map[string]context.CancelFunc{}
	e.subs = map[string]map[chan Event]struct{}{}
	e.waiters = map[string]chan bool{}
	e.Store.InterruptedConversations(ctx)
}

type Settings struct {
	Provider   string     `json:"provider"`
	Configured bool       `json:"configured"`
	KeyHint    string     `json:"keyHint,omitempty"`
	Model      string     `json:"model"`
	Effort     string     `json:"effort"`
	AutoRead   bool       `json:"autoRead"` // run read-only tools without asking
	Persona    string     `json:"persona"`  // added to the system prompt of every call
	Hint       string     `json:"hint,omitempty"`
	OllamaURL  string     `json:"ollamaUrl,omitempty"`
	Providers  []Provider `json:"providers"`
}

func (e *Engine) Settings(ctx context.Context) Settings {
	providers := e.Providers(ctx)
	active := e.ProviderID(ctx)
	s := Settings{
		Provider:  active,
		Effort:    e.Store.Setting(ctx, "assistant.effort", "high"),
		AutoRead:  e.Store.Setting(ctx, "assistant.auto_read", "true") == "true",
		Persona:   e.Store.Setting(ctx, personaKey, ""),
		OllamaURL: e.Store.Setting(ctx, ollamaURLKey, ollamaBase),
		Providers: providers,
	}
	for _, p := range providers {
		if p.ID == active {
			s.Model, s.Configured, s.KeyHint, s.Hint = p.Model, p.Ready, p.KeyHint, p.Hint
		}
	}
	return s
}

func (e *Engine) SetKey(ctx context.Context, provider, key string) error {
	key = strings.TrimSpace(key)
	name := e.keyName(provider)
	if name == "" {
		return errors.New("this provider needs no key")
	}
	if key == "" {
		return e.Store.SetSetting(ctx, name, "")
	}
	// Only an auth failure rejects a key; anything else saves it and the first message
	// reports the problem.
	switch provider {
	case ProviderGemini:
		if err := e.checkGemini(ctx, key); err != nil {
			if errors.Is(err, errBadKey) {
				return errors.New("Gemini rejected this API key")
			}
			slog.Warn("gemini key not verified, saved anyway", "err", err)
		}
	case ProviderClaudeCode:
		// An OAuth token for the CLI, not an API key: check the shape, then ask the CLI.
		if !strings.HasPrefix(key, "sk-ant-") {
			return errors.New("paste the token printed by claude setup-token (it starts with sk-ant-oat), or an Anthropic API key (sk-ant-api)")
		}
		if err := e.checkClaudeCode(ctx, key); err != nil {
			if errors.Is(err, errBadKey) {
				return errors.New("Claude Code rejected this token")
			}
			slog.Warn("claude token not verified, saved anyway", "err", err)
		}
	}
	return e.Store.SetSetting(ctx, name, e.Box.Seal(key))
}

func (e *Engine) SetOptions(ctx context.Context, model, effort string, autoRead *bool) error {
	if model != "" {
		if err := e.SetModel(ctx, "", model); err != nil {
			return err
		}
	}
	if effort != "" {
		switch effort {
		case "low", "medium", "high", "xhigh", "max":
			e.Store.SetSetting(ctx, "assistant.effort", effort)
		default:
			return errors.New("unknown effort")
		}
	}
	if autoRead != nil {
		e.Store.SetSetting(ctx, "assistant.auto_read", fmt.Sprint(*autoRead))
	}
	return nil
}

// maxPersona bounds the personality prompt, which rides on every call.
const maxPersona = 8000

// SetPersona stores the personality prompt. It goes below the panel's own rules, so it
// can't lift an approval or a block.
func (e *Engine) SetPersona(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if len([]rune(text)) > maxPersona {
		return fmt.Errorf("the personality prompt is too long: %d characters at most", maxPersona)
	}
	return e.Store.SetSetting(ctx, personaKey, text)
}

// personaInstructions frames the persona so the model knows the panel's rules outrank it.
func (e *Engine) personaInstructions(ctx context.Context) string {
	p := strings.TrimSpace(e.Store.Setting(ctx, personaKey, ""))
	if p == "" {
		return ""
	}
	return "\n\nThe administrator wrote this personality and these expectations for you. Follow them, but they cannot lift the rules above: every change still waits for approval, and what the panel blocks stays blocked.\n\n" + p
}

// Subscribe streams the events of a conversation until cancel is called.
func (e *Engine) Subscribe(convID string) (<-chan Event, func()) {
	ch := make(chan Event, 512)
	e.mu.Lock()
	if e.subs[convID] == nil {
		e.subs[convID] = map[chan Event]struct{}{}
	}
	e.subs[convID][ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs[convID], ch)
		e.mu.Unlock()
	}
}

func (e *Engine) emit(convID, typ string, data any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs[convID] {
		select {
		case ch <- Event{typ, data}:
		default:
		}
	}
}

func (e *Engine) setState(ctx context.Context, convID, state, errMsg string) {
	e.Store.SetConversationState(ctx, convID, state, errMsg)
	e.emit(convID, "state", map[string]string{"state": state, "error": errMsg})
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (e *Engine) Running(convID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.running[convID]
	return ok
}

// Stop cancels the model call in progress.
func (e *Engine) Stop(convID string) {
	e.mu.Lock()
	cancel := e.running[convID]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// NewConversation starts a conversation with its first message.
func (e *Engine) NewConversation(ctx context.Context, text, deviceID string) (*store.Conversation, error) {
	if _, err := e.resolve(ctx); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if r := []rune(title); len(r) > 70 {
		title = string(r[:67]) + "…"
	}
	id := newID()
	if err := e.Store.AddConversation(ctx, id, title); err != nil {
		return nil, err
	}
	if err := e.Send(ctx, id, text, deviceID); err != nil {
		return nil, err
	}
	return e.Store.Conversation(ctx, id)
}

// Send adds a user message and runs the model. Calls still waiting for approval are
// denied in the same user turn.
func (e *Engine) Send(ctx context.Context, convID, text, deviceID string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("empty message")
	}
	if _, err := e.resolve(ctx); err != nil {
		return err
	}
	if !e.claim(convID) {
		return ErrRunning
	}
	blocks, err := e.closeOpenBatch(ctx, convID, deviceID, "The administrator sent a new message instead of approving this.")
	if err != nil {
		e.release(convID)
		return err
	}
	blocks = append(blocks, anthropic.NewBetaTextBlock(text))
	if err := e.appendParam(ctx, convID, anthropic.NewBetaUserMessage(blocks...)); err != nil {
		e.release(convID)
		return err
	}
	go e.loop(convID)
	return nil
}

func (e *Engine) claim(convID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.running[convID]; ok {
		return false
	}
	e.running[convID] = func() {}
	return true
}

func (e *Engine) release(convID string) {
	e.mu.Lock()
	delete(e.running, convID)
	e.mu.Unlock()
}

func (e *Engine) appendParam(ctx context.Context, convID string, p anthropic.BetaMessageParam) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return e.appendRaw(ctx, convID, raw)
}

// appendRaw stores a message in Anthropic block format, whatever the provider.
func (e *Engine) appendRaw(ctx context.Context, convID string, raw json.RawMessage) error {
	var head struct {
		Role string `json:"role"`
	}
	json.Unmarshal(raw, &head)
	seq, err := e.Store.AppendMessage(ctx, convID, head.Role, string(raw))
	if err == nil {
		e.emit(convID, "message", map[string]any{"seq": seq, "role": head.Role, "content": json.RawMessage(raw)})
	}
	return err
}

// closeOpenBatch answers unanswered tool calls of the last turn: each tool_use needs a
// tool_result.
func (e *Engine) closeOpenBatch(ctx context.Context, convID, deviceID, reason string) ([]anthropic.BetaContentBlockParamUnion, error) {
	msgs, err := e.Store.Messages(ctx, convID)
	if err != nil || len(msgs) == 0 || msgs[len(msgs)-1].Role != "assistant" {
		return nil, err
	}
	batch := msgs[len(msgs)-1].Seq
	calls, err := e.Store.ToolCalls(ctx, convID)
	if err != nil {
		return nil, err
	}
	var out []anthropic.BetaContentBlockParamUnion
	for _, c := range calls {
		if c.Batch != batch {
			continue
		}
		if c.Status == "pending" || c.Status == "running" {
			e.Store.FinishToolCall(ctx, c.ID, "denied", reason, true, deviceID)
			c.Status, c.Result, c.IsError = "denied", reason, true
			e.emit(convID, "tool", c)
		}
		out = append(out, anthropic.NewBetaToolResultBlock(c.ID, c.Result, c.IsError))
	}
	return out, nil
}

// Decide approves or denies a pending call; once the batch is answered the results go
// back to the model.
func (e *Engine) Decide(ctx context.Context, callID string, approve bool, deviceID string) (*store.ToolCall, error) {
	tc, err := e.Store.ToolCall(ctx, callID)
	if err != nil {
		return nil, err
	}
	// Claude Code is blocked inside the tool call: hand it the answer instead.
	if e.waiting(callID) {
		ok, err := e.Store.ClaimToolCall(ctx, callID, deviceID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("this action was already answered")
		}
		e.signalDecision(callID, approve)
		if !approve {
			tc.Status, tc.Result, tc.IsError = "denied", "The administrator denied this action.", true
		}
		return tc, nil
	}
	if tc.Batch == 0 {
		return nil, errors.New("this action belongs to a Claude Code turn that already ended")
	}
	ok, err := e.Store.ClaimToolCall(ctx, callID, deviceID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("this action was already answered")
	}
	if approve {
		tc.Status = "running"
		e.emit(tc.ConvID, "tool", tc)
		d, c, _ := parse(tc.Name, tc.Input)
		res, err := e.execute(ctx, d, c)
		if err != nil {
			res, tc.IsError = truncate(strings.TrimSpace(res+"\n"+err.Error())), true
		} else {
			res = truncate(res)
		}
		tc.Status, tc.Result = "done", res
	} else {
		tc.Status, tc.Result, tc.IsError = "denied", "The administrator denied this action.", true
	}
	e.Store.FinishToolCall(ctx, callID, tc.Status, tc.Result, tc.IsError, deviceID)
	e.emit(tc.ConvID, "tool", tc)
	e.continueIfAnswered(ctx, tc.ConvID, tc.Batch)
	return tc, nil
}

func (e *Engine) continueIfAnswered(ctx context.Context, convID string, batch int64) {
	calls, err := e.Store.ToolCalls(ctx, convID)
	if err != nil {
		return
	}
	var results []anthropic.BetaContentBlockParamUnion
	for _, c := range calls {
		if c.Batch != batch {
			continue
		}
		if c.Status == "pending" || c.Status == "running" {
			return
		}
		results = append(results, anthropic.NewBetaToolResultBlock(c.ID, c.Result, c.IsError))
	}
	if _, err := e.resolve(ctx); err != nil {
		return
	}
	if !e.claim(convID) {
		return
	}
	if err := e.appendParam(ctx, convID, anthropic.NewBetaUserMessage(results...)); err != nil {
		e.release(convID)
		return
	}
	go e.loop(convID)
}

func (e *Engine) systemPrompt(ctx context.Context) string {
	h := e.Host
	return fmt.Sprintf(`You are the assistant built into VPS Admin, the administration panel of the Linux server %q (%s, kernel %s, %s, %d cores). The person writing to you is its administrator.

How you work:
- Investigate with the read-only tools before proposing anything, then explain what you found in plain words.
- Every change (a command that modifies something, a restart, a deploy) is shown to the administrator as a card they approve or deny. Ask for one change at a time, and say in one sentence what it does and what it affects (downtime, data, users) before asking.
- Commands run as the unprivileged vps-admin user, which can use Docker. sudo only works for systemctl start/stop/restart/reload and a few status commands. When something needs more, give the exact command for the administrator to run over SSH instead of trying workarounds.
- Some actions are blocked for you whatever the approval: deleting Docker volumes, changing SSH access or the firewall, reading keys or secrets, powering the server off. Say so and give the command instead.
- Never invent command output. If a tool fails, say so.
- Answer in the language the administrator writes in. Be concise: short paragraphs, commands in code blocks.`+
		e.personaInstructions(ctx),
		h.Hostname, h.OS, h.Kernel, h.Arch, h.Cores)
}

// loop calls the model until it answers without tools or needs approval. The caller has
// claimed the conversation.
func (e *Engine) loop(convID string) {
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.running[convID] = cancel
	e.mu.Unlock()
	defer func() {
		cancel()
		e.release(convID)
	}()
	e.setState(ctx, convID, "running", "")
	autoRead := e.Store.Setting(ctx, "assistant.auto_read", "true") == "true"

	// The Claude Code CLI runs tools itself through the MCP endpoint.
	if first, err := e.resolve(ctx); err != nil {
		e.fail(ctx, convID, err)
		return
	} else if first.Kind == "cli" {
		e.claudeLoop(ctx, convID, first)
		return
	}

	for step := 0; step < maxSteps; step++ {
		tgt, err := e.resolve(ctx)
		if err != nil {
			e.fail(ctx, convID, err)
			return
		}
		t, err := e.answerStep(ctx, convID, &tgt)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				e.setState(context.Background(), convID, "idle", "stopped")
				return
			}
			e.fail(ctx, convID, err)
			return
		}
		if err := e.appendRaw(ctx, convID, t.raw); err != nil {
			e.fail(ctx, convID, err)
			return
		}
		switch t.stop {
		case "refusal":
			e.setState(ctx, convID, "idle", "The model declined this request"+refusal(t.detail)+".")
			return
		case "max_tokens":
			e.setState(ctx, convID, "idle", "The answer hit the length limit. Ask it to continue.")
			return
		case "tool_use":
		default:
			e.setState(ctx, convID, "idle", "")
			return
		}

		msgs, _ := e.Store.Messages(ctx, convID)
		batch := msgs[len(msgs)-1].Seq
		var results []anthropic.BetaContentBlockParamUnion
		pending := false
		ord := 0
		for _, b := range t.blocks {
			if b.Type != "tool_use" {
				continue
			}
			ord++
			tc := &store.ToolCall{ID: b.ID, ConvID: convID, Batch: batch, Ord: ord, Name: b.Name, Input: b.Input, CreatedAt: time.Now()}
			d, c, perr := parse(b.Name, b.Input)
			var kind Kind
			if perr != nil {
				tc.Kind, tc.Status, tc.Result, tc.IsError = string(Blocked), "blocked", "INVALID_INPUT: "+perr.Error()+". Call the tool again with complete, valid arguments.", true
			} else {
				var why string
				kind, why = kindOf(d, c)
				tc.Kind, tc.Summary = string(kind), Display(d, c)
				switch {
				case kind == Blocked:
					tc.Status, tc.Result, tc.IsError = "blocked", "Blocked by the panel policy ("+why+"). Do not retry; give the administrator the command to run over SSH if it is really needed.", true
				case kind == Read && autoRead:
					tc.Status = "running"
				default:
					tc.Status = "pending"
					pending = true
				}
			}
			e.Store.AddToolCall(ctx, tc)
			e.emit(convID, "tool", tc)
			if tc.Status == "running" {
				res, err := e.execute(ctx, d, c)
				if err != nil {
					res, tc.IsError = strings.TrimSpace(res+"\n"+err.Error()), true
				}
				tc.Status, tc.Result = "done", truncate(res)
				e.Store.FinishToolCall(ctx, tc.ID, tc.Status, tc.Result, tc.IsError, "")
				e.emit(convID, "tool", tc)
			}
			results = append(results, anthropic.NewBetaToolResultBlock(tc.ID, tc.Result, tc.IsError))
		}
		if pending {
			e.setState(ctx, convID, "awaiting", "")
			return
		}
		if err := e.appendParam(ctx, convID, anthropic.NewBetaUserMessage(results...)); err != nil {
			e.fail(ctx, convID, err)
			return
		}
	}
	e.setState(ctx, convID, "idle", fmt.Sprintf("Stopped after %d steps. Send a message to let the assistant continue.", maxSteps))
}

func refusal(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

// claudeLoop runs one message through the Claude Code CLI, which calls the tools itself.
func (e *Engine) claudeLoop(ctx context.Context, convID string, tgt target) {
	prompt, err := e.lastUserText(ctx, convID)
	if err != nil {
		e.fail(ctx, convID, err)
		return
	}
	t, err := e.claudeTurn(ctx, convID, tgt, prompt,
		func(s string) { e.emit(convID, "text", map[string]any{"index": 1, "text": s}) },
		func(s string) { e.emit(convID, "thinking", map[string]any{"index": 0, "text": s}) })
	if err != nil {
		if errors.Is(err, errStopped) || errors.Is(ctx.Err(), context.Canceled) {
			e.setState(context.Background(), convID, "idle", "stopped")
			return
		}
		e.fail(ctx, convID, err)
		return
	}
	if err := e.appendRaw(ctx, convID, t.raw); err != nil {
		e.fail(ctx, convID, err)
		return
	}
	e.setState(ctx, convID, "idle", "")
}

// answerStep asks the active provider for one step. If Google refuses the model, it
// switches to the next one it serves, at most twice.
func (e *Engine) answerStep(ctx context.Context, convID string, tgt *target) (*turn, error) {
	for attempt := 0; ; attempt++ {
		t, err := e.answer(ctx, convID, *tgt)
		if err == nil || tgt.ID != ProviderGemini || !geminiModelGone(err) || attempt >= 2 {
			return t, err
		}
		next, ok := e.geminiNextModel(ctx, tgt.Model)
		if !ok {
			return t, err
		}
		slog.Warn("gemini model switched", "from", tgt.Model, "to", next)
		tgt.Model = next
	}
}

// answer asks the active provider for one step, streaming text and reasoning.
func (e *Engine) answer(ctx context.Context, convID string, tgt target) (*turn, error) {
	history, err := e.Store.Messages(ctx, convID)
	if err != nil {
		return nil, err
	}
	return e.chat(ctx, tgt.ID, tgt.Model, tgt.Key, history,
		func(s string) { e.emit(convID, "text", map[string]any{"index": 1, "text": s}) },
		func(s string) { e.emit(convID, "thinking", map[string]any{"index": 0, "text": s}) })
}

// target is the provider a model call goes to, resolved before each step.
type target struct {
	ID    string
	Kind  string
	Model string
	Key   string
}

func kindOfProvider(id string) string {
	if id == ProviderClaudeCode {
		return "cli"
	}
	return "openai"
}

// resolve picks the active provider without touching the network.
func (e *Engine) resolve(ctx context.Context) (target, error) {
	id := e.ProviderID(ctx)
	t := target{ID: id, Kind: kindOfProvider(id), Model: e.model(ctx, id), Key: e.keyFor(ctx, id)}
	if t.ID == ProviderOllama {
		if t.Model == "" {
			// Nothing chosen yet: use the first installed model, as settings shows.
			if models, ok, _ := e.ollamaModels(ctx, e.ollamaEndpoint(ctx), ""); ok {
				t.Model = e.pickOllama(ctx, models, "")
			}
		}
		if t.Model == "" {
			return target{}, fmt.Errorf("%w. Pick an Ollama model first.", ErrNoKey)
		}
		return t, nil
	}
	if t.ID == ProviderClaudeCode {
		if t.Key == "" {
			return target{}, fmt.Errorf("%w (Claude Code). Paste a token from claude setup-token.", ErrNoKey)
		}
		if _, _, err := e.claudeInfo(ctx); err != nil {
			return target{}, fmt.Errorf("Claude Code is not ready: %w", err)
		}
		return t, nil
	}
	if t.Key == "" {
		return target{}, fmt.Errorf("%w (%s).", ErrNoKey, t.ID)
	}
	if t.ID == ProviderGemini {
		// Skip a model Google retired, using only the cached list.
		if models, live := e.lastGeminiModels(ctx, t.Key); live {
			t.Model = firstModel(models, t.Model)
		}
	}
	return t, nil
}

func (e *Engine) fail(ctx context.Context, convID string, err error) {
	msg := err.Error()
	var pe *providerError
	var apiErr *anthropic.Error
	switch {
	case errors.As(err, &pe):
		msg = pe.Msg
	case errors.As(err, &apiErr):
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			msg = "Anthropic rejected the API key. Change it in the assistant settings."
		case http.StatusTooManyRequests:
			msg = "Rate limited by Anthropic. Wait a moment and send a message to continue."
		case 529:
			msg = "Anthropic is overloaded right now. Try again in a minute."
		default:
			msg = fmt.Sprintf("Anthropic API error %d: %s", apiErr.StatusCode, apiErr.Error())
		}
	}
	slog.Warn("assistant error", "conversation", convID, "err", err)
	e.setState(context.WithoutCancel(ctx), convID, "error", msg)
}

// Delete removes a conversation that is not running.
func (e *Engine) Delete(ctx context.Context, convID string) error {
	if e.Running(convID) {
		return ErrRunning
	}
	return e.Store.DeleteConversation(ctx, convID)
}
