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
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

const (
	maxSteps = 30 // model calls per user message, so a confused loop cannot run up a bill
	keyName  = "assistant.api_key"
)

// Models offered in the panel. Opus 5.5 is the default.
var Models = []struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}{
	{"claude-opus-5-5", "Claude Opus 5.5"},
	{"claude-sonnet-5-5", "Claude Sonnet 5.5"},
	{"claude-haiku-5-5", "Claude Haiku 5.5"},
	{"claude-fable-5-1", "Claude Fable 5.1"},
}

var (
	ErrNoKey   = errors.New("connect an Anthropic API key first")
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
	// BaseURL overrides the API endpoint, for tests.
	BaseURL string

	mu      sync.Mutex
	running map[string]context.CancelFunc
	subs    map[string]map[chan Event]struct{}
}

func (e *Engine) Init(ctx context.Context) {
	e.running = map[string]context.CancelFunc{}
	e.subs = map[string]map[chan Event]struct{}{}
	e.Store.InterruptedConversations(ctx)
}

type Settings struct {
	Configured bool   `json:"configured"`
	KeyHint    string `json:"keyHint,omitempty"`
	Model      string `json:"model"`
	Effort     string `json:"effort"`
	AutoRead   bool   `json:"autoRead"` // run read-only tools without asking
}

func (e *Engine) Settings(ctx context.Context) Settings {
	s := Settings{
		Model:    e.Store.Setting(ctx, "assistant.model", "claude-opus-5-5"),
		Effort:   e.Store.Setting(ctx, "assistant.effort", "high"),
		AutoRead: e.Store.Setting(ctx, "assistant.auto_read", "true") == "true",
	}
	if key := e.key(ctx); key != "" {
		s.Configured = true
		s.KeyHint = "sk-ant-…" + key[max(0, len(key)-4):]
	}
	return s
}

func (e *Engine) key(ctx context.Context) string {
	k, _ := e.Box.Open(e.Store.Setting(ctx, keyName, ""))
	return k
}

func (e *Engine) client(key string) anthropic.Client {
	opts := []option.RequestOption{option.WithAPIKey(key)}
	if e.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(e.BaseURL))
	}
	return anthropic.NewClient(opts...)
}

// SetKey checks the key against the API before saving it.
func (e *Engine) SetKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return e.Store.SetSetting(ctx, keyName, "")
	}
	c := e.client(key)
	if _, err := c.Models.List(ctx, anthropic.ModelListParams{Limit: anthropic.Int(1)}); err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
			return errors.New("Anthropic rejected this API key")
		}
		return fmt.Errorf("could not check the key: %w", err)
	}
	return e.Store.SetSetting(ctx, keyName, e.Box.Seal(key))
}

func (e *Engine) SetOptions(ctx context.Context, model, effort string, autoRead *bool) error {
	if model != "" {
		ok := false
		for _, m := range Models {
			ok = ok || m.ID == model
		}
		if !ok {
			return errors.New("unknown model")
		}
		e.Store.SetSetting(ctx, "assistant.model", model)
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
	if e.key(ctx) == "" {
		return nil, ErrNoKey
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
// denied: their results and the new text go in the same user turn.
func (e *Engine) Send(ctx context.Context, convID, text, deviceID string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("empty message")
	}
	key := e.key(ctx)
	if key == "" {
		return ErrNoKey
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
	go e.loop(convID, key)
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
	seq, err := e.Store.AppendMessage(ctx, convID, string(p.Role), string(raw))
	if err == nil {
		e.emit(convID, "message", map[string]any{"seq": seq, "role": p.Role, "content": json.RawMessage(raw)})
	}
	return err
}

// closeOpenBatch answers every tool call of the last assistant turn that has no result
// yet, so the history stays valid (each tool_use needs its tool_result).
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

// Decide approves or denies a pending call. Once the whole batch is answered, the
// results go back to the model.
func (e *Engine) Decide(ctx context.Context, callID string, approve bool, deviceID string) (*store.ToolCall, error) {
	tc, err := e.Store.ToolCall(ctx, callID)
	if err != nil {
		return nil, err
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
	key := e.key(ctx)
	if key == "" || !e.claim(convID) {
		return
	}
	if err := e.appendParam(ctx, convID, anthropic.NewBetaUserMessage(results...)); err != nil {
		e.release(convID)
		return
	}
	go e.loop(convID, key)
}

func (e *Engine) systemPrompt() string {
	h := e.Host
	return fmt.Sprintf(`You are the assistant built into VPS Admin, the administration panel of the Linux server %q (%s, kernel %s, %s, %d cores). The person writing to you is its administrator.

How you work:
- Investigate with the read-only tools before proposing anything, then explain what you found in plain words.
- Every change (a command that modifies something, a restart, a deploy) is shown to the administrator as a card they approve or deny. Ask for one change at a time, and say in one sentence what it does and what it affects (downtime, data, users) before asking.
- Commands run as the unprivileged vps-admin user, which can use Docker. sudo only works for systemctl start/stop/restart/reload and a few status commands. When something needs more, give the exact command for the administrator to run over SSH instead of trying workarounds.
- Some actions are blocked for you whatever the approval: deleting Docker volumes, changing SSH access or the firewall, reading keys or secrets, powering the server off. Say so and give the command instead.
- Never invent command output. If a tool fails, say so.
- Answer in the language the administrator writes in. Be concise: short paragraphs, commands in code blocks.`,
		h.Hostname, h.OS, h.Kernel, h.Arch, h.Cores)
}

// fallbackModels can use the server-side "default" fallback on a refusal.
var fallbackModels = map[string]bool{"claude-opus-5-5": true, "claude-sonnet-5-5": true, "claude-fable-5-1": true}

func (e *Engine) params(ctx context.Context, convID string) (anthropic.BetaMessageNewParams, error) {
	st := e.Settings(ctx)
	msgs, err := e.Store.Messages(ctx, convID)
	if err != nil {
		return anthropic.BetaMessageNewParams{}, err
	}
	history := make([]anthropic.BetaMessageParam, 0, len(msgs))
	for _, m := range msgs {
		// Sent back byte for byte: thinking blocks are only valid in an unchanged history.
		history = append(history, param.Override[anthropic.BetaMessageParam](json.RawMessage(m.Content)))
	}
	p := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(st.Model),
		MaxTokens: 64000,
		System:    []anthropic.BetaTextBlockParam{{Text: e.systemPrompt(), CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		Messages:  history,
		Tools:     toolParams(),
		Thinking: anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{
			Display: anthropic.BetaThinkingConfigAdaptiveDisplaySummarized,
		}},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(st.Effort)},
		CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
	}
	if fallbackModels[st.Model] {
		p.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}
	return p, nil
}

// loop calls the model until it answers without tools, or stops on calls that need
// approval. The caller has claimed the conversation.
func (e *Engine) loop(convID, key string) {
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.running[convID] = cancel
	e.mu.Unlock()
	defer func() {
		cancel()
		e.release(convID)
	}()
	e.setState(ctx, convID, "running", "")
	client := e.client(key)
	autoRead := e.Settings(ctx).AutoRead

	for step := 0; step < maxSteps; step++ {
		p, err := e.params(ctx, convID)
		if err != nil {
			e.fail(ctx, convID, err)
			return
		}
		msg, err := e.stream(ctx, client, convID, p)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				e.setState(context.Background(), convID, "idle", "stopped")
				return
			}
			e.fail(ctx, convID, err)
			return
		}
		if err := e.appendParam(ctx, convID, msg.ToParam()); err != nil {
			e.fail(ctx, convID, err)
			return
		}
		switch msg.StopReason {
		case anthropic.BetaStopReasonRefusal:
			e.setState(ctx, convID, "idle", "Claude declined this request ("+string(msg.StopDetails.Category)+").")
			return
		case anthropic.BetaStopReasonMaxTokens:
			e.setState(ctx, convID, "idle", "The answer hit the length limit. Ask Claude to continue.")
			return
		case anthropic.BetaStopReasonToolUse:
		default:
			e.setState(ctx, convID, "idle", "")
			return
		}

		msgs, _ := e.Store.Messages(ctx, convID)
		batch := msgs[len(msgs)-1].Seq
		var results []anthropic.BetaContentBlockParamUnion
		pending := false
		ord := 0
		for _, block := range msg.Content {
			tu, ok := block.AsAny().(anthropic.BetaToolUseBlock)
			if !ok {
				continue
			}
			ord++
			raw := tu.JSON.Input.Raw()
			tc := &store.ToolCall{ID: tu.ID, ConvID: convID, Batch: batch, Ord: ord, Name: tu.Name, Input: raw, CreatedAt: time.Now()}
			d, c, perr := parse(tu.Name, raw)
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
	e.setState(ctx, convID, "idle", fmt.Sprintf("Stopped after %d steps. Send a message to let Claude continue.", maxSteps))
}

func (e *Engine) fail(ctx context.Context, convID string, err error) {
	msg := err.Error()
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
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

// stream calls the model and forwards text and thinking as they arrive.
func (e *Engine) stream(ctx context.Context, client anthropic.Client, convID string, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	s := client.Beta.Messages.NewStreaming(ctx, p)
	msg := anthropic.BetaMessage{}
	for s.Next() {
		ev := s.Current()
		if err := msg.Accumulate(ev); err != nil {
			return nil, err
		}
		if d, ok := ev.AsAny().(anthropic.BetaRawContentBlockDeltaEvent); ok {
			switch delta := d.Delta.AsAny().(type) {
			case anthropic.BetaTextDelta:
				e.emit(convID, "text", map[string]any{"index": d.Index, "text": delta.Text})
			case anthropic.BetaThinkingDelta:
				e.emit(convID, "thinking", map[string]any{"index": d.Index, "text": delta.Thinking})
			}
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return &msg, nil
}

// Delete removes a conversation that is not running.
func (e *Engine) Delete(ctx context.Context, convID string) error {
	if e.Running(convID) {
		return ErrRunning
	}
	return e.Store.DeleteConversation(ctx, convID)
}
