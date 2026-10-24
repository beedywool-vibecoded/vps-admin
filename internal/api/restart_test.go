package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCleanContainerIDs(t *testing.T) {
	got, err := cleanContainerIDs([]string{" abc ", "abc", "", "def"})
	if err != nil || len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range [][]string{nil, {""}, {"../etc"}, {"a b"}, {"a/b"}} {
		if _, err := cleanContainerIDs(bad); err == nil {
			t.Errorf("%q should have been refused", bad)
		}
	}
}

func TestRestartPoliciesNeedsIDs(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.containerRestartPolicies(rec, httptest.NewRequest("GET", "/api/docker/restart", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 without ids, got %d", rec.Code)
	}
}

func TestRestartSetRefusesBadRequests(t *testing.T) {
	s := &Server{}
	for _, body := range []string{
		`{"ids":["abc"]}`,
		`{"ids":[],"start":true}`,
		`{"ids":["../etc"],"start":true}`,
	} {
		rec := httptest.NewRecorder()
		s.containerRestartSet(rec, httptest.NewRequest("POST", "/api/docker/restart", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
}
