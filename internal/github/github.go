// Package github links a server to GitHub through a GitHub App created from the panel
// with the manifest flow: one click, read-only access to the repositories the user picks,
// and push webhooks for automatic deploys. Each server gets its own App, so nothing is
// shared between installs.
package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotConnected = errors.New("GitHub is not connected")
	ErrSignature    = errors.New("invalid webhook signature")
)

// Creds are what GitHub returns when the App is created. Stored sealed.
type Creds struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	HTMLURL       string `json:"html_url"`
	Owner         string `json:"owner"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
}

type Client struct {
	API  string // https://api.github.com, changed by tests
	HTTP *http.Client

	mu     sync.Mutex
	creds  *Creds
	key    *rsa.PrivateKey
	tokens map[int64]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

func New() *Client {
	return &Client{API: "https://api.github.com", HTTP: &http.Client{Timeout: 60 * time.Second}, tokens: map[int64]cachedToken{}}
}

// SetCreds installs (or with nil, removes) the App credentials.
func (c *Client) SetCreds(cr *Creds) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens = map[int64]cachedToken{}
	if cr == nil {
		c.creds, c.key = nil, nil
		return nil
	}
	block, _ := pem.Decode([]byte(cr.PEM))
	if block == nil {
		return errors.New("invalid App private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k8, err8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		rk, ok := k8.(*rsa.PrivateKey)
		if err8 != nil || !ok {
			return fmt.Errorf("App private key: %w", err)
		}
		key = rk
	}
	c.creds, c.key = cr, key
	return nil
}

func (c *Client) Creds() *Creds {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.creds
}

// Manifest describes the App GitHub creates for this server.
// webhookURL is where GitHub can reach the server, which may differ from the panel's
// own (private) address.
func Manifest(publicURL, webhookURL, name string) map[string]any {
	if webhookURL == "" {
		webhookURL = publicURL
	}
	return map[string]any{
		"name": name,
		"url":  publicURL,
		"hook_attributes": map[string]any{
			"url":    webhookURL + "/api/github/webhook",
			"active": true,
		},
		"redirect_url":    publicURL + "/api/github/manifest/callback",
		"setup_url":       publicURL + "/apps?github=installed",
		"setup_on_update": true,
		// Public so the install page offers every account and organization the user
		// belongs to. Installing grants this server read access; it cannot deploy
		// anything that is not set up in this panel.
		"public":              true,
		"default_permissions": map[string]string{"contents": "read", "metadata": "read"},
		"default_events":      []string{"push"},
	}
}

// Convert exchanges the one-time code from the manifest redirect for the App credentials.
func (c *Client) Convert(ctx context.Context, code string) (*Creds, error) {
	body, err := c.call(ctx, http.MethodPost, "/app-manifests/"+url.PathEscape(code)+"/conversions", "", nil)
	if err != nil {
		return nil, err
	}
	// Same fields as Creds, except owner, which GitHub sends as an object.
	var raw struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		Name          string `json:"name"`
		HTMLURL       string `json:"html_url"`
		PEM           string `json:"pem"`
		WebhookSecret string `json:"webhook_secret"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return &Creds{ID: raw.ID, Slug: raw.Slug, Name: raw.Name, HTMLURL: raw.HTMLURL, Owner: raw.Owner.Login,
		PEM: raw.PEM, WebhookSecret: raw.WebhookSecret, ClientID: raw.ClientID, ClientSecret: raw.ClientSecret}, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// appJWT authenticates as the App itself (RS256, valid 9 minutes).
func (c *Client) appJWT() (string, error) {
	c.mu.Lock()
	cr, key := c.creds, c.key
	c.mu.Unlock()
	if cr == nil {
		return "", ErrNotConnected
	}
	now := time.Now()
	header := b64([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": strconv.FormatInt(cr.ID, 10)})
	signing := header + "." + b64(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + b64(sig), nil
}

// Token returns an installation token, cached until a minute before it expires.
func (c *Client) Token(ctx context.Context, installation int64) (string, error) {
	c.mu.Lock()
	t, ok := c.tokens[installation]
	c.mu.Unlock()
	if ok && time.Until(t.expires) > time.Minute {
		return t.token, nil
	}
	jwt, err := c.appJWT()
	if err != nil {
		return "", err
	}
	body, err := c.call(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installation), "Bearer "+jwt, nil)
	if err != nil {
		return "", err
	}
	var res struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.tokens[installation] = cachedToken{res.Token, res.ExpiresAt}
	c.mu.Unlock()
	return res.Token, nil
}

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("GitHub: %s (HTTP %d)", e.Message, e.Status) }

func (c *Client) request(ctx context.Context, method, path, auth string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	u := path
	if strings.HasPrefix(path, "/") {
		u = c.API + path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "vps-admin")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		var e struct {
			Message string `json:"message"`
		}
		json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&e)
		return nil, &APIError{res.StatusCode, e.Message}
	}
	return res, nil
}

func (c *Client) call(ctx context.Context, method, path, auth string, body any) ([]byte, error) {
	res, err := c.request(ctx, method, path, auth, body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(io.LimitReader(res.Body, 16<<20))
}

func (c *Client) asInstallation(ctx context.Context, installation int64, path string, v any) error {
	tok, err := c.Token(ctx, installation)
	if err != nil {
		return err
	}
	body, err := c.call(ctx, http.MethodGet, path, "token "+tok, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

type Installation struct {
	ID      int64  `json:"id"`
	Account string `json:"account"`
	Type    string `json:"type"` // User | Organization
	HTMLURL string `json:"htmlUrl"`
}

func (c *Client) Installations(ctx context.Context) ([]Installation, error) {
	jwt, err := c.appJWT()
	if err != nil {
		return nil, err
	}
	body, err := c.call(ctx, http.MethodGet, "/app/installations?per_page=100", "Bearer "+jwt, nil)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make([]Installation, 0, len(raw))
	for _, r := range raw {
		out = append(out, Installation{ID: r.ID, Account: r.Account.Login, Type: r.Account.Type, HTMLURL: r.HTMLURL})
	}
	return out, nil
}

type Repo struct {
	FullName       string    `json:"fullName"`
	Private        bool      `json:"private"`
	DefaultBranch  string    `json:"defaultBranch"`
	Language       string    `json:"language"`
	PushedAt       time.Time `json:"pushedAt"`
	InstallationID int64     `json:"installationId"`
}

// Repos lists every repository the App can read, across installations.
func (c *Client) Repos(ctx context.Context) ([]Repo, error) {
	insts, err := c.Installations(ctx)
	if err != nil {
		return nil, err
	}
	out := []Repo{}
	for _, in := range insts {
		for page := 1; page <= 10; page++ {
			var res struct {
				Repositories []struct {
					FullName      string    `json:"full_name"`
					Private       bool      `json:"private"`
					DefaultBranch string    `json:"default_branch"`
					Language      string    `json:"language"`
					PushedAt      time.Time `json:"pushed_at"`
				} `json:"repositories"`
			}
			if err := c.asInstallation(ctx, in.ID, fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), &res); err != nil {
				return nil, err
			}
			for _, r := range res.Repositories {
				out = append(out, Repo{r.FullName, r.Private, r.DefaultBranch, r.Language, r.PushedAt, in.ID})
			}
			if len(res.Repositories) < 100 {
				break
			}
		}
	}
	return out, nil
}

// Files lists the names at path in the repository at ref, directories with a trailing "/".
func (c *Client) Files(ctx context.Context, installation int64, repo, ref, path string) ([]string, error) {
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	p := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, strings.Trim(path, "/"), url.QueryEscape(ref))
	if err := c.asInstallation(ctx, installation, p, &entries); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type == "dir" {
			e.Name += "/"
		}
		out = append(out, e.Name)
	}
	return out, nil
}

// File returns a small file of the repository, or "" when it does not exist.
func (c *Client) File(ctx context.Context, installation int64, repo, ref, path string) (string, error) {
	var f struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	err := c.asInstallation(ctx, installation, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, strings.Trim(path, "/"), url.QueryEscape(ref)), &f)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(f.Content, "\n", ""))
	return string(b), err
}

type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

func (c *Client) Commit(ctx context.Context, installation int64, repo, ref string) (*Commit, error) {
	var res struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := c.asInstallation(ctx, installation, fmt.Sprintf("/repos/%s/commits/%s", repo, url.PathEscape(ref)), &res); err != nil {
		return nil, err
	}
	msg, _, _ := strings.Cut(res.Commit.Message, "\n")
	return &Commit{SHA: res.SHA, Message: msg}, nil
}

// Branches lists the branch names of repo (the first 300, enough for a picker).
func (c *Client) Branches(ctx context.Context, installation int64, repo string) ([]string, error) {
	var out []string
	for page := 1; page <= 3; page++ {
		var res []struct {
			Name string `json:"name"`
		}
		if err := c.asInstallation(ctx, installation, fmt.Sprintf("/repos/%s/branches?per_page=100&page=%d", repo, page), &res); err != nil {
			return nil, err
		}
		for _, b := range res {
			out = append(out, b.Name)
		}
		if len(res) < 100 {
			break
		}
	}
	return out, nil
}

// Tarball streams the gzipped source of repo at sha. The caller closes it.
func (c *Client) Tarball(ctx context.Context, installation int64, repo, sha string) (io.ReadCloser, error) {
	tok, err := c.Token(ctx, installation)
	if err != nil {
		return nil, err
	}
	res, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/tarball/%s", repo, url.PathEscape(sha)), "token "+tok, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// VerifyWebhook checks the X-Hub-Signature-256 header against the App secret.
func (c *Client) VerifyWebhook(body []byte, header string) error {
	cr := c.Creds()
	if cr == nil {
		return ErrNotConnected
	}
	got, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return ErrSignature
	}
	sig, err := hex.DecodeString(got)
	if err != nil {
		return ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(cr.WebhookSecret))
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ErrSignature
	}
	return nil
}

type Push struct {
	Repo    string
	Branch  string
	SHA     string
	Message string
	Deleted bool
}

// ParsePush reads a push event payload.
func ParsePush(body []byte) (*Push, error) {
	var p struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		HeadCommit *struct {
			Message string `json:"message"`
		} `json:"head_commit"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
	if !ok {
		return nil, errors.New("not a branch push")
	}
	out := &Push{Repo: p.Repository.FullName, Branch: branch, SHA: p.After, Deleted: p.Deleted}
	if p.HeadCommit != nil {
		out.Message, _, _ = strings.Cut(p.HeadCommit.Message, "\n")
	}
	return out, nil
}
