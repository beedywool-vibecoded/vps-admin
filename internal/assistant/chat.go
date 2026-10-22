package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/beedywool/vps-admin/internal/store"
)

// The panel keeps every conversation in the Anthropic block format, because that is
// where it started. The OpenAI-compatible providers (Gemini, Ollama) are translated
// on the way in and out, so the approval cards, the audit and the stored history stay
// exactly the same whichever model answers.

// turnBlock is one content block of a model answer, normalized.
type turnBlock struct {
	Type  string // text | thinking | tool_use
	Text  string
	ID    string
	Name  string
	Input string // raw JSON arguments
}

// turn is one model answer: the message to store, the blocks to walk for tool calls,
// and why the model stopped.
type turn struct {
	raw    json.RawMessage
	blocks []turnBlock
	stop   string // tool_use | end_turn | max_tokens | refusal
	detail string // the refusal category, when the provider names one
}

type storedBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type storedMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (b storedBlock) blocks() []storedBlock {
	var list []storedBlock
	if json.Unmarshal(b.Content, &list) == nil {
		return list
	}
	return nil
}

func (b storedBlock) resultText() string {
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var parts []string
	for _, c := range b.blocks() {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n")
}

// oaMsg is one message of the OpenAI chat protocol.
type oaMsg struct {
	Role       string   `json:"role"`
	Content    string   `json:"content,omitempty"`
	ToolCalls  []oaCall `json:"tool_calls,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
}

type oaCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// toOpenAI rewrites a stored conversation for an OpenAI-compatible model. Thinking
// blocks are dropped: they carry Anthropic signatures that no other model accepts,
// and the panel still shows them when it replays the stored answer.
func toOpenAI(history []store.ConvMessage) []oaMsg {
	var out []oaMsg
	pushUser := func(text string) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == "user" && len(out[n-1].ToolCalls) == 0 {
			out[n-1].Content += "\n\n" + text
			return
		}
		out = append(out, oaMsg{Role: "user", Content: text})
	}
	pushTool := func(id, text string) {
		out = append(out, oaMsg{Role: "tool", ToolCallID: id, Content: text})
	}
	for _, m := range history {
		var sm storedMsg
		if err := json.Unmarshal([]byte(m.Content), &sm); err != nil {
			continue
		}
		var blocks []storedBlock
		var text string
		if json.Unmarshal(sm.Content, &blocks) != nil {
			// A plain string content: the format used before tools existed.
			if json.Unmarshal(sm.Content, &text) != nil {
				continue
			}
			blocks = nil
		}
		if m.Role == "user" {
			for _, b := range blocks {
				if b.Type == "tool_result" {
					pushTool(b.ToolUseID, b.resultText())
				}
			}
			var parts []string
			if text != "" {
				parts = append(parts, text)
			}
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
			pushUser(strings.Join(parts, "\n\n"))
			continue
		}
		var calls []oaCall
		for _, b := range blocks {
			switch b.Type {
			case "text":
				text += b.Text
			case "tool_use":
				c := oaCall{ID: b.ID, Type: "function"}
				c.Function.Name = b.Name
				c.Function.Arguments = string(b.Input)
				if c.Function.Arguments == "" || c.Function.Arguments == "null" {
					c.Function.Arguments = "{}"
				}
				calls = append(calls, c)
			}
		}
		msg := oaMsg{Role: "assistant", Content: text, ToolCalls: calls}
		if text == "" && len(calls) == 0 {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// openAITools flattens the panel tools into function definitions. The schema is kept
// plain (no strict mode, no additionalProperties): the Gemini compatible endpoint
// refuses what it does not know, and every input is validated again after parsing.
func openAITools() []oaTool {
	out := make([]oaTool, 0, len(defs))
	for _, d := range defs {
		t := oaTool{Type: "function"}
		t.Function.Name = d.name
		t.Function.Description = d.description
		props := map[string]any{}
		required := make([]string, 0, len(d.props))
		for k, v := range d.props {
			props[k] = v
			required = append(required, k)
		}
		sort.Strings(required)
		t.Function.Parameters = map[string]any{
			"type":       "object",
			"properties": props,
			"required":   required,
		}
		out = append(out, t)
	}
	return out
}

// providerError is an HTTP answer from a provider, kept so the panel can explain it.
type providerError struct {
	Status int
	Msg    string
}

func (p *providerError) Error() string { return p.Msg }

type chatRequest struct {
	Model     string   `json:"model"`
	Messages  []oaMsg  `json:"messages"`
	Tools     []oaTool `json:"tools,omitempty"`
	Stream    bool     `json:"stream"`
	MaxTokens int      `json:"max_tokens,omitempty"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// chatEndpoint is where a provider's OpenAI-compatible API lives.
func (e *Engine) chatEndpoint(ctx context.Context, id string) string {
	if id == ProviderGemini {
		if e.GeminiURL != "" {
			return strings.TrimRight(e.GeminiURL, "/")
		}
		return geminiBase
	}
	return e.ollamaEndpoint(ctx) + "/v1"
}

func (e *Engine) chatMaxTokens(id string) int {
	if id == ProviderOllama {
		return 4096 // a local model writing more than this is usually rambling
	}
	return 8192
}

// chat calls an OpenAI-compatible model and streams text and reasoning as they come.
func (e *Engine) chat(ctx context.Context, id, model, key string, history []store.ConvMessage,
	onText, onThinking func(string)) (*turn, error) {
	req := chatRequest{
		Model:     model,
		Messages:  append([]oaMsg{{Role: "system", Content: e.systemPrompt()}}, toOpenAI(history)...),
		Tools:     openAITools(),
		Stream:    true,
		MaxTokens: e.chatMaxTokens(id),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.chatEndpoint(ctx, id)+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 60 * time.Second}}
	resp, err := client.Do(httpReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		if id == ProviderOllama {
			return nil, fmt.Errorf("Ollama did not answer at %s: %w", e.chatEndpoint(ctx, id), err)
		}
		return nil, fmt.Errorf("could not reach %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &providerError{Status: resp.StatusCode, Msg: errorText(id, resp.StatusCode, b)}
	}

	var text strings.Builder
	acc := map[int]*oaCall{}
	var order []int
	finish := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	dataLines := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue // comments, event names, blank separators
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		dataLines++
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				break
			}
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // a keep-alive or a provider-specific line
		}
		if chunk.Error != nil {
			return nil, &providerError{Status: http.StatusBadGateway, Msg: chunk.Error.Message}
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				text.WriteString(c.Delta.Content)
				onText(c.Delta.Content)
			}
			if r := c.Delta.ReasoningContent + c.Delta.Reasoning; r != "" {
				onThinking(r)
			}
			for _, tc := range c.Delta.ToolCalls {
				call, ok := acc[tc.Index]
				if !ok {
					call = &oaCall{Type: "function"}
					acc[tc.Index] = call
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					call.ID = tc.ID
				}
				call.Function.Name += tc.Function.Name
				call.Function.Arguments += tc.Function.Arguments
			}
			if c.FinishReason != "" {
				finish = c.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, err
	}
	sort.Ints(order)
	var blocks []map[string]any
	t := &turn{}
	if s := text.String(); s != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": s})
		t.blocks = append(t.blocks, turnBlock{Type: "text", Text: s})
	}
	tools := 0
	for _, i := range order {
		c := acc[i]
		if c.Function.Name == "" {
			continue
		}
		if c.ID == "" {
			c.ID = "call_" + newID()[:16]
		}
		args := strings.TrimSpace(c.Function.Arguments)
		if args == "" || !json.Valid([]byte(args)) {
			args = "{}"
		}
		blocks = append(blocks, map[string]any{
			"type": "tool_use", "id": c.ID, "name": c.Function.Name, "input": json.RawMessage(args),
		})
		t.blocks = append(t.blocks, turnBlock{Type: "tool_use", ID: c.ID, Name: c.Function.Name, Input: args})
		tools++
	}
	if blocks == nil {
		return nil, fmt.Errorf("the model sent an empty answer (finish %q, %d stream events)", finish, dataLines)
	}
	param, err := json.Marshal(map[string]any{"role": "assistant", "content": blocks})
	if err != nil {
		return nil, err
	}
	t.raw = param
	switch {
	case tools > 0:
		t.stop = "tool_use"
	case finish == "length":
		t.stop = "max_tokens"
	default:
		t.stop = "end_turn"
	}
	return t, nil
}

// errorText turns a provider's error body into one readable sentence.
func errorText(id string, status int, body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(body, &payload)
	msg := strings.TrimSpace(payload.Error.Message)
	if msg == "" {
		msg = strings.TrimSpace(string(body))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return id + " rejected the API key."
	case http.StatusTooManyRequests:
		if id == ProviderGemini {
			return "The free Gemini quota is used up for today. Wait for it to reset or switch provider."
		}
		return "The model is busy (rate limited). Try again in a moment."
	case http.StatusNotFound:
		return id + " does not know this model: " + msg
	}
	if msg == "" {
		return fmt.Sprintf("%s answered %d", id, status)
	}
	return msg
}

// checkGemini makes the smallest possible request. A valid key that is out of quota
// is still a valid key, so only an authentication answer rejects it.
func (e *Engine) checkGemini(ctx context.Context, key string) error {
	body, _ := json.Marshal(chatRequest{
		Model:     "gemini-2.5-flash-lite",
		Messages:  []oaMsg{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.chatEndpoint(ctx, ProviderGemini)+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Gemini: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusTooManyRequests {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("Gemini rejected this API key")
	}
	return errors.New(errorText(ProviderGemini, resp.StatusCode, b))
}
