package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type App struct {
	ID             string    `json:"id"`
	Repo           string    `json:"repo"`
	InstallationID int64     `json:"installationId"`
	Branch         string    `json:"branch"`
	RootDir        string    `json:"rootDir"`
	Build          string    `json:"build"`
	Port           int       `json:"port"`
	Domain         string    `json:"domain"`
	AutoDeploy     bool      `json:"autoDeploy"`
	Keep           int       `json:"keep"`
	MemoryMB       int       `json:"memoryMb"`
	CreatedAt      time.Time `json:"createdAt"`
	// Dir is an existing folder (a git checkout with a compose file) that this app keeps
	// up to date in place. Empty for apps the panel builds itself.
	Dir         string `json:"dir"`
	InstallCmd  string `json:"installCmd"`
	BuildCmd    string `json:"buildCmd"`
	StartCmd    string `json:"startCmd"`
	Dockerfile  string `json:"dockerfile"`  // path relative to the root folder
	ComposeFile string `json:"composeFile"` // path relative to the root folder
	// Access is public, vpn, tailscale or local (deploy.Access); Allow the networks of
	// a VPN-only app; TSPort its HTTPS port on the tailnet once served there.
	Access string `json:"access"`
	Allow  string `json:"allow"`
	TSPort int    `json:"tsPort"`
}

type EnvVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"` // sealed in the database
	Secret bool   `json:"secret"`
}

type Deployment struct {
	ID         int64      `json:"id"`
	AppID      string     `json:"appId"`
	SHA        string     `json:"sha"`
	Message    string     `json:"message"`
	Trigger    string     `json:"trigger"`
	Status     string     `json:"status"`
	Build      string     `json:"build"`
	Image      string     `json:"image"`
	Container  string     `json:"container"`
	HostPort   int        `json:"hostPort"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

var ErrExists = errors.New("already exists")

const appCols = `id, repo, installation_id, branch, root_dir, build, port, domain, auto_deploy, keep, memory_mb, created_at, dir, install_cmd, build_cmd, start_cmd, dockerfile, compose_file, access, allow, ts_port`

type scanner interface{ Scan(...any) error }

func scanApp(r scanner) (*App, error) {
	var a App
	var created int64
	if err := r.Scan(&a.ID, &a.Repo, &a.InstallationID, &a.Branch, &a.RootDir, &a.Build, &a.Port, &a.Domain, &a.AutoDeploy, &a.Keep, &a.MemoryMB, &created,
		&a.Dir, &a.InstallCmd, &a.BuildCmd, &a.StartCmd, &a.Dockerfile, &a.ComposeFile, &a.Access, &a.Allow, &a.TSPort); err != nil {
		return nil, err
	}
	a.CreatedAt = time.Unix(created, 0)
	return &a, nil
}

func (s *Store) AddApp(ctx context.Context, a *App) error {
	if _, err := s.App(ctx, a.ID); err == nil {
		return ErrExists
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO apps(`+appCols+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Repo, a.InstallationID, a.Branch, a.RootDir, a.Build, a.Port, a.Domain, a.AutoDeploy, a.Keep, a.MemoryMB, now(),
		a.Dir, a.InstallCmd, a.BuildCmd, a.StartCmd, a.Dockerfile, a.ComposeFile, a.Access, a.Allow, a.TSPort)
	return err
}

func (s *Store) UpdateApp(ctx context.Context, a *App) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET branch = ?, root_dir = ?, build = ?, port = ?, domain = ?, auto_deploy = ?, keep = ?, memory_mb = ?,
		install_cmd = ?, build_cmd = ?, start_cmd = ?, dockerfile = ?, compose_file = ?, access = ?, allow = ?, ts_port = ? WHERE id = ?`,
		a.Branch, a.RootDir, a.Build, a.Port, a.Domain, a.AutoDeploy, a.Keep, a.MemoryMB,
		a.InstallCmd, a.BuildCmd, a.StartCmd, a.Dockerfile, a.ComposeFile, a.Access, a.Allow, a.TSPort, a.ID)
	return err
}

func (s *Store) App(ctx context.Context, id string) (*App, error) {
	a, err := scanApp(s.db.QueryRowContext(ctx, `SELECT `+appCols+` FROM apps WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) Apps(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appCols+` FROM apps ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// AppsForPush lists the apps that deploy automatically from this repo and branch.
func (s *Store) AppsForPush(ctx context.Context, repo, branch string) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appCols+` FROM apps WHERE repo = ? COLLATE NOCASE AND branch = ? AND auto_deploy = 1`, repo, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteApp(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM apps WHERE id = ?`, id)
	return err
}

// Env returns the sealed variables of an app.
func (s *Store) Env(ctx context.Context, appID string) ([]EnvVar, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, secret FROM app_env WHERE app_id = ? ORDER BY key`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EnvVar{}
	for rows.Next() {
		var e EnvVar
		if err := rows.Scan(&e.Key, &e.Value, &e.Secret); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEnv replaces every variable of an app at once.
func (s *Store) SetEnv(ctx context.Context, appID string, vars []EnvVar) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM app_env WHERE app_id = ?`, appID); err != nil {
		return err
	}
	for _, v := range vars {
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_env(app_id, key, value, secret) VALUES(?, ?, ?, ?)`, appID, v.Key, v.Value, v.Secret); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const depCols = `id, app_id, sha, message, trigger, status, build, image, container, host_port, error, created_at, finished_at`

func scanDep(r scanner) (*Deployment, error) {
	var d Deployment
	var created int64
	var fin sql.NullInt64
	if err := r.Scan(&d.ID, &d.AppID, &d.SHA, &d.Message, &d.Trigger, &d.Status, &d.Build, &d.Image, &d.Container, &d.HostPort, &d.Error, &created, &fin); err != nil {
		return nil, err
	}
	d.CreatedAt = time.Unix(created, 0)
	d.FinishedAt = unixPtr(fin)
	return &d, nil
}

func (s *Store) AddDeployment(ctx context.Context, d *Deployment) error {
	r, err := s.db.ExecContext(ctx, `INSERT INTO deployments(app_id, sha, message, trigger, status, image, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		d.AppID, d.SHA, d.Message, d.Trigger, d.Status, d.Image, now())
	if err != nil {
		return err
	}
	d.ID, err = r.LastInsertId()
	d.CreatedAt = time.Now()
	return err
}

// SaveDeployment writes every field except the log, which AppendLog maintains.
func (s *Store) SaveDeployment(ctx context.Context, d *Deployment) error {
	var fin any
	if d.FinishedAt != nil {
		fin = d.FinishedAt.Unix()
	}
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE deployments SET sha = ?, message = ?, status = ?, build = ?, image = ?, container = ?, host_port = ?, error = ?, finished_at = ? WHERE id = ?`,
		d.SHA, d.Message, d.Status, d.Build, d.Image, d.Container, d.HostPort, d.Error, fin, d.ID)
	return err
}

func (s *Store) AppendLog(ctx context.Context, id int64, text string) {
	s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE deployments SET log = log || ? WHERE id = ?`, text, id)
}

func (s *Store) DeploymentLog(ctx context.Context, id int64) (string, error) {
	var l string
	err := s.db.QueryRowContext(ctx, `SELECT log FROM deployments WHERE id = ?`, id).Scan(&l)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return l, err
}

func (s *Store) Deployment(ctx context.Context, id int64) (*Deployment, error) {
	d, err := scanDep(s.db.QueryRowContext(ctx, `SELECT `+depCols+` FROM deployments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *Store) Deployments(ctx context.Context, appID string, limit int) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+depCols+` FROM deployments WHERE app_id = ? ORDER BY id DESC LIMIT ?`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Deployment{}
	for rows.Next() {
		d, err := scanDep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// LiveDeployment is the deployment currently serving the app, if any.
func (s *Store) LiveDeployment(ctx context.Context, appID string) (*Deployment, error) {
	d, err := scanDep(s.db.QueryRowContext(ctx, `SELECT `+depCols+` FROM deployments WHERE app_id = ? AND status = 'live' ORDER BY id DESC LIMIT 1`, appID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

// InterruptedDeployments marks builds that were running when the agent stopped.
func (s *Store) InterruptedDeployments(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deployments SET status = 'failed', error = 'interrupted by an agent restart', finished_at = ? WHERE status IN ('queued', 'building', 'starting')`, now())
	return err
}
