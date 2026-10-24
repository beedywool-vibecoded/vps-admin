package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/github"
	"github.com/beedywool/vps-admin/internal/store"
)

const githubSetting = "github.app"

// manifestStates holds the one-time state values of pending GitHub App creations. The
// callback arrives as a cross-site navigation from github.com, which does not carry the
// SameSite=Strict session cookie, so the state is what proves the request is ours.
type manifestStates struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (m *manifestStates) add() string {
	b := make([]byte, 24)
	rand.Read(b)
	s := base64.RawURLEncoding.EncodeToString(b)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = map[string]time.Time{}
	}
	for k, exp := range m.m {
		if time.Now().After(exp) {
			delete(m.m, k)
		}
	}
	m.m[s] = time.Now().Add(15 * time.Minute)
	return s
}

func (m *manifestStates) take(s string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.m[s]
	delete(m.m, s)
	return ok && time.Now().Before(exp)
}

// LoadGitHub restores the App credentials saved by a previous run.
func (s *Server) LoadGitHub(ctx context.Context) {
	sealed := s.Store.Setting(ctx, githubSetting, "")
	if sealed == "" {
		return
	}
	raw, err := s.Box.Open(sealed)
	if err != nil {
		slog.Error("cannot decrypt the GitHub App credentials", "err", err)
		return
	}
	var cr github.Creds
	if err := json.Unmarshal([]byte(raw), &cr); err != nil {
		return
	}
	if err := s.GitHub.SetCreds(&cr); err != nil {
		slog.Error("invalid GitHub App credentials", "err", err)
	}
}

func (s *Server) githubStatus(w http.ResponseWriter, r *http.Request) {
	res := map[string]any{"connected": false, "domains": s.Deploy.SitesEnabled(), "publicUrl": s.Cfg.PublicURL}
	if cr := s.GitHub.Creds(); cr != nil {
		res["connected"] = true
		res["app"] = map[string]string{"slug": cr.Slug, "name": cr.Name, "owner": cr.Owner, "htmlUrl": cr.HTMLURL,
			"installUrl": "https://github.com/apps/" + cr.Slug + "/installations/new"}
		insts, err := s.GitHub.Installations(r.Context())
		if err != nil {
			res["error"] = err.Error()
		}
		res["installations"] = insts
	}
	writeJSON(w, http.StatusOK, res)
}

// githubManifest returns what the panel posts to GitHub to create the App.
func (s *Server) githubManifest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Org string `json:"org"`
	}
	readJSON(r, &body)
	host, _ := os.Hostname()
	suffix := make([]byte, 3)
	rand.Read(suffix)
	// App names are global on GitHub, hence the random suffix.
	name := fmt.Sprintf("VPS Admin %s %x", host, suffix)
	if len(name) > 34 {
		name = name[:27] + fmt.Sprintf(" %x", suffix)
	}
	target := "https://github.com/settings/apps/new"
	if body.Org != "" {
		target = "https://github.com/organizations/" + url.PathEscape(body.Org) + "/settings/apps/new"
	}
	manifest, _ := json.Marshal(github.Manifest(s.Cfg.PublicURL, s.Cfg.WebhookURL, name))
	writeJSON(w, http.StatusOK, map[string]string{"url": target + "?state=" + s.manifest.add(), "manifest": string(manifest)})
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !s.manifest.take(q.Get("state")) || q.Get("code") == "" {
		http.Redirect(w, r, "/apps?github=expired", http.StatusSeeOther)
		return
	}
	cr, err := s.GitHub.Convert(r.Context(), q.Get("code"))
	if err == nil {
		err = s.GitHub.SetCreds(cr)
	}
	if err == nil {
		raw, _ := json.Marshal(cr)
		err = s.Store.SetSetting(r.Context(), githubSetting, s.Box.Seal(string(raw)))
	}
	s.Store.Audit(r.Context(), auditEntry(s.clientIP(r), "github.connect", "", err))
	if err != nil {
		http.Redirect(w, r, "/apps?github=failed", http.StatusSeeOther)
		return
	}
	// Next: pick the repositories the App may read.
	http.Redirect(w, r, "https://github.com/apps/"+url.PathEscape(cr.Slug)+"/installations/new", http.StatusSeeOther)
}

func (s *Server) githubDisconnect(w http.ResponseWriter, r *http.Request) {
	s.GitHub.SetCreds(nil)
	err := s.Store.SetSetting(r.Context(), githubSetting, "")
	s.audit(r, "github.disconnect", "", err)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) githubRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.GitHub.Repos(r.Context())
	if err != nil {
		writeErrorMsg(w, githubStatusCode(err), "github_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

func githubStatusCode(err error) int {
	var ae *github.APIError
	if errors.Is(err, github.ErrNotConnected) {
		return http.StatusPreconditionFailed
	}
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return http.StatusNotFound
	}
	return http.StatusBadGateway
}

func (s *Server) githubDetect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	inst, _ := strconv.ParseInt(q.Get("installation"), 10, 64)
	repo, ref, root := q.Get("repo"), q.Get("ref"), strings.Trim(q.Get("root"), "/")
	files, err := s.GitHub.Files(r.Context(), inst, repo, ref, root)
	if err != nil {
		writeErrorMsg(w, githubStatusCode(err), "github_failed", err.Error())
		return
	}
	join := func(f string) string {
		if root == "" {
			return f
		}
		return root + "/" + f
	}
	pkg, _ := s.GitHub.File(r.Context(), inst, repo, ref, join("package.json"))
	df, _ := s.GitHub.File(r.Context(), inst, repo, ref, join("Dockerfile"))
	writeJSON(w, http.StatusOK, map[string]any{"detection": deploy.Detect(files, pkg, df), "files": files})
}

// githubBranches lists a repository's branches for the branch picker.
func (s *Server) githubBranches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	inst, _ := strconv.ParseInt(q.Get("installation"), 10, 64)
	list, err := s.GitHub.Branches(r.Context(), inst, q.Get("repo"))
	if err != nil {
		writeErrorMsg(w, githubStatusCode(err), "github_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": list})
}

// readRepo looks a repository up in what the GitHub App can read, returning its current
// owner/name and installation. An empty name means the App has no access to it. GitHub
// reports the name it has today, so a repository that moved to an organization is found
// under its new owner.
func (s *Server) readRepo(ctx context.Context, name string) (string, int64, error) {
	repos, err := s.GitHub.Repos(ctx)
	if err != nil {
		return "", 0, err
	}
	for _, rp := range repos {
		if strings.EqualFold(rp.FullName, name) {
			return rp.FullName, rp.InstallationID, nil
		}
	}
	return "", 0, nil
}

// githubWebhook deploys apps that follow the pushed branch. Public, authenticated by the
// HMAC signature GitHub computes with the App webhook secret.
func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if err := s.GitHub.VerifyWebhook(body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		writeError(w, http.StatusUnauthorized, "bad_signature")
		return
	}
	if r.Header.Get("X-GitHub-Event") != "push" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	push, err := github.ParsePush(body)
	if err != nil || push.Deleted {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	apps, err := s.Store.AppsForPush(r.Context(), push.Repo, push.Branch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	started := []string{}
	for _, a := range apps {
		_, err := s.Deploy.Deploy(context.WithoutCancel(r.Context()), a, "push", push.SHA, push.Message, "")
		s.Store.Audit(r.Context(), auditEntry(s.clientIP(r), "app.deploy", a.ID, err))
		if err == nil {
			started = append(started, a.ID)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"deploying": started})
}

// Apps

type envView struct {
	Key      string `json:"key"`
	Value    string `json:"value"` // empty for secrets
	Secret   bool   `json:"secret"`
	HasValue bool   `json:"hasValue"`
}

type appView struct {
	store.App
	Status string            `json:"status"`
	Live   *store.Deployment `json:"live,omitempty"`
	Latest *store.Deployment `json:"latest,omitempty"`
	Busy   bool              `json:"busy"`
	// URL is where the app answers; AccessMode the effective mode (Access may be empty
	// for apps made before access modes).
	URL        string `json:"url,omitempty"`
	AccessMode string `json:"accessMode"`
}

func (s *Server) appView(ctx context.Context, a store.App) appView {
	v := appView{App: a, Status: s.Deploy.ContainerStatus(ctx, a), Busy: s.Deploy.Busy(a.ID), URL: s.Deploy.URL(ctx, a), AccessMode: deploy.Access(a)}
	if live, err := s.Store.LiveDeployment(ctx, a.ID); err == nil {
		v.Live = live
	}
	if deps, err := s.Store.Deployments(ctx, a.ID, 1); err == nil && len(deps) > 0 {
		v.Latest = &deps[0]
	}
	return v
}

func (s *Server) appsList(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Store.Apps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	out := make([]appView, 0, len(apps))
	for _, a := range apps {
		out = append(out, s.appView(r.Context(), a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "domains": s.Deploy.SitesEnabled()})
}

func (s *Server) loadApp(w http.ResponseWriter, r *http.Request) *store.App {
	a, err := s.Store.App(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "app_not_found")
		return nil
	}
	return a
}

type appInput struct {
	ID          string  `json:"id"`
	Repo        string  `json:"repo"`
	Branch      string  `json:"branch"`
	RootDir     string  `json:"rootDir"`
	Build       string  `json:"build"`
	Port        int     `json:"port"`
	Domain      string  `json:"domain"`
	AutoDeploy  bool    `json:"autoDeploy"`
	Keep        int     `json:"keep"`
	MemoryMB    int     `json:"memoryMb"`
	InstallCmd  string  `json:"installCmd"`
	BuildCmd    string  `json:"buildCmd"`
	StartCmd    string  `json:"startCmd"`
	Dockerfile  string  `json:"dockerfile"`
	ComposeFile string  `json:"composeFile"`
	Access      string  `json:"access"`
	Allow       string  `json:"allow"`
	Env         []envIn `json:"env"`
	Deploy      bool    `json:"deploy"`
	// Optional: the variables can be written to a file in the app's folder instead of
	// being handed to the app. Pointers so an older panel cannot wipe the setting.
	EnvFile *bool   `json:"envFile"`
	EnvPath *string `json:"envPath"`
}

type envIn struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
	Keep   bool   `json:"keep"` // secret left unchanged
	// HasValue is read-only: the panel gets it from appGet and used to echo it back
	// on save, which the strict JSON decoder rejected as an unknown field (400).
	HasValue *bool `json:"hasValue"`
}

func validBuild(b string) bool {
	switch b {
	case deploy.BuildAuto, deploy.BuildCompose, deploy.BuildDockerfile, deploy.BuildNode, deploy.BuildStatic:
		return true
	}
	return false
}

func (in *appInput) validate() string {
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	in.RootDir = strings.Trim(strings.TrimSpace(in.RootDir), "/")
	if in.EnvPath != nil {
		p := strings.TrimSpace(*in.EnvPath)
		in.EnvPath = &p
	}
	switch {
	case in.EnvPath != nil && !deploy.ValidEnvPath(*in.EnvPath):
		return "the .env path must be a file inside the app's folder"
	case strings.TrimSpace(in.Branch) == "":
		return "branch is required"
	case !validBuild(in.Build):
		return "unknown build type"
	case in.Port < 0 || in.Port > 65535:
		return "invalid port"
	case !deploy.ValidDomain(in.Domain):
		return "invalid domain"
	case in.Keep < 1 || in.Keep > 20:
		return "keep must be between 1 and 20"
	case in.MemoryMB < 0:
		return "invalid memory limit"
	case strings.Contains(in.RootDir, ".."):
		return "invalid root folder"
	case strings.Contains(in.Dockerfile, "..") || strings.HasPrefix(in.Dockerfile, "/") ||
		strings.Contains(in.ComposeFile, "..") || strings.HasPrefix(in.ComposeFile, "/"):
		return "file paths are relative to the root folder"
	}
	for _, c := range []string{in.InstallCmd, in.BuildCmd, in.StartCmd} {
		if strings.ContainsAny(c, "\n\r") {
			return "commands fit on one line (chain them with &&)"
		}
	}
	switch in.Access {
	case "", deploy.AccessPublic, deploy.AccessLocal, deploy.AccessTailscale:
	case deploy.AccessVPN:
		nets, err := deploy.AllowList(in.Allow)
		if err != nil {
			return err.Error()
		}
		if len(nets) == 0 {
			return "list the networks allowed to reach the app"
		}
		if in.Domain == "" {
			return "a VPN-only app still needs a domain"
		}
	default:
		return "unknown access mode"
	}
	return ""
}

// accessError checks that the server can publish the app the way asked.
func (s *Server) accessError(ctx context.Context, in *appInput) string {
	switch in.Access {
	case deploy.AccessTailscale:
		ts := s.Runner.DetectEnvironment(ctx).Tailscale
		if ts == nil || !ts.Running {
			return "Tailscale is not running on this server"
		}
		if !ts.HTTPS {
			return "enable HTTPS certificates in the Tailscale admin console (DNS page) first"
		}
		if !s.Deploy.HasHelper() {
			return "rerun the installer to let the agent use tailscale serve"
		}
	case "", deploy.AccessPublic, deploy.AccessVPN:
		if in.Domain != "" && !s.Deploy.SitesEnabled() {
			return deploy.ErrNoDomain.Error()
		}
	}
	return ""
}

// sealEnv validates and encrypts variables, keeping existing secrets marked keep.
func (s *Server) sealEnv(ctx context.Context, appID string, in []envIn) ([]store.EnvVar, string) {
	old := map[string]store.EnvVar{}
	if appID != "" {
		cur, _ := s.Store.Env(ctx, appID)
		for _, e := range cur {
			old[e.Key] = e
		}
	}
	seen := map[string]bool{}
	out := make([]store.EnvVar, 0, len(in))
	for _, e := range in {
		e.Key = strings.TrimSpace(e.Key)
		if e.Key == "" {
			continue
		}
		if !deploy.ValidEnvKey(e.Key) {
			return nil, "invalid variable name " + e.Key
		}
		if seen[e.Key] {
			return nil, "duplicate variable " + e.Key
		}
		seen[e.Key] = true
		if e.Keep {
			if prev, ok := old[e.Key]; ok {
				out = append(out, store.EnvVar{Key: e.Key, Value: prev.Value, Secret: e.Secret || prev.Secret})
				continue
			}
		}
		out = append(out, store.EnvVar{Key: e.Key, Value: s.Box.Seal(e.Value), Secret: e.Secret})
	}
	return out, ""
}

func (s *Server) appCreate(w http.ResponseWriter, r *http.Request) {
	var in appInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !deploy.AppID.MatchString(in.ID) {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "the name must be 2 to 40 lowercase letters, digits or dashes")
		return
	}
	if msg := in.validate(); msg != "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", msg)
		return
	}
	if msg := s.accessError(r.Context(), &in); msg != "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", msg)
		return
	}
	// The repository must be one the App can read; that also gives its installation.
	repo, inst, err := s.readRepo(r.Context(), in.Repo)
	if err != nil {
		writeErrorMsg(w, githubStatusCode(err), "github_failed", err.Error())
		return
	}
	if inst == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "the GitHub App has no access to "+in.Repo)
		return
	}
	in.Repo = repo
	env, msg := s.sealEnv(r.Context(), "", in.Env)
	if msg != "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", msg)
		return
	}
	a := &store.App{ID: in.ID, Repo: in.Repo, InstallationID: inst, Branch: in.Branch, RootDir: in.RootDir, Build: in.Build,
		Port: in.Port, Domain: in.Domain, AutoDeploy: in.AutoDeploy, Keep: in.Keep, MemoryMB: in.MemoryMB,
		InstallCmd: strings.TrimSpace(in.InstallCmd), BuildCmd: strings.TrimSpace(in.BuildCmd), StartCmd: strings.TrimSpace(in.StartCmd),
		Dockerfile: strings.TrimSpace(in.Dockerfile), ComposeFile: strings.TrimSpace(in.ComposeFile),
		Access: in.Access, Allow: strings.TrimSpace(in.Allow)}
	if in.EnvFile != nil {
		a.EnvFile = *in.EnvFile
	}
	if in.EnvPath != nil {
		a.EnvPath = *in.EnvPath
	}
	err = s.Store.AddApp(r.Context(), a)
	if errors.Is(err, store.ErrExists) {
		writeErrorMsg(w, http.StatusConflict, "exists", "an app with this name already exists")
		return
	}
	if err == nil {
		err = s.Store.SetEnv(r.Context(), a.ID, env)
	}
	s.audit(r, "app.create", a.ID, err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	res := map[string]any{"app": a}
	if in.Deploy {
		d, err := s.Deploy.Deploy(context.WithoutCancel(r.Context()), *a, "create", "", "", "")
		if err == nil {
			res["deployment"] = d
		}
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) appGet(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	vars, _ := s.Store.Env(r.Context(), a.ID)
	env := make([]envView, 0, len(vars))
	for _, v := range vars {
		ev := envView{Key: v.Key, Secret: v.Secret, HasValue: v.Value != ""}
		if !v.Secret {
			ev.Value, _ = s.Box.Open(v.Value)
		}
		env = append(env, ev)
	}
	deps, _ := s.Store.Deployments(r.Context(), a.ID, 30)
	writeJSON(w, http.StatusOK, map[string]any{"app": s.appView(r.Context(), *a), "env": env, "deployments": deps, "domains": s.Deploy.SitesEnabled()})
}

func (s *Server) appUpdate(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	var in appInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if msg := in.validate(); msg != "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", msg)
		return
	}
	if msg := s.accessError(r.Context(), &in); msg != "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", msg)
		return
	}
	// A repository can be renamed or moved to another account (for example from a user to
	// an organization). Follow its new installation when the source changes, so pushes and
	// deployments read the right repository.
	if in.Repo != "" && !strings.EqualFold(in.Repo, a.Repo) {
		repo, inst, err := s.readRepo(r.Context(), in.Repo)
		if err != nil {
			writeErrorMsg(w, githubStatusCode(err), "github_failed", err.Error())
			return
		}
		if inst == 0 {
			writeErrorMsg(w, http.StatusBadRequest, "invalid", "the GitHub App has no access to "+in.Repo)
			return
		}
		a.Repo, a.InstallationID = repo, inst
	}
	// The build type is fixed once the app runs: switching between compose and a single
	// container would leave the old one behind.
	if a.Build != in.Build && a.Build != deploy.BuildAuto {
		if _, err := s.Store.LiveDeployment(r.Context(), a.ID); err == nil {
			writeErrorMsg(w, http.StatusBadRequest, "invalid", "delete and recreate the app to change its build type")
			return
		}
	}
	a.Branch, a.RootDir, a.Build, a.Port, a.Domain = in.Branch, in.RootDir, in.Build, in.Port, in.Domain
	a.AutoDeploy, a.Keep, a.MemoryMB = in.AutoDeploy, in.Keep, in.MemoryMB
	a.InstallCmd, a.BuildCmd, a.StartCmd = strings.TrimSpace(in.InstallCmd), strings.TrimSpace(in.BuildCmd), strings.TrimSpace(in.StartCmd)
	a.Dockerfile, a.ComposeFile = strings.TrimSpace(in.Dockerfile), strings.TrimSpace(in.ComposeFile)
	a.Access, a.Allow = in.Access, strings.TrimSpace(in.Allow)
	if in.EnvFile != nil {
		a.EnvFile = *in.EnvFile
	}
	if in.EnvPath != nil {
		a.EnvPath = *in.EnvPath
	}
	if a.Dir != "" {
		a.Build = deploy.BuildCompose // a linked folder is always a compose stack
	}
	err := s.Store.UpdateApp(r.Context(), a)
	s.audit(r, "app.update", a.ID, err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app": a})
}

func (s *Server) appSetEnv(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	var in struct {
		Env     []envIn `json:"env"`
		EnvFile *bool   `json:"envFile"`
		EnvPath *string `json:"envPath"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if in.EnvPath != nil {
		p := strings.TrimSpace(*in.EnvPath)
		if !deploy.ValidEnvPath(p) {
			writeErrorMsg(w, http.StatusBadRequest, "invalid", "the .env path must be a file inside the app's folder")
			return
		}
		a.EnvPath = p
	}
	if in.EnvFile != nil {
		a.EnvFile = *in.EnvFile
	}
	env, msg := s.sealEnv(r.Context(), a.ID, in.Env)
	if msg != "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", msg)
		return
	}
	var err error
	if in.EnvFile != nil || in.EnvPath != nil {
		err = s.Store.UpdateApp(r.Context(), a)
	}
	if err == nil {
		err = s.Store.SetEnv(r.Context(), a.ID, env)
	}
	s.audit(r, "app.env", a.ID, err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) appDelete(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	err := s.Deploy.Remove(r.Context(), *a, r.URL.Query().Get("volumes") == "1")
	if err == nil {
		err = s.Store.DeleteApp(r.Context(), a.ID)
	}
	s.audit(r, "app.delete", a.ID, err)
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) appDeploy(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	d, err := s.Deploy.Deploy(context.WithoutCancel(r.Context()), *a, "manual", "", "", "")
	s.audit(r, "app.deploy", a.ID, err)
	if err != nil {
		writeErrorMsg(w, http.StatusConflict, "deploy_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, d)
}

func (s *Server) appRollback(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("dep"), 10, 64)
	target, err := s.Store.Deployment(r.Context(), id)
	if err != nil || target.AppID != a.ID || target.SHA == "" || (target.Status != "superseded" && target.Status != "live") {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "this deployment cannot be restored")
		return
	}
	image := target.Image
	if a.Build == deploy.BuildCompose {
		image = "" // compose rebuilds from the old commit
	}
	d, err := s.Deploy.Deploy(context.WithoutCancel(r.Context()), *a, "rollback", target.SHA, target.Message, image)
	s.audit(r, "app.rollback", a.ID, err)
	if err != nil {
		writeErrorMsg(w, http.StatusConflict, "deploy_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, d)
}

func (s *Server) appRestart(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	err := s.Deploy.Restart(r.Context(), *a)
	s.audit(r, "app.restart", a.ID, err)
	if err != nil {
		writeErrorMsg(w, http.StatusConflict, "restart_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deploymentLog(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("dep"), 10, 64)
	d, err := s.Store.Deployment(r.Context(), id)
	if err != nil || d.AppID != r.PathValue("id") {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	log, _ := s.Store.DeploymentLog(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"deployment": d, "log": log})
}

// deploymentStream sends the stored log, then live output until the deployment ends.
func (s *Server) deploymentStream(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("dep"), 10, 64)
	d, err := s.Store.Deployment(r.Context(), id)
	if err != nil || d.AppID != r.PathValue("id") {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Deploy.Subscribe(id)
	defer cancel()
	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	log, _ := s.Store.DeploymentLog(r.Context(), id)
	if !send("log", log) {
		return
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk := <-ch:
			if !send("log", chunk) {
				return
			}
		case <-tick.C:
			cur, err := s.Store.Deployment(r.Context(), id)
			if err != nil {
				return
			}
			if !send("status", cur) {
				return
			}
			if cur.FinishedAt != nil {
				// drain what the logger flushed last
				for {
					select {
					case chunk := <-ch:
						send("log", chunk)
						continue
					default:
					}
					break
				}
				send("end", cur)
				return
			}
		}
	}
}

// composeFolder returns the working folder of a compose project running on this server.
// Only such folders can be linked: the panel never touches arbitrary paths.
func (s *Server) composeFolder(ctx context.Context, project string) (string, error) {
	list, err := s.Docker.Containers(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range list {
		if c.Project == project && c.WorkDir != "" {
			return c.WorkDir, nil
		}
	}
	return "", errors.New("no compose project with this name runs on the server")
}

// folderInspect tells which repository and branch a compose folder follows.
func (s *Server) folderInspect(w http.ResponseWriter, r *http.Request) {
	dir, err := s.composeFolder(r.Context(), r.URL.Query().Get("project"))
	if err != nil {
		writeErrorMsg(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	info, err := s.Deploy.InspectFolder(r.Context(), dir)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "not_git", err.Error()+": clone the repository there first")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dir": dir, "folder": info})
}

// appLink makes an existing compose folder update from GitHub, like a panel app.
func (s *Server) appLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project    string `json:"project"`
		Repo       string `json:"repo"`
		Branch     string `json:"branch"`
		RootDir    string `json:"rootDir"`
		AutoDeploy bool   `json:"autoDeploy"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	dir, err := s.composeFolder(r.Context(), in.Project)
	if err != nil {
		writeErrorMsg(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if _, err := s.Deploy.InspectFolder(r.Context(), dir); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "not_git", err.Error())
		return
	}
	if !deploy.AppID.MatchString(in.Project) || strings.TrimSpace(in.Branch) == "" || strings.Contains(in.RootDir, "..") {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "invalid project, branch or root folder")
		return
	}
	repo, inst, err := s.readRepo(r.Context(), in.Repo)
	if err != nil {
		writeErrorMsg(w, githubStatusCode(err), "github_failed", err.Error())
		return
	}
	if inst == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "the GitHub App has no access to "+in.Repo+": add it on GitHub first")
		return
	}
	in.Repo = repo
	a := &store.App{ID: in.Project, Repo: in.Repo, InstallationID: inst, Branch: in.Branch, RootDir: strings.Trim(in.RootDir, "/"),
		Build: deploy.BuildCompose, AutoDeploy: in.AutoDeploy, Keep: 5, Dir: dir}
	err = s.Store.AddApp(r.Context(), a)
	if errors.Is(err, store.ErrExists) {
		writeErrorMsg(w, http.StatusConflict, "exists", "this project is already linked")
		return
	}
	s.audit(r, "app.link", a.ID+" "+dir, err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"app": a})
}

// appCheck compares what runs with the head of the branch on GitHub.
func (s *Server) appCheck(w http.ResponseWriter, r *http.Request) {
	a := s.loadApp(w, r)
	if a == nil {
		return
	}
	c, err := s.GitHub.Commit(r.Context(), a.InstallationID, a.Repo, a.Branch)
	if err != nil {
		writeErrorMsg(w, githubStatusCode(err), "github_failed", err.Error())
		return
	}
	current := s.Deploy.CurrentCommit(r.Context(), *a)
	writeJSON(w, http.StatusOK, map[string]any{"current": current, "latest": c, "upToDate": current != "" && current == c.SHA})
}

// rebuilding holds the containers being rebuilt, one rebuild at a time each.
var rebuilding sync.Map

// containerRebuild pulls or rebuilds the compose service of one container and recreates
// it, streaming the output as plain text. It goes on if the browser leaves.
func (s *Server) containerRebuild(w http.ResponseWriter, r *http.Request) {
	list, err := s.Docker.Containers(r.Context())
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "docker", err.Error())
		return
	}
	id := r.PathValue("id")
	var c *docker.Container
	for i := range list {
		if list[i].ID == id || list[i].Name == id {
			c = &list[i]
		}
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if c.Project == "" || c.WorkDir == "" || c.Service == "" {
		writeErrorMsg(w, http.StatusBadRequest, "not_compose", "only containers started by docker compose can be rebuilt")
		return
	}
	if _, busy := rebuilding.LoadOrStore(c.Project+"/"+c.Service, true); busy {
		writeErrorMsg(w, http.StatusConflict, "busy", "this container is already being rebuilt")
		return
	}
	defer rebuilding.Delete(c.Project + "/" + c.Service)
	// The compose files it was started with, when they sit in its folder (the only
	// folder the compose container sees).
	var files []string
	for _, f := range strings.Split(c.Labels["com.docker.compose.project.config_files"], ",") {
		if f = strings.TrimSpace(f); f != "" && strings.HasPrefix(f, c.WorkDir+"/") {
			files = append(files, f)
		}
	}

	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	out := &flushWriter{w: w, rc: rc}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	err = s.Deploy.RebuildService(ctx, out, c.Project, c.WorkDir, files, c.Service)
	s.audit(r, "container.rebuild", c.Name, err)
	if err != nil {
		fmt.Fprintf(out, "\n==> FAILED: %v\n", err)
		return
	}
	fmt.Fprintln(out, "\n==> Done")
}

// flushWriter sends each write right away and ignores a gone client, so the command
// it serves keeps running to the end.
type flushWriter struct {
	mu   sync.Mutex
	w    io.Writer
	rc   *http.ResponseController
	gone bool
}

func (f *flushWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.gone {
		if _, err := f.w.Write(p); err != nil {
			f.gone = true
		} else {
			f.rc.Flush()
		}
	}
	return len(p), nil
}
