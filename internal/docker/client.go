// Package docker is a small Docker Engine API client over the unix socket. The official
// SDK pulls in a large dependency tree for the handful of calls the agent needs.
package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// apiVersion is Docker 20.10, the oldest engine still common on VPS images.
const apiVersion = "/v1.41"

var (
	ErrUnavailable = errors.New("docker is not reachable")
	ErrNotFound    = errors.New("container not found")
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

type Client struct {
	http *http.Client
}

func New(socket string) *Client {
	return &Client{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns: 4,
	}}}
}

type apiError struct {
	Message string `json:"message"`
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body io.Reader) (*http.Response, error) {
	u := "http://docker" + apiVersion + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) {
			return nil, ErrUnavailable
		}
		return nil, err
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		var e apiError
		json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&e)
		if res.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, e.Message)
		}
		return nil, fmt.Errorf("docker: %s (HTTP %d)", e.Message, res.StatusCode)
	}
	return res, nil
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, v any) error {
	res, err := c.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(v)
}

type Version struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
}

func (c *Client) Version(ctx context.Context) (*Version, error) {
	var v Version
	return &v, c.getJSON(ctx, "/version", nil, &v)
}

type PortBinding struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort,omitempty"`
	Type        string `json:"type"`
}

type Container struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	State   string            `json:"state"`  // running | exited | restarting | paused | created | dead
	Status  string            `json:"status"` // "Up 3 days (healthy)"
	Health  string            `json:"health,omitempty"`
	Created time.Time         `json:"created"`
	Ports   []PortBinding     `json:"ports"`
	Project string            `json:"project,omitempty"` // compose project
	WorkDir string            `json:"workDir,omitempty"` // folder of the compose file
	Labels  map[string]string `json:"-"`
	// Exposed is true when a port is published on all interfaces: Docker writes its own
	// iptables rules, so such a port is reachable even if ufw denies it.
	Exposed bool `json:"exposed"`
}

func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	var raw []struct {
		ID      string            `json:"Id"`
		Names   []string          `json:"Names"`
		Image   string            `json:"Image"`
		State   string            `json:"State"`
		Status  string            `json:"Status"`
		Created int64             `json:"Created"`
		Labels  map[string]string `json:"Labels"`
		Ports   []struct {
			IP          string `json:"IP"`
			PrivatePort int    `json:"PrivatePort"`
			PublicPort  int    `json:"PublicPort"`
			Type        string `json:"Type"`
		} `json:"Ports"`
	}
	if err := c.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}}, &raw); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		ct := Container{ID: r.ID[:12], Image: r.Image, State: r.State, Status: r.Status,
			Created: time.Unix(r.Created, 0), Labels: r.Labels, Project: r.Labels["com.docker.compose.project"],
			WorkDir: r.Labels["com.docker.compose.project.working_dir"], Ports: []PortBinding{}}
		if len(r.Names) > 0 {
			ct.Name = strings.TrimPrefix(r.Names[0], "/")
		}
		switch {
		case strings.Contains(r.Status, "(healthy)"):
			ct.Health = "healthy"
		case strings.Contains(r.Status, "(unhealthy)"):
			ct.Health = "unhealthy"
		case strings.Contains(r.Status, "(health: starting)"):
			ct.Health = "starting"
		}
		seen := map[string]bool{}
		for _, p := range r.Ports {
			// Docker lists IPv4 and IPv6 bindings separately; keep one per port.
			k := fmt.Sprintf("%d/%d/%s", p.PrivatePort, p.PublicPort, p.Type)
			if seen[k] {
				continue
			}
			seen[k] = true
			ct.Ports = append(ct.Ports, PortBinding{IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type})
			if p.PublicPort != 0 && (p.IP == "0.0.0.0" || p.IP == "::" || p.IP == "") {
				ct.Exposed = true
			}
		}
		out = append(out, ct)
	}
	slices.SortFunc(out, func(a, b Container) int {
		if a.Project != b.Project {
			return strings.Compare(a.Project, b.Project)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func validID(id string) bool { return idPattern.MatchString(id) }

// Action runs start, stop or restart on a container.
func (c *Client) Action(ctx context.Context, id, action string) error {
	if !validID(id) {
		return ErrNotFound
	}
	if !slices.Contains([]string{"start", "stop", "restart"}, action) {
		return errors.New("action must be start, stop or restart")
	}
	q := url.Values{}
	if action != "start" {
		q.Set("t", "15") // seconds before SIGKILL
	}
	res, err := c.do(ctx, http.MethodPost, "/containers/"+id+"/"+action, q, nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// Logs returns the last lines of a container output, stdout and stderr merged.
func (c *Client) Logs(ctx context.Context, id string, tail int) (string, error) {
	if !validID(id) {
		return "", ErrNotFound
	}
	var info struct {
		Config struct {
			Tty bool `json:"Tty"`
		} `json:"Config"`
	}
	if err := c.getJSON(ctx, "/containers/"+id+"/json", nil, &info); err != nil {
		return "", err
	}
	tail = min(max(tail, 1), 5000)
	res, err := c.do(ctx, http.MethodGet, "/containers/"+id+"/logs",
		url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {strconv.Itoa(tail)}}, nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body := io.LimitReader(res.Body, 8<<20)
	if info.Config.Tty {
		b, err := io.ReadAll(body)
		return string(b), err
	}
	return demux(body)
}

// demux strips the 8 byte frame headers Docker adds when stdout and stderr share a stream.
func demux(r io.Reader) (string, error) {
	br := bufio.NewReader(r)
	var b strings.Builder
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, hdr); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return b.String(), nil
			}
			return b.String(), err
		}
		n := binary.BigEndian.Uint32(hdr[4:])
		if _, err := io.CopyN(&b, br, int64(n)); err != nil {
			return b.String(), nil
		}
	}
}

type Stats struct {
	ID       string  `json:"id"`
	CPU      float64 `json:"cpu"` // percent of one core times cores, like `docker stats`
	MemUsed  uint64  `json:"memUsed"`
	MemLimit uint64  `json:"memLimit"`
}

// Stats samples every running container in parallel. Each call takes about a second
// because the engine needs two readings to compute CPU usage.
func (c *Client) Stats(ctx context.Context, ids []string) []Stats {
	out := make([]Stats, len(ids))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = c.stats(ctx, id)
		}()
	}
	wg.Wait()
	return out
}

func (c *Client) stats(ctx context.Context, id string) Stats {
	s := Stats{ID: id}
	var raw struct {
		CPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
			Online int    `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		Mem struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if !validID(id) || c.getJSON(ctx, "/containers/"+id+"/stats", url.Values{"stream": {"false"}}, &raw) != nil {
		return s
	}
	cpuDelta := float64(raw.CPU.Usage.Total) - float64(raw.PreCPU.Usage.Total)
	sysDelta := float64(raw.CPU.System) - float64(raw.PreCPU.System)
	if cpuDelta > 0 && sysDelta > 0 {
		s.CPU = float64(int64(cpuDelta/sysDelta*float64(max(raw.CPU.Online, 1))*1000+0.5)) / 10
	}
	// Same as the docker CLI: page cache does not count as used memory.
	cache := raw.Mem.Stats["inactive_file"]
	if raw.Mem.Usage > cache {
		s.MemUsed = raw.Mem.Usage - cache
	}
	s.MemLimit = raw.Mem.Limit
	return s
}
