package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/store"
)

func newAuth(t *testing.T) *Auth {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, err := New(&cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPairingCodeFlow(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	old, _ := a.NewPairingCode(ctx)
	code, _ := a.NewPairingCode(ctx)
	if len(code) != 11 || strings.Count(code, "-") != 2 {
		t.Fatalf("unexpected code format %q", code)
	}
	w := httptest.NewRecorder()
	if _, err := a.Pair(ctx, w, "1.2.3.4", old, "x", "Sam", KindApp); !errors.Is(err, ErrBadCode) {
		t.Fatalf("a replaced code still works: %v", err)
	}
	// Lower case and missing dashes are accepted.
	res, err := a.Pair(ctx, w, "1.2.3.4", strings.ToLower(strings.ReplaceAll(code, "-", "")), "Pixel", "Sam", KindApp)
	if err != nil || !strings.HasPrefix(res.Token, tokenPrefix) {
		t.Fatalf("pair: %+v, %v", res, err)
	}
	if _, err := a.Pair(ctx, w, "1.2.3.4", code, "again", "Sam", KindApp); !errors.Is(err, ErrBadCode) {
		t.Fatalf("code reused: %v", err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+res.Token)
	if id := a.Identify(r); id == nil || id.Scope != ScopeFull || id.DeviceID != res.DeviceID {
		t.Fatalf("token not accepted: %+v", id)
	}
	a.store.RevokeDevice(ctx, res.DeviceID)
	if a.Identify(r) != nil {
		t.Fatal("revoked token still accepted")
	}
}

func TestWebPairingGivesEnrollSession(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	code, _ := a.NewPairingCode(ctx)
	w := httptest.NewRecorder()
	res, err := a.Pair(ctx, w, "1.2.3.4", code, "Firefox", "Sam", KindWeb)
	if err != nil || res.Token != "" {
		t.Fatalf("pair: %+v, %v", res, err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	if id := a.Identify(r); id == nil || id.Scope != ScopeEnroll {
		t.Fatalf("expected an enroll session, got %+v", id)
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(3, time.Minute, 5)
	now := time.Unix(1000, 0)
	l.nowFunc = func() time.Time { return now }
	for range 3 {
		l.fail("a")
	}
	if l.allow("a") {
		t.Fatal("per IP limit not enforced")
	}
	if !l.allow("b") {
		t.Fatal("other IP blocked")
	}
	l.fail("b")
	l.fail("c")
	if l.allow("d") {
		t.Fatal("global limit not enforced")
	}
	now = now.Add(2 * time.Minute)
	if !l.allow("a") {
		t.Fatal("window did not reset")
	}
}
