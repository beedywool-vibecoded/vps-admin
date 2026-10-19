package scripts

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		``:                      nil,
		`a b  c`:                {"a", "b", "c"},
		`--tag "v1 beta" 'x y'`: {"--tag", "v1 beta", "x y"},
		`a""b`:                  {"ab"},
		`$(rm -rf /)`:           {"$(rm", "-rf", "/)"}, // no expansion: plain words
	}
	for in, want := range cases {
		got, err := SplitArgs(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	if _, err := SplitArgs(`"open`); err == nil {
		t.Error("unclosed quote accepted")
	}
}

func TestLinkListRun(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "update-app.sh")
	os.WriteFile(src, []byte("#!/bin/sh\n# Pulls and restarts the app\necho \"hello $1\"\nexit 3\n"), 0o755)

	name, err := Link(filepath.Join(dir, "scripts"), src, "", "")
	if err != nil || name != "update-app" {
		t.Fatalf("link: %s, %v", name, err)
	}
	if _, err := Link(filepath.Join(dir, "scripts"), src, "../evil", ""); err == nil {
		t.Error("path in name accepted")
	}
	r := &Runner{Dir: filepath.Join(dir, "scripts"), DataDir: dir}
	r.Init()
	list, _ := r.List()
	if len(list) != 1 || list[0].Description != "Pulls and restarts the app" || !list[0].Executable {
		t.Fatalf("list: %+v", list)
	}
	done := make(chan *Run, 1)
	run, err := r.Start("update-app", []string{"world"}, nil, func(r *Run) { done <- r })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start("update-app", nil, nil, nil); err == nil {
		// may have finished already; only a concurrent start must fail
		select {
		case <-run.Done():
		default:
			t.Error("second concurrent run accepted")
		}
	}
	select {
	case got := <-done:
		if strings.TrimSpace(got.Output()) != "hello world" || got.ExitCode == nil || *got.ExitCode != 3 {
			t.Fatalf("run: %q exit %v", got.Output(), got.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("script did not finish")
	}
	if err := Unlink(r.Dir, "update-app"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("unlink removed the target file")
	}
}

func TestComposeFolderDetected(t *testing.T) {
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, "docker-compose.yml"), []byte("services: {}\n"), 0o644)
	os.MkdirAll(filepath.Join(proj, "tools"), 0o755)
	src := filepath.Join(proj, "tools", "update.sh")
	os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755)
	dir := t.TempDir()
	if _, err := Link(dir, src, "", ""); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Dir: dir}
	r.Init()
	list, _ := r.List()
	if len(list) != 1 || list[0].Dir != proj {
		t.Fatalf("compose folder not found: %+v", list)
	}
	other := t.TempDir()
	Link(dir, src, "update", other)
	list, _ = r.List()
	if list[0].Dir != other {
		t.Fatalf("-dir not honored: %+v", list)
	}
	Unlink(dir, "update")
	if len(readMeta(dir)) != 0 {
		t.Fatal("meta kept after unlink")
	}
}
