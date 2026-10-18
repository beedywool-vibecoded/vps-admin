// Package auth implements device pairing, app tokens, web sessions and passkeys.
//
// Trust model: whoever can run `vps-admin pair` as root on the server can enroll a device.
// The code is single use, expires after 10 minutes and only the newest one is valid.
// An app device then gets a long-lived bearer token. A browser gets a short "enroll"
// session that can only register a passkey; every later web login needs that passkey.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/store"
)

const (
	PairingTTL      = 10 * time.Minute
	EnrollTTL       = 10 * time.Minute
	SessionTTL      = 12 * time.Hour
	ScopeFull       = "full"
	ScopeEnroll     = "enroll"
	KindWeb         = "web"
	KindApp         = "app"
	tokenPrefix     = "vpa_"
	codeAlphabet    = "23456789ABCDEFGHJKMNPQRSTUVWXYZ" // no 0/O, 1/I/L
	codeLen         = 9
	csrfHeader      = "X-VPS-Admin"
	maxDeviceName   = 60
	sessionCookieV1 = "vpsadmin_session"
)

var (
	ErrBadCode     = errors.New("invalid or expired pairing code")
	ErrRateLimited = errors.New("too many attempts, try again later")
)

type Auth struct {
	cfg      *config.Config
	store    *store.Store
	limiter  *limiter
	passkeys *passkeys
}

func New(cfg *config.Config, st *store.Store) (*Auth, error) {
	pk, err := newPasskeys(cfg, st)
	if err != nil {
		return nil, err
	}
	return &Auth{cfg: cfg, store: st, limiter: newLimiter(5, 15*time.Minute, 30), passkeys: pk}, nil
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func newID() string { return randomString(12) }

// NewPairingCode creates a code like "K7Q-4ZP-9MD" and revokes the previous ones.
func (a *Auth) NewPairingCode(ctx context.Context) (string, error) {
	b := make([]byte, codeLen)
	rand.Read(b)
	code := make([]byte, 0, codeLen+2)
	for i, v := range b {
		if i > 0 && i%3 == 0 {
			code = append(code, '-')
		}
		// 256 % 31 leaves a tiny bias, irrelevant for a 10 minute single-use code.
		code = append(code, codeAlphabet[int(v)%len(codeAlphabet)])
	}
	c := string(code)
	return c, a.store.AddPairingCode(ctx, hash(normalizeCode(c)), PairingTTL)
}

func normalizeCode(c string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(c) {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func cleanName(name, fallback string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return fallback
	}
	if r := []rune(name); len(r) > maxDeviceName {
		name = string(r[:maxDeviceName])
	}
	return name
}

type PairResult struct {
	DeviceID string `json:"deviceId"`
	Token    string `json:"token,omitempty"` // app devices only, shown once
}

// Pair redeems a code. For an app it returns a bearer token; for a browser it sets an
// enroll session cookie on w.
func (a *Auth) Pair(ctx context.Context, w http.ResponseWriter, ip, code, name, kind string) (*PairResult, error) {
	if !a.limiter.allow(ip) {
		return nil, ErrRateLimited
	}
	ok, err := a.store.UsePairingCode(ctx, hash(normalizeCode(code)))
	if err != nil {
		return nil, err
	}
	if !ok {
		a.limiter.fail(ip)
		return nil, ErrBadCode
	}
	a.limiter.reset(ip)

	res := &PairResult{DeviceID: newID()}
	var tokenHash []byte
	switch kind {
	case KindApp:
		res.Token = tokenPrefix + randomString(32)
		tokenHash = hash(res.Token)
	case KindWeb:
	default:
		return nil, errors.New("kind must be web or app")
	}
	if err := a.store.AddDevice(ctx, res.DeviceID, cleanName(name, "Unnamed device"), kind, tokenHash, ip); err != nil {
		return nil, err
	}
	if kind == KindWeb {
		if err := a.startSession(ctx, w, res.DeviceID, ScopeEnroll, EnrollTTL); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func (a *Auth) cookieName() string {
	if a.cfg.Secure() {
		return "__Host-" + sessionCookieV1
	}
	return sessionCookieV1
}

func (a *Auth) startSession(ctx context.Context, w http.ResponseWriter, deviceID, scope string, ttl time.Duration) error {
	id := randomString(32)
	if err := a.store.AddSession(ctx, hash(id), deviceID, scope, ttl); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName(),
		Value:    id,
		Path:     "/",
		MaxAge:   int(ttl / time.Second),
		HttpOnly: true,
		Secure:   a.cfg.Secure(),
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (a *Auth) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(a.cookieName()); err == nil {
		a.store.DeleteSession(ctx, hash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.cfg.Secure(), SameSite: http.SameSiteStrictMode})
}

// Identity is who made a request.
type Identity struct {
	DeviceID string
	Scope    string
	Via      string // token | session
	session  string
}

type ctxKey struct{}

func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(ctxKey{}).(*Identity)
	return id
}

// Identify resolves the bearer token or session cookie. It returns nil for anonymous requests.
func (a *Auth) Identify(r *http.Request) *Identity {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimPrefix(h, "Bearer ")
		if !strings.HasPrefix(tok, tokenPrefix) {
			return nil
		}
		id, err := a.store.DeviceByToken(ctx, hash(tok))
		if err != nil {
			return nil
		}
		return &Identity{DeviceID: id, Scope: ScopeFull, Via: "token"}
	}
	c, err := r.Cookie(a.cookieName())
	if err != nil {
		return nil
	}
	s, err := a.store.Session(ctx, hash(c.Value))
	if err != nil {
		return nil
	}
	return &Identity{DeviceID: s.DeviceID, Scope: s.Scope, Via: "session", session: c.Value}
}

// Require wraps h so it only runs for an identity with the given scope. Cookie-based
// requests that change state must also carry the X-VPS-Admin header: a cross-site form
// cannot set it, which closes CSRF on top of SameSite=Strict.
func (a *Auth) Require(scope string, ip func(*http.Request) string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := a.Identify(r)
		if id == nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if scope == ScopeFull && id.Scope != ScopeFull {
			http.Error(w, `{"error":"passkey_required"}`, http.StatusForbidden)
			return
		}
		if id.Via == "session" && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(csrfHeader) == "" {
			http.Error(w, `{"error":"missing_csrf_header"}`, http.StatusForbidden)
			return
		}
		a.store.TouchDevice(r.Context(), id.DeviceID, ip(r))
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

// State describes what the login screen should offer.
type State struct {
	Paired       bool   `json:"paired"`      // at least one device exists
	HasPasskeys  bool   `json:"hasPasskeys"` // passkey login is possible
	Scope        string `json:"scope"`       // scope of the current request, "" if anonymous
	DeviceID     string `json:"deviceId,omitempty"`
	RPID         string `json:"rpId"`
	SecureOrigin bool   `json:"secureOrigin"` // passkeys need https (or localhost)
}

func (a *Auth) State(r *http.Request) (*State, error) {
	n, err := a.store.CountActiveDevices(r.Context())
	if err != nil {
		return nil, err
	}
	pks, err := a.store.Passkeys(r.Context())
	if err != nil {
		return nil, err
	}
	st := &State{Paired: n > 0, HasPasskeys: len(pks) > 0, RPID: a.cfg.RPID(),
		SecureOrigin: a.cfg.Secure() || a.cfg.RPID() == "localhost"}
	if id := a.Identify(r); id != nil {
		st.Scope, st.DeviceID = id.Scope, id.DeviceID
	}
	return st, nil
}
