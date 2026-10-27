package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sharedCaddy = `{
	email admin@example.com
}

import conf.d/*.caddy

# The blog
blog.example.com, www.blog.example.com {
	encode zstd
	reverse_proxy 127.0.0.1:8080
}

shop.example.com {
	reverse_proxy 127.0.0.1:9090
}
`

func TestCaddyBlockEditing(t *testing.T) {
	commented, ok := CommentBlock(sharedCaddy, "www.blog.example.com")
	if !ok {
		t.Fatal("the blog block was not found")
	}
	if !strings.Contains(commented, PausedBegin) || !strings.Contains(commented, PausedEnd) {
		t.Fatalf("the paused block lost its markers:\n%s", commented)
	}
	if strings.Contains(commented, "\nblog.example.com,") {
		t.Fatalf("the active blog block is still there:\n%s", commented)
	}
	if !strings.Contains(commented, "\nshop.example.com {\n") || !strings.Contains(commented, "email admin@example.com") {
		t.Fatalf("editing the blog touched another block:\n%s", commented)
	}
	// The attached comment travels with the block and comes back untouched.
	if !strings.Contains(commented, "## The blog") {
		t.Fatalf("the comment above the block was left behind:\n%s", commented)
	}

	paused := pausedBlockSites(commented, "/etc/caddy/Caddyfile", "")
	if len(paused) != 1 || !paused[0].Paused || paused[0].Domains[1] != "www.blog.example.com" || paused[0].Target != "127.0.0.1:8080" {
		t.Fatalf("paused block = %+v", paused)
	}

	restored, ok := RestoreBlock(commented, "blog.example.com")
	if !ok || restored != sharedCaddy {
		t.Fatalf("restore = %v\n%s", ok, restored)
	}

	removed, ok := RemoveBlock(sharedCaddy, "shop.example.com")
	if !ok {
		t.Fatal("the shop block was not removed")
	}
	if strings.Contains(removed, "shop.example.com") || !strings.Contains(removed, "blog.example.com") {
		t.Fatalf("remove took the wrong block:\n%s", removed)
	}

	// Deleting a paused row removes its markers and the whole block.
	removed, ok = RemoveBlock(commented, "blog.example.com")
	if !ok || strings.Contains(removed, "blog.example.com") || strings.Contains(removed, PausedBegin) {
		t.Fatalf("delete of a paused block = %v\n%s", ok, removed)
	}
	if !strings.Contains(removed, "shop.example.com") {
		t.Fatalf("delete of a paused block touched the other site:\n%s", removed)
	}
}

func TestCaddyBlockEditingRefusesUnknownSites(t *testing.T) {
	if _, ok := CommentBlock(sharedCaddy, "nope.example.com"); ok {
		t.Fatal("an unknown domain was commented out")
	}
	if _, ok := RestoreBlock(sharedCaddy, "blog.example.com"); ok {
		t.Fatal("an active site was treated as paused")
	}
	if _, ok := RemoveBlock(sharedCaddy, "nope.example.com"); ok {
		t.Fatal("an unknown domain was deleted")
	}
}

// Paused rows are read from the markers.
func TestPausedCaddyBlockIsRead(t *testing.T) {
	root := t.TempDir()
	sites := filepath.Join(root, "vps-admin-apps")
	if err := os.MkdirAll(sites, 0o755); err != nil {
		t.Fatal(err)
	}
	commented, ok := CommentBlock(sharedCaddy, "blog.example.com")
	if !ok {
		t.Fatal("the blog block was not found")
	}
	caddyfile := filepath.Join(root, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte(commented), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := Read(sites, "")
	if rep.Engine != "caddy" {
		t.Fatalf("engine = %q", rep.Engine)
	}
	blog := find(t, rep, "blog.example.com")
	if !blog.Paused || blog.File != caddyfile || blog.Target != "127.0.0.1:8080" || len(blog.Domains) != 2 {
		t.Fatalf("paused blog = %+v", blog)
	}
	shop := find(t, rep, "shop.example.com")
	if shop.Paused || shop.Target != "127.0.0.1:9090" {
		t.Fatalf("shop = %+v", shop)
	}
}
