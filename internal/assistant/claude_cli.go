package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/beedywool/vps-admin/internal/store"
)

// The subscription provider runs the official `claude` binary, the way Anthropic
// documents for a machine with no browser: a long-lived token from `claude setup-token`
// in CLAUDE_CODE_OAUTH_TOKEN, one turn per message, stream-json on stdout. The binary is
// left untouched and its own tools are disabled, so every action still goes through the
// panel's policy and approval cards.

var (
	errCLIMissing = errors.New("the claude command is not installed on this server")
	errStopped    = errors.New("the administrator stopped this conversation")
)

const claudeInstall = `curl -fsSL https://claude.ai/install.sh | bash
# or, with Node.js installed:
npm install -g @anthropic-ai/claude-code`

// claudeInfo reports where the CLI is and which version it is, cached for a few
// seconds: the settings panel asks after every change.
func (e *Engine) claudeInfo(ctx context.Context) (path, version string, err error) {
	e.cmu.Lock()
	if time.Since(e.cat) < 30*time.Second {
		path, version, err = e.cpath, e.cver, e.cerr
		e.cmu.Unlock()
		return
	}
	e.cmu.Unlock()

	path, err = findClaude(e.DataDir)
	if err == nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, verr := exec.CommandContext(ctx, path, "--version").Output()
		if verr != nil {
			err = fmt.Errorf("the claude command did not start: %w", verr)
		} else if fields := strings.Fields(string(out)); len(fields) > 0 {
			version = strings.TrimSpace(strings.TrimPrefix(fields[0], "v"))
		}
	}
	e.cmu.Lock()
	e.cpath, e.cver, e.cerr, e.cat = path, version, err, time.Now()
	e.cmu.Unlock()
	return
}

// findClaude looks where an installer would have put it. The agent's PATH is minimal on
// purpose, so the usual install locations are tried instead of trusting it.
func findClaude(dataDir string) (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	candidates := []string{
		"/usr/local/bin/claude",
		"/usr/bin/claude",
		"/root/.local/bin/claude",
		"/opt/claude/bin/claude",
	}
	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, ".local/bin/claude"))
	}
	if dataDir != "" {
		candidates = append(candidates, filepath.Join(dataDir, "claude-home", ".local/bin/claude"))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errCLIMissing
}

// claudeEnv is deliberately small: the CLI only needs its token, a writable home of its
// own (sessions live there) and a PATH. Nothing else from the agent's environment leaks
// into a process that can reach the model.
func (e *Engine) claudeEnv(path, token string) ([]string, error) {
	home := filepath.Join(e.DataDir, "claude-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	key := "CLAUDE_CODE_OAUTH_TOKEN"
	if !strings.HasPrefix(token, "sk-ant-oat") {
		key = "ANTHROPIC_API_KEY"
	}
	env := []string{
		"PATH=" + filepath.Dir(path) + ":/usr/local/bin:/usr/bin:/bin",
		"HOME=" + home,
		"LANG=C.UTF-8",
		"TERM=dumb",
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		key + "=" + token,
	}
	if e.ClaudeURL != "" { // tests: the CLI talks to a fake Anthropic endpoint
		env = append(env, "ANTHROPIC_BASE_URL="+e.ClaudeURL)
	}
	return env, nil
}

// claudeTurn runs one message through the CLI and returns the assistant turn.
func (e *Engine) claudeTurn(ctx context.Context, convID string, tgt target, prompt string,
	onText, onThinking func(string)) (*turn, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	path, _, err := e.claudeInfo(ctx)
	if err != nil {
		return nil, err
	}
	b, err := e.newBridge(convID)
	if err != nil {
		return nil, err
	}
	defer b.close()

	env, err := e.claudeEnv(path, tgt.Key)
	if err != nil {
		return nil, err
	}
	sessionKey := "assistant.claude_session." + convID
	session := e.Store.Setting(ctx, sessionKey, "")

	t, newSession, err := e.runClaude(ctx, path, env, b, tgt, prompt, session, onText, onThinking)
	if err != nil && session != "" && isMissingSession(err) {
		// The CLI forgot this conversation (a wiped home, a pruned session): start a new
		// one and hand it the last exchanges in the prompt instead.
		history := e.transcript(ctx, convID)
		t, newSession, err = e.runClaude(ctx, path, env, b, tgt, history+prompt, "", onText, onThinking)
	}
	if err != nil {
		return nil, err
	}
	if newSession != "" && newSession != session {
		e.Store.SetSetting(ctx, sessionKey, newSession)
	}
	return t, nil
}

// checkClaudeCode runs the smallest possible turn: one word, the cheapest family, no
// tools. A token from `claude setup-token` is an OAuth credential for the CLI, so the CLI
// is the only thing that can say whether it still works; and only an authentication
// answer condemns it, so a subscription that is rate limited or a network that is down
// does not cost the administrator what they pasted.
func (e *Engine) checkClaudeCode(ctx context.Context, token string) error {
	path, _, err := e.claudeInfo(ctx)
	if err != nil {
		return err
	}
	env, err := e.claudeEnv(path, token)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path,
		"-p", "ping",
		"--model", "haiku",
		"--tools", "",
		"--output-format", "json",
	)
	cmd.Env = env
	cmd.Dir = "/"
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &tailBuffer{buf: &stderr, max: 4096}
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return errors.New("Claude Code did not finish the token check in time")
	}
	// The CLI ends a print run with one result line; anything before it is a warning.
	var res claudeResult
	found := false
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var probe claudeResult
		if json.Unmarshal([]byte(line), &probe) == nil && probe.Type == "result" {
			res, found = probe, true
		}
	}
	if !found {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			return errors.New("the Claude Code CLI did not answer the token check")
		}
		return errors.New(cliReason(msg))
	}
	low := strings.ToLower(res.Result)
	switch {
	case res.APIErrorStatus == 401 || res.APIErrorStatus == 403:
		return fmt.Errorf("%w: Claude Code", errBadKey)
	case strings.Contains(low, "authenticate"), strings.Contains(low, "oauth token is invalid"),
		strings.Contains(low, "invalid api key"), strings.Contains(low, "not logged in"):
		return fmt.Errorf("%w: Claude Code", errBadKey)
	case res.IsError:
		return errors.New(cliReason(res.Result))
	}
	return nil
}

// claudeResult is the last line of a print run.
type claudeResult struct {
	Type           string `json:"type"`
	IsError        bool   `json:"is_error"`
	APIErrorStatus int    `json:"api_error_status"`
	Result         string `json:"result"`
}

func isMissingSession(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "no conversation found") || strings.Contains(m, "session not found") ||
		strings.Contains(m, "could not find session")
}

// runClaude starts the binary, streams its events, and returns the turn it produced.
func (e *Engine) runClaude(ctx context.Context, path string, env []string, b *bridge, tgt target,
	prompt, session string, onText, onThinking func(string)) (*turn, string, error) {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--model", tgt.Model,
		"--tools", "",
		"--mcp-config", b.config(),
		"--strict-mcp-config",
		"--allowedTools", strings.Join(toolNames(), ","),
		"--permission-mode", "dontAsk",
		"--append-system-prompt", e.panelPrompt(ctx),
	}
	effort := e.Store.Setting(ctx, "assistant.effort", "high")
	switch effort {
	case "low", "medium", "high", "xhigh", "max":
		args = append(args, "--effort", effort)
	}
	if session != "" {
		args = append(args, "--resume", session)
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Dir = "/"
	cmd.WaitDelay = 5 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &tailBuffer{buf: &stderr, max: 8192}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("could not start the Claude Code CLI: %w", err)
	}

	t := &turn{}
	var text strings.Builder
	sessionID := ""
	resultText, resultError, finish := "", "", ""
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type      string `json:"type"`
			Subtype   string `json:"subtype"`
			IsError   bool   `json:"is_error"`
			Result    string `json:"result"`
			SessionID string `json:"session_id"`
			Message   *struct {
				Content []storedBlock `json:"content"`
			} `json:"message"`
			Event *struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Thinking string `json:"thinking"`
				} `json:"delta"`
			} `json:"event"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // a warning printed on stdout, not an event
		}
		if ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		switch ev.Type {
		case "stream_event":
			if ev.Event == nil || ev.Event.Type != "content_block_delta" {
				continue
			}
			switch ev.Event.Delta.Type {
			case "text_delta":
				if ev.Event.Delta.Text != "" {
					onText(ev.Event.Delta.Text)
				}
			case "thinking_delta":
				if ev.Event.Delta.Thinking != "" {
					onThinking(ev.Event.Delta.Thinking)
				}
			}
		case "assistant":
			if ev.Message == nil {
				continue
			}
			for _, block := range ev.Message.Content {
				if block.Type == "text" && block.Text != "" {
					text.WriteString(block.Text)
				}
			}
		case "result":
			resultText, resultError, finish = ev.Result, ev.Subtype, ev.Type
			if ev.IsError && resultError == "success" {
				resultError = "error"
			}
		}
	}
	scanErr := sc.Err()
	waitErr := cmd.Wait()
	if scanErr != nil && ctx.Err() == nil {
		return nil, sessionID, scanErr
	}
	if ctx.Err() != nil {
		return nil, sessionID, errStopped
	}
	answer := text.String()
	if answer == "" {
		answer = resultText
	}
	if answer == "" {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "the Claude Code CLI exited without an answer"
		}
		if waitErr != nil {
			msg += " (" + waitErr.Error() + ")"
		}
		return nil, sessionID, errors.New(cliReason(msg))
	}
	if resultError != "" && resultError != "success" {
		return nil, sessionID, fmt.Errorf("Claude Code ended this turn: %s", resultError)
	}
	raw, err := json.Marshal(map[string]any{"role": "assistant", "content": []map[string]any{
		{"type": "text", "text": answer},
	}})
	if err != nil {
		return nil, sessionID, err
	}
	t.raw = raw
	t.blocks = []turnBlock{{Type: "text", Text: answer}}
	t.stop = "end_turn"
	_ = finish
	return t, sessionID, nil
}

// cliReason turns the CLI's own complaint into the sentence the panel shows.
func cliReason(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "invalid api key"), strings.Contains(low, "authentication"),
		strings.Contains(low, "not logged in"), strings.Contains(low, "oauth token"):
		return "Claude Code rejected the token. Run `claude setup-token` again and paste the new one."
	case strings.Contains(low, "rate limit"), strings.Contains(low, "429"):
		return "Your Claude subscription is rate limited right now. Try again in a moment."
	case strings.Contains(low, "usage limit"), strings.Contains(low, "quota"):
		return "Your Claude subscription reached its usage limit. Try again later or switch provider."
	case strings.Contains(low, "not installed"), strings.Contains(low, "command not found"):
		return errCLIMissing.Error()
	}
	if len(msg) > 400 {
		msg = msg[:400] + "…"
	}
	return "Claude Code failed: " + msg
}

// panelPrompt replaces what the CLI would say about itself with what the assistant is
// here: the administrator of one server, and the approval rule.
func (e *Engine) panelPrompt(ctx context.Context) string {
	h := e.Host
	return fmt.Sprintf(`You are the assistant built into VPS Admin, the administration panel of the Linux server %q (%s, kernel %s, %s, %d cores), and the person writing to you is its administrator.

Your only tools are the panel's, named mcp__vps__*: they read the server and propose changes. Read-only ones run at once; a change is shown to the administrator as a card they approve or deny, and the tool call waits for their answer. Ask for one change at a time and say in one sentence what it does and what it affects.
The panel's policy blocks deleting Docker volumes, changing SSH access or the firewall, reading keys, and powering the server off, whatever the approval: say so and give the command for the administrator to run over SSH.
Some commands need root: the panel's sudo only covers systemctl start/stop/restart/reload and a few status commands.
Answer in the language the administrator writes in, concisely, with commands in code blocks, and never invent command output.`+
		e.personaInstructions(ctx),
		h.Hostname, h.OS, h.Kernel, h.Arch, h.Cores)
}

// toolNames is the explicit allowlist handed to the CLI: its own tools are disabled and
// only these MCP tools are permitted, so no shell escapes the policy.
func toolNames() []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, "mcp__vps__"+d.name)
	}
	return out
}

// transcript is the compact retelling of the conversation, used when the CLI lost its
// session: enough context to go on without replaying every tool result.
func (e *Engine) transcript(ctx context.Context, convID string) string {
	msgs, err := e.Store.Messages(ctx, convID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	start := max(0, len(msgs)-12)
	for _, m := range msgs[start:] {
		var sm storedMsg
		if json.Unmarshal([]byte(m.Content), &sm) != nil {
			continue
		}
		var text string
		var blocks []storedBlock
		if json.Unmarshal(sm.Content, &blocks) == nil {
			var parts []string
			for _, blk := range blocks {
				if blk.Type == "text" && blk.Text != "" {
					parts = append(parts, blk.Text)
				}
			}
			text = strings.Join(parts, "\n")
		} else {
			json.Unmarshal(sm.Content, &text)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		who := "Administrator"
		if m.Role == "assistant" {
			who = "You"
		}
		fmt.Fprintf(&b, "%s: %s\n\n", who, text)
	}
	if b.Len() == 0 {
		return ""
	}
	return "Earlier in this conversation, before a restart:\n\n" + b.String() + "Continue from here.\n\n"
}

// lastUserText is the message the CLI answers on this turn.
func (e *Engine) lastUserText(ctx context.Context, convID string) (string, error) {
	msgs, err := e.Store.Messages(ctx, convID)
	if err != nil {
		return "", err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		var sm storedMsg
		if json.Unmarshal([]byte(msgs[i].Content), &sm) != nil {
			continue
		}
		var blocks []storedBlock
		if json.Unmarshal(sm.Content, &blocks) == nil {
			var parts []string
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
			if s := strings.TrimSpace(strings.Join(parts, "\n\n")); s != "" {
				return s, nil
			}
			continue
		}
		var text string
		if json.Unmarshal(sm.Content, &text) == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
	}
	return "", errors.New("no message to answer")
}

// tailBuffer keeps the end of a stream, where the CLI puts its error.
type tailBuffer struct {
	buf *bytes.Buffer
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if t.buf.Len() > t.max {
		b := t.buf.Bytes()
		kept := append([]byte(nil), b[len(b)-t.max:]...)
		t.buf.Reset()
		t.buf.Write(kept)
	}
	return len(p), nil
}

// compile-time guard: the CLI path reads the store the same way as the API one.
var _ = store.ConvMessage{}
