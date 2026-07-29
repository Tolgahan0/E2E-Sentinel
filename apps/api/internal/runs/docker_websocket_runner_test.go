package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"e2e-sentinel/apps/api/internal/dockerclient"
)

func newTestWebSocketRunner(t *testing.T, docker ContainerClient) (*DockerWebSocketRunner, string) {
	t.Helper()
	hostDir := t.TempDir()
	return &DockerWebSocketRunner{
		Docker: docker, Image: "e2e-sentinel-websocket-runner:latest",
		WorkspaceContainerDir: hostDir, WorkspaceHostDir: hostDir,
		MemoryBytes: 1 << 28, NanoCPUs: 500_000_000, Timeout: 5 * time.Second,
	}, hostDir
}

func TestDockerWebSocketRunner_Name(t *testing.T) {
	r := &DockerWebSocketRunner{}
	if r.Name() != "websocket-docker" {
		t.Errorf("Name() = %q, want websocket-docker", r.Name())
	}
}

func TestDockerWebSocketRunner_ValidateRequiresHostWorkspaceDir(t *testing.T) {
	r := &DockerWebSocketRunner{}
	if err := r.Validate(context.Background(), RunInput{SpecContent: "x"}); !errors.Is(err, ErrRunnerNotConfigured) {
		t.Fatalf("err = %v, want ErrRunnerNotConfigured", err)
	}
}

func TestDockerWebSocketRunner_Execute_FullLifecycle(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, hostDir := newTestWebSocketRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{
		RunID: "run-1", SpecFilename: "generated-run-1.test.js", SpecContent: "console.log('ok')",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if len(fake.created) != 1 || len(fake.started) != 1 || len(fake.removed) != 1 {
		t.Errorf("container lifecycle calls = created:%d started:%d removed:%d, want 1 each", len(fake.created), len(fake.started), len(fake.removed))
	}

	written, err := os.ReadFile(filepath.Join(hostDir, "run-1", "generated-run-1.test.js"))
	if err != nil {
		t.Fatalf("reading written spec file: %v", err)
	}
	if string(written) != "console.log('ok')" {
		t.Errorf("written spec content = %q", written)
	}
}

func TestDockerWebSocketRunner_Execute_UsesNodeCommand(t *testing.T) {
	var gotCmd []string
	fake := &fakeContainerClient{}
	runner, _ := newTestWebSocketRunner(t, fake)

	// Wrap CreateContainer via a thin adapter to capture Cmd, since
	// fakeContainerClient doesn't record it directly.
	captured := &cmdCapturingClient{ContainerClient: fake}
	runner.Docker = captured

	if _, err := runner.Execute(context.Background(), RunInput{RunID: "run-2", SpecFilename: "generated-run-2.test.js", SpecContent: "x"}); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	gotCmd = captured.lastCmd
	if len(gotCmd) != 2 || gotCmd[0] != "node" || gotCmd[1] != "generated-run-2.test.js" {
		t.Errorf("Cmd = %v, want [node generated-run-2.test.js]", gotCmd)
	}
}

type cmdCapturingClient struct {
	ContainerClient
	lastCmd []string
}

func (c *cmdCapturingClient) CreateContainer(ctx context.Context, name string, cfg dockerclient.ContainerConfig) (string, error) {
	c.lastCmd = cfg.Cmd
	return c.ContainerClient.CreateContainer(ctx, name, cfg)
}

func TestDockerWebSocketRunner_Execute_NonZeroExitIsFailureNotError(t *testing.T) {
	fake := &fakeContainerClient{waitFunc: func(string) (dockerclient.WaitResult, error) {
		return dockerclient.WaitResult{StatusCode: 1}, nil
	}}
	runner, _ := newTestWebSocketRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{RunID: "run-3", SpecFilename: "generated-run-3.test.js", SpecContent: "x"})
	if err != nil {
		t.Fatalf("Execute() error: %v, want nil (a failing connection is a result, not an execution error)", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestDockerWebSocketRunner_CollectArtifactsIsAlwaysEmpty(t *testing.T) {
	r := &DockerWebSocketRunner{}
	artifacts, err := r.CollectArtifacts(context.Background(), "any-run-id")
	if err != nil {
		t.Fatalf("CollectArtifacts() error: %v", err)
	}
	if artifacts != nil {
		t.Errorf("artifacts = %v, want nil (a WebSocket smoke test produces no files)", artifacts)
	}
}

func TestDockerWebSocketRunner_CancelStopsByDeterministicName(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, _ := newTestWebSocketRunner(t, fake)

	if err := runner.Cancel(context.Background(), "run-4"); err != nil {
		t.Fatalf("Cancel() error: %v", err)
	}
	if len(fake.stopped) != 1 || fake.stopped[0] != "sentinel-run-run-4" {
		t.Errorf("stopped = %v, want [sentinel-run-run-4]", fake.stopped)
	}
}

func TestDockerWebSocketRunner_CleanupRemovesWorkspace(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, hostDir := newTestWebSocketRunner(t, fake)

	runDir := filepath.Join(hostDir, "run-5")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := runner.Cleanup(context.Background(), "run-5"); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Error("workspace directory still exists after Cleanup()")
	}
}
