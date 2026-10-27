package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Device struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Owner      string     `json:"owner"` // the person using it
	Kind       string     `json:"kind"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
	LastIP     string     `json:"lastIp,omitempty"`
	Passkeys   int        `json:"passkeys"`
}

func unixPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0)
	return &t
}

// AddPairingCode stores the hash of a one-time code valid for ttl.
func (s *Store) AddPairingCode(ctx context.Context, hash []byte, ttl time.Duration) error {
	n := now()
	// Only the latest code stays usable.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM pairing_codes WHERE used_at IS NULL OR expires_at < ?`, n); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pairing_codes(code_hash, created_at, expires_at) VALUES(?, ?, ?)`, hash, n, n+int64(ttl/time.Second))
	return err
}

// UsePairingCode marks a valid code as used. It returns false when the code is unknown,
// expired or already used.
func (s *Store) UsePairingCode(ctx context.Context, hash []byte) (bool, error) {
	n := now()
	r, err := s.db.ExecContext(ctx,
		`UPDATE pairing_codes SET used_at = ? WHERE code_hash = ? AND used_at IS NULL AND expires_at >= ?`, n, hash, n)
	if err != nil {
		return false, err
	}
	c, err := r.RowsAffected()
	return c == 1, err
}

func (s *Store) AddDevice(ctx context.Context, id, name, owner, kind string, tokenHash []byte, ip string) error {
	var th any
	if tokenHash != nil {
		th = tokenHash
	}
	n := now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices(id, name, owner, kind, token_hash, created_at, last_seen_at, last_ip) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, owner, kind, th, n, n, ip)
	return err
}

// DeviceByToken returns the active device that owns this app token hash.
func (s *Store) DeviceByToken(ctx context.Context, hash []byte) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM devices WHERE token_hash = ? AND revoked_at IS NULL`, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// TouchDevice records activity, at most once a minute per device.
func (s *Store) TouchDevice(ctx context.Context, id, ip string) {
	n := now()
	s.db.ExecContext(ctx,
		`UPDATE devices SET last_seen_at = ?, last_ip = ? WHERE id = ? AND (last_seen_at IS NULL OR last_seen_at < ? OR last_ip IS NOT ?)`,
		n, ip, id, n-60, ip)
}

func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.name, d.owner, d.kind, d.created_at, d.last_seen_at, COALESCE(d.last_ip, ''),
		       (SELECT COUNT(*) FROM passkeys p WHERE p.device_id = d.id)
		FROM devices d WHERE d.revoked_at IS NULL ORDER BY d.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		var created int64
		var seen sql.NullInt64
		if err := rows.Scan(&d.ID, &d.Name, &d.Owner, &d.Kind, &created, &seen, &d.LastIP, &d.Passkeys); err != nil {
			return nil, err
		}
		d.CreatedAt = time.Unix(created, 0)
		d.LastSeenAt = unixPtr(seen)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeviceActive(ctx context.Context, id string) bool {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1 FROM devices WHERE id = ? AND revoked_at IS NULL`, id).Scan(&one) == nil
}

// RenameDevice sets who uses a device and what it is.
func (s *Store) RenameDevice(ctx context.Context, id, owner, name string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE devices SET owner = ?, name = ? WHERE id = ? AND revoked_at IS NULL`, owner, name, id)
	if err != nil {
		return err
	}
	if c, _ := r.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeDevice disables a device and drops its sessions and passkeys.
func (s *Store) RevokeDevice(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx,
		`UPDATE devices SET revoked_at = ?, token_hash = NULL WHERE id = ? AND revoked_at IS NULL`, now(), id)
	if err != nil {
		return err
	}
	if c, _ := r.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE device_id = ?`, id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE device_id = ?`, id)
	return err
}

func (s *Store) CountActiveDevices(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE revoked_at IS NULL`).Scan(&n)
	return n, err
}

type Session struct {
	DeviceID  string
	Scope     string
	ExpiresAt time.Time
}

func (s *Store) AddSession(ctx context.Context, idHash []byte, deviceID, scope string, ttl time.Duration) error {
	n := now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, n); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(id_hash, device_id, scope, created_at, expires_at) VALUES(?, ?, ?, ?, ?)`,
		idHash, deviceID, scope, n, n+int64(ttl/time.Second))
	return err
}

func (s *Store) Session(ctx context.Context, idHash []byte) (*Session, error) {
	var ss Session
	var exp int64
	err := s.db.QueryRowContext(ctx, `
		SELECT s.device_id, s.scope, s.expires_at FROM sessions s
		JOIN devices d ON d.id = s.device_id AND d.revoked_at IS NULL
		WHERE s.id_hash = ? AND s.expires_at >= ?`, idHash, now()).Scan(&ss.DeviceID, &ss.Scope, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ss.ExpiresAt = time.Unix(exp, 0)
	return &ss, nil
}

func (s *Store) DeleteSession(ctx context.Context, idHash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, idHash)
	return err
}

type Passkey struct {
	ID         []byte
	DeviceID   string
	Credential string
}

func (s *Store) AddPasskey(ctx context.Context, id []byte, deviceID, credential string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO passkeys(id, device_id, credential, created_at) VALUES(?, ?, ?, ?)`, id, deviceID, credential, now())
	return err
}

func (s *Store) Passkeys(ctx context.Context) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.device_id, p.credential FROM passkeys p
		JOIN devices d ON d.id = p.device_id AND d.revoked_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		if err := rows.Scan(&p.ID, &p.DeviceID, &p.Credential); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePasskey stores the credential again after a login (sign counter, flags).
func (s *Store) UpdatePasskey(ctx context.Context, id []byte, credential string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ?`, credential, now(), id)
	return err
}
