package dockerclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startFakeDaemon serves handler over a temporary Unix socket and
// returns the socket path, so tests never depend on a real Docker
// daemon being present. It deliberately does NOT use t.TempDir(): that
// nests under a long per-test directory name, and a Unix socket path
// has a ~104 byte limit on macOS/BSD — easily exceeded by Go's default
// temp dir naming. A short path directly under the OS temp root avoids
// that.
func startFakeDaemon(t *testing.T, handler http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dsock")
	if err != nil {
		t.Fatalf("creating short temp dir for socket: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "d.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listening on fake socket: %v", err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { _ = server.Close() })

	return socketPath
}

func TestPing_UnavailableSocketReturnsErrUnavailable(t *testing.T) {
	client := New(filepath.Join(t.TempDir(), "does-not-exist.sock"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := client.Ping(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Ping() = %v, want ErrUnavailable", err)
	}
}

func TestPing_ReachableSocketSucceeds(t *testing.T) {
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	client := New(socketPath)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestListContainers_ParsesComposeLabels(t *testing.T) {
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{
				"Id": "abc123",
				"Names": ["/routa-api-1"],
				"Image": "routa-api:latest",
				"State": "running",
				"Status": "Up 2 minutes (healthy)",
				"Labels": {"com.docker.compose.project": "routa", "com.docker.compose.service": "api"},
				"Ports": [{"PrivatePort": 8080, "PublicPort": 8080, "Type": "tcp"}]
			}
		]`))
	}))

	client := New(socketPath)
	containers, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers() error: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("got %d containers, want 1", len(containers))
	}

	c := containers[0]
	if c.Labels[LabelComposeProject] != "routa" || c.Labels[LabelComposeService] != "api" {
		t.Errorf("compose labels not parsed correctly: %+v", c.Labels)
	}
	if c.State != "running" {
		t.Errorf("State = %q, want running", c.State)
	}
	if len(c.Ports) != 1 || c.Ports[0].PrivatePort != 8080 {
		t.Errorf("Ports = %+v, want one entry with PrivatePort 8080", c.Ports)
	}
}

func TestListContainers_UnavailableSocketReturnsErrUnavailable(t *testing.T) {
	client := New(filepath.Join(t.TempDir(), "does-not-exist.sock"))
	if _, err := client.ListContainers(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ListContainers() = %v, want ErrUnavailable", err)
	}
}
