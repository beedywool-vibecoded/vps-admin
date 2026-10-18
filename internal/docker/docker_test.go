package docker

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func frame(stream byte, s string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
	return append(h, s...)
}

func TestDemux(t *testing.T) {
	var b bytes.Buffer
	b.Write(frame(1, "out line\n"))
	b.Write(frame(2, "err line\n"))
	got, err := demux(&b)
	if err != nil || got != "out line\nerr line\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"7b95d9a38af3", "my-app_1", "stabilite-api-1"} {
		if !validID(id) {
			t.Errorf("%q rejected", id)
		}
	}
	for _, id := range []string{"", "../etc", "a/b", "-x", "a b"} {
		if validID(id) {
			t.Errorf("%q accepted", id)
		}
	}
}
