package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientOrigins(t *testing.T) {
	load := func(origins string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(p, []byte(`{"client_origins": `+origins+`}`), 0o600)
		return Load(p)
	}
	c, err := load(`["https://vps-admin.example.com/", "http://localhost:5173"]`)
	if err != nil {
		t.Fatal(err)
	}
	if !c.AllowsClient("https://vps-admin.example.com") || !c.AllowsClient("http://localhost:5173") || c.AllowsClient("https://other.example.com") {
		t.Fatalf("got %v", c.ClientOrigins)
	}
	for _, bad := range []string{`["http://vps-admin.example.com"]`, `["https://x.example.com/path"]`, `["nope"]`} {
		if _, err := load(bad); err == nil {
			t.Fatalf("%s should be refused", bad)
		}
	}
}
