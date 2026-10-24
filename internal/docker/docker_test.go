package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRestartPolicyReadsAndWrites(t *testing.T) {
	var sent map[string]any
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		reply := func(code int, body string) *http.Response {
			return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/containers/abc/json":
			return reply(http.StatusOK, `{"HostConfig":{"RestartPolicy":{"Name":"unless-stopped"}}}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/abc/update":
			json.NewDecoder(r.Body).Decode(&sent)
			return reply(http.StatusOK, `{}`), nil
		}
		return reply(http.StatusNotFound, `{"message":"no such container"}`), nil
	})}}
	policy, err := c.RestartPolicy(context.Background(), "abc")
	if err != nil || policy != "unless-stopped" {
		t.Fatalf("RestartPolicy = %q, %v", policy, err)
	}
	if err := c.SetRestartPolicy(context.Background(), "abc", "no"); err != nil {
		t.Fatal(err)
	}
	inner, _ := sent["RestartPolicy"].(map[string]any)
	if inner["Name"] != "no" {
		t.Fatalf("sent %v", sent)
	}
	if err := c.SetRestartPolicy(context.Background(), "abc", "sometimes"); err == nil {
		t.Fatal("an unknown policy must be refused")
	}
}

func TestPanelVolumesKeepsAppVolumesAndRunningContainers(t *testing.T) {
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		reply := func(body string) *http.Response {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
		}
		switch {
		case r.URL.Path == "/v1.41/containers/json":
			return reply(`[
				{"Id":"aaaaaaaaaaaa1111","State":"running","Labels":{"vpsadmin.app":"web"}},
				{"Id":"bbbbbbbbbbbb2222","State":"running","Labels":{"com.docker.compose.project":"vpsadmin-api"}},
				{"Id":"cccccccccccc3333","State":"running","Labels":{}}]`), nil
		case r.URL.Path == "/v1.41/containers/aaaaaaaaaaaa/json":
			return reply(`{"Mounts":[{"Type":"volume","Name":"vpsadmin-web_data"},{"Type":"bind","Name":""}]}`), nil
		case r.URL.Path == "/v1.41/containers/bbbbbbbbbbbb/json":
			return reply(`{"Mounts":[{"Type":"volume","Name":"vpsadmin-api_db"}]}`), nil
		case r.URL.Path == "/v1.41/volumes":
			return reply(`{"Volumes":[
				{"Name":"vpsadmin-web_old","Labels":{"com.docker.compose.project":"vpsadmin-web"}},
				{"Name":"postgres-data","Labels":{}}]}`), nil
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause"):
			return reply(`{}`), nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}}
	uses, err := c.PanelVolumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, u := range uses {
		got[u.Name] = u.Containers
	}
	for _, want := range []string{"vpsadmin-web_data", "vpsadmin-api_db", "vpsadmin-web_old"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing volume %q in %+v", want, uses)
		}
	}
	if _, ok := got["postgres-data"]; ok {
		t.Errorf("a volume outside the panel apps must be skipped: %+v", uses)
	}
	if len(got["vpsadmin-web_data"]) != 1 || got["vpsadmin-web_data"][0] != "aaaaaaaaaaaa" {
		t.Errorf("the running container of the volume must be listed: %+v", got)
	}
	if err := c.Pause(context.Background(), "aaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := c.Pause(context.Background(), "../etc"); err == nil {
		t.Fatal("an invalid id must be refused")
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
