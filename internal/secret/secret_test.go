package secret

import "testing"

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := b.Seal("hunter2")
	if s == "hunter2" || s == b.Seal("hunter2") {
		t.Fatal("not encrypted, or nonce reused")
	}
	b2, _ := Open(dir) // same key file
	if got, err := b2.Open(s); err != nil || got != "hunter2" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := b.Open(s[:len(s)-4] + "AAAA"); err == nil {
		t.Fatal("tampered value accepted")
	}
	if got, _ := b.Open(""); got != "" {
		t.Fatal("empty should stay empty")
	}
}
