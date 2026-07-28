// Package dockerclient is a minimal, read-only Docker Engine API client
// over the Unix socket. It implements exactly two operations (ping,
// list containers) rather than pulling in the full Docker SDK, keeping
// the capability surface — and therefore what a bug here could do —
// deliberately small (spec §7.3: "must not assume Docker socket access
// is safe").
//
// It never assumes the socket is present or reachable: every method
// returns ErrUnavailable when it isn't, so callers can degrade
// gracefully (spec §25 Phase 2 acceptance: "Docker-unavailable state is
// handled gracefully").
package dockerclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// ErrUnavailable is returned by every method when the Docker daemon
// socket is missing or unreachable.
var ErrUnavailable = errors.New("dockerclient: docker daemon unavailable")

// DefaultSocketPath is the standard Docker Engine Unix socket location.
const DefaultSocketPath = "/var/run/docker.sock"

// Client talks to the Docker Engine API over a Unix socket.
type Client struct {
	httpClient *http.Client
}

// New builds a Client bound to socketPath. It does not check
// reachability yet — call Ping for that.
func New(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					d := net.Dialer{}
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	// The host part of the URL is ignored by the custom DialContext
	// above; Docker's API is versioned but tolerates unversioned paths
	// by falling back to the daemon's default API version.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return fmt.Errorf("dockerclient: building request: %w", err)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("dockerclient: %s returned %d: %s", path, res.StatusCode, string(body))
	}

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return fmt.Errorf("dockerclient: decoding response from %s: %w", path, err)
		}
	}
	return nil
}

// Ping checks daemon reachability.
func (c *Client) Ping(ctx context.Context) error {
	return c.get(ctx, "/_ping", nil)
}

// Container is the subset of Docker's container-list response E2E
// Sentinel needs — deliberately narrow, per the package doc.
type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`  // "running", "exited", ...
	Status  string            `json:"Status"` // human-readable, e.g. "Up 2 minutes (healthy)"
	Labels  map[string]string `json:"Labels"`
	Ports   []ContainerPort   `json:"Ports"`
	Command string            `json:"Command"`
}

// ContainerPort is one published or exposed port.
type ContainerPort struct {
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

// Compose label keys Docker Compose sets on every container it manages.
const (
	LabelComposeProject = "com.docker.compose.project"
	LabelComposeService = "com.docker.compose.service"
)

// ListContainers lists all containers (running and stopped) known to
// the daemon. No filtering is done here — callers filter by compose
// labels themselves, since which labels matter is a discovery-layer
// concern, not a Docker-API-client concern.
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	var containers []Container
	if err := c.get(ctx, "/containers/json?all=true", &containers); err != nil {
		return nil, err
	}
	return containers, nil
}
