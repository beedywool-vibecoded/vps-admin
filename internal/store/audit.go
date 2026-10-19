package store

import (
	"context"
	"time"
)

type AuditEntry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	DeviceID string    `json:"deviceId,omitempty"`
	Device   string    `json:"device,omitempty"`
	IP       string    `json:"ip,omitempty"`
	Action   string    `json:"action"`
	Target   string    `json:"target,omitempty"`
	OK       bool      `json:"ok"`
	Detail   string    `json:"detail,omitempty"`
}

// Audit records an action. Errors are ignored on purpose: a full disk must not block
// the admin from fixing it.
func (s *Store) Audit(ctx context.Context, e AuditEntry) {
	var dev any
	if e.DeviceID != "" {
		dev = e.DeviceID
	}
	s.db.ExecContext(context.WithoutCancel(ctx),
		`INSERT INTO audit(at, device_id, ip, action, target, ok, detail) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		now(), dev, e.IP, e.Action, e.Target, e.OK, e.Detail)
}

// AuditLog returns entries older than before (0 for the newest), newest first.
func (s *Store) AuditLog(ctx context.Context, before int64, limit int) ([]AuditEntry, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.at, COALESCE(a.device_id, ''), CASE WHEN d.owner != '' THEN d.owner || ' · ' || d.name ELSE COALESCE(d.name, '') END, COALESCE(a.ip, ''), a.action, a.target, a.ok, a.detail
		FROM audit a LEFT JOIN devices d ON d.id = a.device_id
		WHERE a.id < ? ORDER BY a.id DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var at int64
		if err := rows.Scan(&e.ID, &at, &e.DeviceID, &e.Device, &e.IP, &e.Action, &e.Target, &e.OK, &e.Detail); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAudit keeps a year of history.
func (s *Store) PruneAudit(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM audit WHERE at < ?`, time.Now().AddDate(-1, 0, 0).Unix())
	return err
}
