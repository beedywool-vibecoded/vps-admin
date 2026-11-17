package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLauncherOrigin(t *testing.T) {
	load := func(origin string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(p, []byte(`{"launcher_origin": "`+origin+`", "client_origins": ["https://old.example.com"]}`), 0o600)
		return Load(p)
	}
	// A field older versions wrote is ignored; the origin is normalized
	c, err := load("https://launcher.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if c.LauncherOrigin != "https://launcher.example.com" {
		t.Fatalf("got %q", c.LauncherOrigin)
	}
	for _, bad := range []string{"http://launcher.example.com", "https://x.example.com/path", "nope"} {
		if _, err := load(bad); err == nil {
			t.Fatalf("%s should be refused", bad)
		}
	}
	t.Setenv("LAUNCHER_ORIGIN", "https://env.example.com")
	if c, err = load(""); err != nil || c.LauncherOrigin != "https://env.example.com" {
		t.Fatalf("env: %v %q", err, c.LauncherOrigin)
	}
}
