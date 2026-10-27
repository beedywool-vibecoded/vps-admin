package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConvMessage struct {
	Seq     int64  `json:"seq"`
	Role    string `json:"role"`
	Content string `json:"content"` // raw JSON of the message param
}

type ToolCall struct {
	ID        string     `json:"id"`
	ConvID    string     `json:"convId"`
	Batch     int64      `json:"batch"`
	Ord       int        `json:"ord"`
	Name      string     `json:"name"`
	Input     string     `json:"input"`
	Summary   string     `json:"summary"`
	Kind      string     `json:"kind"`
	Status    string     `json:"status"`
	Result    string     `json:"result"`
	IsError   bool       `json:"isError"`
	CreatedAt time.Time  `json:"createdAt"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
}

func (s *Store) AddConversation(ctx context.Context, id, title string) error {
	n := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversations(id, title, state, created_at, updated_at) VALUES(?, ?, 'idle', ?, ?)`, id, title, n, n)
	return err
}

func scanConv(r scanner) (*Conversation, error) {
	var c Conversation
	var cr, up int64
	if err := r.Scan(&c.ID, &c.Title, &c.State, &c.Error, &cr, &up); err != nil {
		return nil, err
	}
	c.CreatedAt, c.UpdatedAt = time.Unix(cr, 0), time.Unix(up, 0)
	return &c, nil
}

func (s *Store) Conversation(ctx context.Context, id string) (*Conversation, error) {
	c, err := scanConv(s.db.QueryRowContext(ctx, `SELECT id, title, state, error, created_at, updated_at FROM conversations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) Conversations(ctx context.Context, limit int) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, state, error, created_at, updated_at FROM conversations ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) SetConversationState(ctx context.Context, id, state, errMsg string) error {
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE conversations SET state = ?, error = ?, updated_at = ? WHERE id = ?`, state, errMsg, now(), id)
	return err
}

func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	return err
}

// AppendMessage appends to the history and returns its seq. Append-only: thinking blocks
// are only valid in an unchanged conversation.
func (s *Store) AppendMessage(ctx context.Context, convID, role, content string) (int64, error) {
	ctx = context.WithoutCancel(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM conv_messages WHERE conv_id = ?`, convID).Scan(&seq); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conv_messages(conv_id, seq, role, content, created_at) VALUES(?, ?, ?, ?, ?)`, convID, seq, role, content, now()); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now(), convID); err != nil {
		return 0, err
	}
	return seq, tx.Commit()
}

func (s *Store) Messages(ctx context.Context, convID string) ([]ConvMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, role, content FROM conv_messages WHERE conv_id = ? ORDER BY seq`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConvMessage{}
	for rows.Next() {
		var m ConvMessage
		if err := rows.Scan(&m.Seq, &m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const toolCols = `id, conv_id, batch, ord, name, input, summary, kind, status, result, is_error, created_at, decided_at`

func scanTool(r scanner) (*ToolCall, error) {
	var t ToolCall
	var cr int64
	var dec sql.NullInt64
	if err := r.Scan(&t.ID, &t.ConvID, &t.Batch, &t.Ord, &t.Name, &t.Input, &t.Summary, &t.Kind, &t.Status, &t.Result, &t.IsError, &cr, &dec); err != nil {
		return nil, err
	}
	t.CreatedAt = time.Unix(cr, 0)
	t.DecidedAt = unixPtr(dec)
	return &t, nil
}

func (s *Store) AddToolCall(ctx context.Context, t *ToolCall) error {
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO tool_calls(id, conv_id, batch, ord, name, input, summary, kind, status, result, is_error, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ConvID, t.Batch, t.Ord, t.Name, t.Input, t.Summary, t.Kind, t.Status, t.Result, t.IsError, now())
	return err
}

func (s *Store) FinishToolCall(ctx context.Context, id, status, result string, isError bool, deviceID string) error {
	var dev any
	if deviceID != "" {
		dev = deviceID
	}
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE tool_calls SET status = ?, result = ?, is_error = ?, device_id = COALESCE(?, device_id), decided_at = ? WHERE id = ?`,
		status, result, isError, dev, now(), id)
	return err
}

// ClaimToolCall moves a pending call to running; false if it wasn't pending, so double
// approvals are harmless.
func (s *Store) ClaimToolCall(ctx context.Context, id, deviceID string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE tool_calls SET status = 'running', device_id = ? WHERE id = ? AND status = 'pending'`, deviceID, id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

func (s *Store) ToolCall(ctx context.Context, id string) (*ToolCall, error) {
	t, err := scanTool(s.db.QueryRowContext(ctx, `SELECT `+toolCols+` FROM tool_calls WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) ToolCalls(ctx context.Context, convID string) ([]ToolCall, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+toolCols+` FROM tool_calls WHERE conv_id = ? ORDER BY batch, ord`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ToolCall{}
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// InterruptedConversations resets conversations that were mid-request when the agent stopped.
func (s *Store) InterruptedConversations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE conversations SET state = 'error', error = 'interrupted by an agent restart, send a message to continue' WHERE state = 'running'`)
	return err
}
