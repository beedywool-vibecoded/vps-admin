package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type ScriptRun struct {
	ID       string     `json:"id"`
	Script   string     `json:"script"`
	Args     string     `json:"args"`
	Device   string     `json:"device,omitempty"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	ExitCode *int       `json:"exitCode,omitempty"`
	Error    string     `json:"error,omitempty"`
}

func (s *Store) AddScriptRun(ctx context.Context, id, script, args, deviceID string, started time.Time) error {
	var dev any
	if deviceID != "" {
		dev = deviceID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_runs(id, script, args, device_id, started) VALUES(?, ?, ?, ?, ?)`, id, script, args, dev, started.Unix())
	return err
}

func (s *Store) FinishScriptRun(ctx context.Context, id string, finished time.Time, exitCode *int, errMsg, output string) error {
	var code any
	if exitCode != nil {
		code = *exitCode
	}
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE script_runs SET finished = ?, exit_code = ?, error = ?, output = ? WHERE id = ?`, finished.Unix(), code, errMsg, output, id)
	return err
}

// ScriptRuns lists recent runs, of one script or of all when script is empty.
func (s *Store) ScriptRuns(ctx context.Context, script string, limit int) ([]ScriptRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.script, r.args, CASE WHEN d.owner != '' THEN d.owner || ' · ' || d.name ELSE COALESCE(d.name, '') END, r.started, r.finished, r.exit_code, r.error
		FROM script_runs r LEFT JOIN devices d ON d.id = r.device_id
		WHERE ? = '' OR r.script = ? ORDER BY r.started DESC LIMIT ?`, script, script, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptRun{}
	for rows.Next() {
		var r ScriptRun
		var started int64
		var fin, code sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Script, &r.Args, &r.Device, &started, &fin, &code, &r.Error); err != nil {
			return nil, err
		}
		r.Started = time.Unix(started, 0)
		r.Finished = unixPtr(fin)
		if code.Valid {
			c := int(code.Int64)
			r.ExitCode = &c
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ScriptRunOutput(ctx context.Context, id string) (string, error) {
	var out string
	err := s.db.QueryRowContext(ctx, `SELECT output FROM script_runs WHERE id = ?`, id).Scan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return out, err
}

// InterruptedScriptRuns closes runs that were going when the agent stopped.
func (s *Store) InterruptedScriptRuns(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE script_runs SET finished = started, error = 'interrupted by an agent restart' WHERE finished IS NULL`)
	return err
}
