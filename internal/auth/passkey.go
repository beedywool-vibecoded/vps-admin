package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/store"
)

const ceremonyTTL = 5 * time.Minute

var ErrCeremony = errors.New("passkey ceremony expired, start again")

// There is a single administrator per server, so every passkey belongs to one WebAuthn
// user. Each passkey is also tied to the device that registered it, which is what a
// login session is attached to and what revoking removes.
type owner struct {
	id    []byte
	creds []webauthn.Credential
}

func (o *owner) WebAuthnID() []byte                         { return o.id }
func (o *owner) WebAuthnName() string                       { return "admin" }
func (o *owner) WebAuthnDisplayName() string                { return "VPS Admin" }
func (o *owner) WebAuthnCredentials() []webauthn.Credential { return o.creds }

type ceremony struct {
	data     webauthn.SessionData
	deviceID string
	expires  time.Time
}

type passkeys struct {
	wa    *webauthn.WebAuthn
	store *store.Store

	mu         sync.Mutex
	ceremonies map[string]*ceremony
}

func newPasskeys(cfg *config.Config, st *store.Store) (*passkeys, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID(),
		RPDisplayName: "VPS Admin",
		RPOrigins:     []string{cfg.PublicURL},
	})
	if err != nil {
		return nil, err
	}
	return &passkeys{wa: wa, store: st, ceremonies: map[string]*ceremony{}}, nil
}

func (p *passkeys) owner(ctx context.Context) (*owner, map[string]string, error) {
	uid := p.store.Setting(ctx, "webauthn_user_id", "")
	if uid == "" {
		b := make([]byte, 32)
		rand.Read(b)
		uid = base64.RawURLEncoding.EncodeToString(b)
		if err := p.store.SetSetting(ctx, "webauthn_user_id", uid); err != nil {
			return nil, nil, err
		}
	}
	id, _ := base64.RawURLEncoding.DecodeString(uid)
	rows, err := p.store.Passkeys(ctx)
	if err != nil {
		return nil, nil, err
	}
	o := &owner{id: id}
	devices := map[string]string{} // credential id -> device id
	for _, r := range rows {
		var c webauthn.Credential
		if json.Unmarshal([]byte(r.Credential), &c) != nil {
			continue
		}
		o.creds = append(o.creds, c)
		devices[string(c.ID)] = r.DeviceID
	}
	return o, devices, nil
}

func (p *passkeys) put(c *ceremony) string {
	id := randomString(18)
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, v := range p.ceremonies {
		if now.After(v.expires) {
			delete(p.ceremonies, k)
		}
	}
	c.expires = now.Add(ceremonyTTL)
	p.ceremonies[id] = c
	return id
}

func (p *passkeys) take(id string) (*ceremony, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.ceremonies[id]
	delete(p.ceremonies, id)
	if !ok || time.Now().After(c.expires) {
		return nil, ErrCeremony
	}
	return c, nil
}

// Options is what the browser passes to navigator.credentials.create/get, plus the id
// to send back with the result.
type Options struct {
	Ceremony  string `json:"ceremony"`
	PublicKey any    `json:"publicKey"`
}

func (a *Auth) PasskeyRegisterBegin(ctx context.Context, id *Identity) (*Options, error) {
	o, _, err := a.passkeys.owner(ctx)
	if err != nil {
		return nil, err
	}
	excl := make([]protocol.CredentialDescriptor, 0, len(o.creds))
	for _, c := range o.creds {
		excl = append(excl, c.Descriptor())
	}
	creation, data, err := a.passkeys.wa.BeginRegistration(o,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(excl),
	)
	if err != nil {
		return nil, err
	}
	cid := a.passkeys.put(&ceremony{data: *data, deviceID: id.DeviceID})
	return &Options{Ceremony: cid, PublicKey: creation.Response}, nil
}

// PasskeyRegisterFinish stores the new passkey. An enroll session is upgraded to a full one.
func (a *Auth) PasskeyRegisterFinish(ctx context.Context, w http.ResponseWriter, r *http.Request, id *Identity, ceremonyID string) error {
	c, err := a.passkeys.take(ceremonyID)
	if err != nil {
		return err
	}
	if c.deviceID != id.DeviceID {
		return ErrCeremony
	}
	o, _, err := a.passkeys.owner(ctx)
	if err != nil {
		return err
	}
	cred, err := a.passkeys.wa.FinishRegistration(o, c.data, r)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(cred)
	if err := a.store.AddPasskey(ctx, cred.ID, id.DeviceID, string(b)); err != nil {
		return err
	}
	if id.Scope == ScopeEnroll {
		a.store.DeleteSession(ctx, hash(id.session))
		return a.startSession(ctx, w, id.DeviceID, ScopeFull, SessionTTL)
	}
	return nil
}

func (a *Auth) PasskeyLoginBegin(ctx context.Context) (*Options, error) {
	assertion, data, err := a.passkeys.wa.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, err
	}
	cid := a.passkeys.put(&ceremony{data: *data})
	return &Options{Ceremony: cid, PublicKey: assertion.Response}, nil
}

// PasskeyLoginFinish verifies the assertion and opens a full session for the device that
// owns the passkey. It returns that device id.
func (a *Auth) PasskeyLoginFinish(ctx context.Context, w http.ResponseWriter, r *http.Request, ip, ceremonyID string) (string, error) {
	if !a.limiter.allow(ip) {
		return "", ErrRateLimited
	}
	c, err := a.passkeys.take(ceremonyID)
	if err != nil {
		return "", err
	}
	o, devices, err := a.passkeys.owner(ctx)
	if err != nil {
		return "", err
	}
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		if !bytes.Equal(userHandle, o.id) {
			return nil, errors.New("unknown user")
		}
		return o, nil
	}
	_, cred, err := a.passkeys.wa.FinishPasskeyLogin(handler, c.data, r)
	if err != nil {
		a.limiter.fail(ip)
		return "", err
	}
	deviceID, ok := devices[string(cred.ID)]
	if !ok {
		a.limiter.fail(ip)
		return "", errors.New("unknown passkey")
	}
	b, _ := json.Marshal(cred)
	a.store.UpdatePasskey(ctx, cred.ID, string(b))
	a.limiter.reset(ip)
	return deviceID, a.startSession(ctx, w, deviceID, ScopeFull, SessionTTL)
}
