package dockerclient

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func frame(streamType byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	return append(header, []byte(payload)...)
}

func TestCreateStartWaitRemoveContainer_FullLifecycle(t *testing.T) {
	var created, started, waited, removed bool

	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			created = true
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["Image"] != "e2e-sentinel-playwright-runner:latest" {
				t.Errorf("create body Image = %v, want e2e-sentinel-playwright-runner:latest", body["Image"])
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"Id": "container-123"})
		case r.Method == http.MethodPost && r.URL.Path == "/containers/container-123/start":
			started = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/container-123/wait":
			waited = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"StatusCode": 0})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/containers/container-123"):
			removed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))

	client := New(socketPath)
	ctx := context.Background()

	id, err := client.CreateContainer(ctx, "test-run-1", ContainerConfig{
		Image: "e2e-sentinel-playwright-runner:latest",
		Binds: []string{"/workspace:/workspace:ro"},
	})
	if err != nil {
		t.Fatalf("CreateContainer() error: %v", err)
	}
	if id != "container-123" {
		t.Fatalf("id = %q, want container-123", id)
	}
	if !created {
		t.Error("create endpoint was not called")
	}

	if err := client.StartContainer(ctx, id); err != nil {
		t.Fatalf("StartContainer() error: %v", err)
	}
	if !started {
		t.Error("start endpoint was not called")
	}

	result, err := client.WaitContainer(ctx, id)
	if err != nil {
		t.Fatalf("WaitContainer() error: %v", err)
	}
	if result.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0", result.StatusCode)
	}
	if !waited {
		t.Error("wait endpoint was not called")
	}

	if err := client.RemoveContainer(ctx, id); err != nil {
		t.Fatalf("RemoveContainer() error: %v", err)
	}
	if !removed {
		t.Error("remove endpoint was not called")
	}
}

func TestWaitContainer_NonZeroExitCode(t *testing.T) {
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"StatusCode": 1})
	}))
	client := New(socketPath)

	result, err := client.WaitContainer(context.Background(), "any-id")
	if err != nil {
		t.Fatalf("WaitContainer() error: %v", err)
	}
	if result.StatusCode != 1 {
		t.Errorf("StatusCode = %d, want 1 (a failing test's exit code must be reported)", result.StatusCode)
	}
}

func TestStopContainer_UsesTimeout(t *testing.T) {
	var gotQuery string
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))
	client := New(socketPath)

	if err := client.StopContainer(context.Background(), "id1", 5); err != nil {
		t.Fatalf("StopContainer() error: %v", err)
	}
	if gotQuery != "t=5" {
		t.Errorf("query = %q, want t=5", gotQuery)
	}
}

func TestContainerLogs_DemultiplexesStdoutAndStderr(t *testing.T) {
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(frame(1, "stdout line\n"))
		w.Write(frame(2, "stderr line\n"))
		w.Write(frame(1, "stdout line 2\n"))
	}))
	client := New(socketPath)

	stdout, stderr, err := client.ContainerLogs(context.Background(), "any-id")
	if err != nil {
		t.Fatalf("ContainerLogs() error: %v", err)
	}
	if stdout != "stdout line\nstdout line 2\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if stderr != "stderr line\n" {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestContainerLogs_EmptyStreamIsNotAnError(t *testing.T) {
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := New(socketPath)

	stdout, stderr, err := client.ContainerLogs(context.Background(), "any-id")
	if err != nil {
		t.Fatalf("ContainerLogs() error: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("expected empty output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestStopContainer_NotFoundIsNotAnError(t *testing.T) {
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such container", http.StatusNotFound)
	}))
	client := New(socketPath)

	if err := client.StopContainer(context.Background(), "already-gone", 5); err != nil {
		t.Fatalf("StopContainer() error: %v, want nil (cancellation must be idempotent)", err)
	}
}

func TestRemoveContainer_NotFoundIsNotAnError(t *testing.T) {
	socketPath := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such container", http.StatusNotFound)
	}))
	client := New(socketPath)

	if err := client.RemoveContainer(context.Background(), "already-gone"); err != nil {
		t.Fatalf("RemoveContainer() error: %v, want nil (cleanup must be idempotent)", err)
	}
}
