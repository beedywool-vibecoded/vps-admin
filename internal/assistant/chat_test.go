package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

func msg(t *testing.T, role, content string) store.ConvMessage {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		t.Fatalf("bad test content: %v", err)
	}
	return store.ConvMessage{Role: role, Content: content}
}

// The stored history is the Anthropic format; the OpenAI providers must see tool
// results as tool messages and must not receive Anthropic thinking blocks.
func TestToOpenAI(t *testing.T) {
	history := []store.ConvMessage{
		msg(t, "user", `{"role":"user","content":[{"type":"text","text":"List the apps"}]}`),
		msg(t, "assistant", `{"role":"assistant","content":[{"type":"thinking","thinking":"internal","signature":"sig"},{"type":"text","text":"Checking."},{"type":"tool_use","id":"tu_1","name":"list_apps","input":{}}]}`),
		msg(t, "user", `{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"[]","is_error":false},{"type":"text","text":"and then?"}]}`),
	}
	got := toOpenAI(history)
	if len(got) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[0].Content != "List the apps" {
		t.Fatalf("user message not kept: %+v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Content != "Checking." || len(got[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool call not translated: %+v", got[1])
	}
	if got[1].ToolCalls[0].ID != "tu_1" || got[1].ToolCalls[0].Function.Name != "list_apps" || got[1].ToolCalls[0].Function.Arguments != "{}" {
		t.Fatalf("bad tool call: %+v", got[1].ToolCalls[0])
	}
	if got[2].Role != "tool" || got[2].ToolCallID != "tu_1" || got[2].Content != "[]" {
		t.Fatalf("tool result not translated: %+v", got[2])
	}
	if got[3].Role != "user" || got[3].Content != "and then?" {
		t.Fatalf("text after a tool result lost: %+v", got[3])
	}
	for _, m := range got {
		if strings.Contains(m.Content, "internal") {
			t.Fatal("thinking block leaked to another provider")
		}
	}
}

// The OpenAI shape has no required fields for our tools; the panel validates again.
func TestOpenAIToolsArePlain(t *testing.T) {
	tools := openAITools()
	if len(tools) != len(defs) {
		t.Fatalf("want %d tools, got %d", len(defs), len(tools))
	}
	for _, tl := range tools {
		if tl.Type != "function" || tl.Function.Name == "" || tl.Function.Parameters["type"] != "object" {
			t.Fatalf("bad tool definition: %+v", tl)
		}
		if _, ok := tl.Function.Parameters["additionalProperties"]; ok {
			t.Fatalf("additionalProperties is refused by strict compatible endpoints: %s", tl.Function.Name)
		}
	}
}

func sse(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// Text and tool arguments arrive in pieces; both must be reassembled.
func TestChatReassemblesStream(t *testing.T) {
	var body chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing key: %q", got)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		sse(w,
			`{"choices":[{"delta":{"content":"Look"}}]}`,
			`{"choices":[{"delta":{"content":"ing."}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"list_apps","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig-1"}}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		)
	}))
	defer srv.Close()

	e := &Engine{GeminiURL: srv.URL}
	var text, thinking strings.Builder
	turn, err := e.chat(context.Background(), ProviderGemini, "gemini-2.5-flash", "test-key", nil,
		func(s string) { text.WriteString(s) }, func(s string) { thinking.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "Looking." {
		t.Fatalf("streamed text lost: %q", text.String())
	}
	if turn.stop != "tool_use" || len(turn.blocks) != 2 || turn.blocks[1].Name != "list_apps" {
		t.Fatalf("bad turn: %+v", turn)
	}
	if body.Model != "gemini-2.5-flash" || body.Messages[0].Role != "system" || len(body.Tools) != len(defs) {
		t.Fatalf("bad request: %+v", body)
	}
	var param struct {
		Role    string `json:"role"`
		Content []struct {
			Type             string          `json:"type"`
			Text             string          `json:"text"`
			ID               string          `json:"id"`
			Name             string          `json:"name"`
			Input            json.RawMessage `json:"input"`
			ThoughtSignature string          `json:"thought_signature"`
		} `json:"content"`
	}
	if err := json.Unmarshal(turn.raw, &param); err != nil {
		t.Fatalf("stored answer is not valid JSON: %v", err)
	}
	if param.Role != "assistant" || len(param.Content) != 2 || param.Content[1].Type != "tool_use" || string(param.Content[1].Input) != "{}" {
		t.Fatalf("stored answer has the wrong shape: %s", turn.raw)
	}
	// Google checks the signature on the way back in, so the call keeps it.
	if param.Content[1].ThoughtSignature != "sig-1" || turn.blocks[1].ThoughtSignature != "sig-1" {
		t.Fatalf("the thought signature of the call was lost: %s", turn.raw)
	}
}

// Gemini answers a later request that drops the signature of a function call with a 400
// about the missing thought signature, so the stored call has to carry it back.
func TestThoughtSignatureIsSentBackWithTheCall(t *testing.T) {
	history := []store.ConvMessage{
		msg(t, "user", `{"role":"user","content":[{"type":"text","text":"How much space is left?"}]}`),
		msg(t, "assistant", `{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"server_overview","input":{},"thought_signature":"sig-1"}]}`),
		msg(t, "user", `{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"{}","is_error":false}]}`),
	}
	got := toOpenAI(history)
	if len(got) != 3 || len(got[1].ToolCalls) != 1 {
		t.Fatalf("bad translation: %+v", got)
	}
	body, err := json.Marshal(got[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"extra_content":{"google":{"thought_signature":"sig-1"}}`) {
		t.Fatalf("the call goes back without its signature: %s", body)
	}

	// A provider that never sent a signature must not see the field at all.
	plain := toOpenAI([]store.ConvMessage{
		msg(t, "assistant", `{"role":"assistant","content":[{"type":"tool_use","id":"call_2","name":"list_apps","input":{}}]}`),
	})
	out, err := json.Marshal(plain[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "extra_content") {
		t.Fatalf("an Ollama call should carry no Google field: %s", out)
	}
}

func TestChatExplainsRejectedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"API key not valid"}}`)
	}))
	defer srv.Close()
	e := &Engine{GeminiURL: srv.URL}
	_, err := e.chat(context.Background(), ProviderGemini, "gemini-2.5-flash", "bad", nil, func(string) {}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("want a rejected-key message, got %v", err)
	}
}

func TestChatExplainsExhaustedQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"quota"}}`)
	}))
	defer srv.Close()
	e := &Engine{GeminiURL: srv.URL}
	_, err := e.chat(context.Background(), ProviderGemini, "gemini-2.5-flash", "k", nil, func(string) {}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("want a quota message, got %v", err)
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, bad := range []string{"ftp://x", "http://user:pw@host", "not a url", "http://"} {
		if _, err := parseEndpoint(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if u, err := parseEndpoint("http://127.0.0.1:11434/"); err != nil || u != "http://127.0.0.1:11434" {
		t.Fatalf("loopback endpoint refused: %q %v", u, err)
	}
}

// The whole loop with a free online provider: a tool call is executed by the panel,
// its result goes back to the model, and the answer is stored in the same format
// whichever provider wrote it.
func TestGeminiToolLoop(t *testing.T) {
	var calls int
	var second chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		calls++
		if calls == 1 {
			sse(w,
				`{"choices":[{"delta":{"content":"I will check."}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"list_apps","arguments":"{}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			)
			return
		}
		second = req
		sse(w,
			`{"choices":[{"delta":{"content":"No app is deployed."}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		)
	}))
	defer srv.Close()

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
	e := &Engine{Deps: Deps{Store: st}, Box: box, GeminiURL: srv.URL}
	e.Init(context.Background())
	ctx := context.Background()
	st.SetSetting(ctx, providerName, ProviderGemini)
	st.SetSetting(ctx, geminiKey, box.Seal("test-key"))

	conv, err := e.NewConversation(ctx, "What is deployed?", "test-device")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var msgs []store.ConvMessage
	for {
		c, err := st.Conversation(ctx, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		msgs, err = st.Messages(ctx, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		if c.State == "error" {
			t.Fatalf("conversation failed: %s", c.Error)
		}
		if len(msgs) >= 4 && c.State == "idle" && !e.Running(conv.ID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop did not finish, state %s with %d messages", c.State, len(msgs))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(msgs) != 4 {
		t.Fatalf("want 4 stored messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[1].Content, `"type":"tool_use"`) || !strings.Contains(msgs[2].Content, `"tool_use_id":"call_1"`) {
		t.Fatalf("tool exchange not stored in the block format:\n%s\n%s", msgs[1].Content, msgs[2].Content)
	}
	if !strings.Contains(msgs[3].Content, "No app is deployed.") {
		t.Fatalf("final answer not stored: %s", msgs[3].Content)
	}
	found := false
	for _, m := range second.Messages {
		if m.Role == "tool" && m.ToolCallID == "call_1" && m.Content == "[]" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the tool result was not sent back to the model: %+v", second.Messages)
	}
	all, _ := st.ToolCalls(ctx, conv.ID)
	if len(all) != 1 || all[0].Status != "done" || all[0].Kind != "read" {
		t.Fatalf("tool call not recorded: %+v", all)
	}
}

// Google's catalogue still lists a family it has closed to keys made today, so the name
// only fails when a message is sent with it. The next model it does serve takes over, the
// switch is recorded, and the refused name is never asked for again.
func TestGeminiRefusedModelAtSendIsReplaced(t *testing.T) {
	const retired = "gemini-2.5-flash"
	var mu sync.Mutex
	asked := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, geminiListPayload)
		case "/chat/completions":
			var req chatRequest
			json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			asked[req.Model]++
			mu.Unlock()
			if req.Model == retired {
				// Google's OpenAI-compatible door answers an array, which the panel must read
				// rather than show.
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `[{"error":{"code":404,"message":"This model models/gemini-2.5-flash is no longer available to new users. Please update your code to use models/gemini-3.8-flash for the latest features and improvements."}}]`)
				return
			}
			sse(w,
				`{"choices":[{"delta":{"content":"Nothing is deployed."}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

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
	ctx := context.Background()
	e := &Engine{Deps: Deps{Store: st}, Box: box, GeminiURL: srv.URL, GeminiModelsURL: srv.URL}
	e.Init(ctx)
	st.SetSetting(ctx, providerName, ProviderGemini)
	st.SetSetting(ctx, geminiKey, box.Seal("AIzaGood"))
	st.SetSetting(ctx, "assistant.model."+ProviderGemini, retired)

	conv, err := e.NewConversation(ctx, "Is anything deployed?", "test-device")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var msgs []store.ConvMessage
	for {
		c, err := st.Conversation(ctx, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		msgs, _ = st.Messages(ctx, conv.ID)
		if c.State == "error" {
			t.Fatalf("a retired model ended the conversation: %s", c.Error)
		}
		if c.State == "idle" && len(msgs) >= 2 && !e.Running(conv.ID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop did not finish, state %s with %d messages", c.State, len(msgs))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(msgs[len(msgs)-1].Content, "Nothing is deployed.") {
		t.Fatalf("the answer was lost: %s", msgs[len(msgs)-1].Content)
	}
	mu.Lock()
	gotRetired, gotNext := asked[retired], asked[geminiFlashLiteLatest]
	mu.Unlock()
	if gotRetired != 1 || gotNext != 1 {
		t.Fatalf("want one refused attempt and one accepted one, got %v", asked)
	}
	if got := st.Setting(ctx, "assistant.model."+ProviderGemini, ""); got != geminiFlashLiteLatest {
		t.Fatalf("the switch should be recorded, the store says %q", got)
	}
	if !e.retiredGemini(ctx, "AIzaGood")[retired] {
		t.Fatal("the refused name should be remembered for this key")
	}
	p := e.Settings(ctx)
	if p.Model != geminiFlashLiteLatest || p.Hint != "" {
		t.Fatalf("the panel should agree with the message path: %+v", p)
	}
	for _, x := range p.Providers {
		if x.ID != ProviderGemini {
			continue
		}
		for _, m := range x.Models {
			if m.ID == retired {
				t.Fatalf("a name Google refused is still offered: %+v", x.Models)
			}
		}
	}
}

// Google's error bodies are not shown as JSON.
func TestGeminiErrorBodyIsReadable(t *testing.T) {
	body := []byte(`[{"error":{"code":404,"message":"This model models/gemini-2.5-flash is no longer available to new users."}}]`)
	got := errorText(ProviderGemini, http.StatusNotFound, body)
	if strings.Contains(got, "{") || !strings.Contains(got, "no longer available") {
		t.Fatalf("want the message Google wrote, got %q", got)
	}
}
