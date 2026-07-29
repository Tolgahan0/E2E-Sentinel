package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"e2e-sentinel/apps/api/internal/dockerclient"
)

// DockerWebSocketRunner executes one generated WebSocket smoke-test
// script (internal/testgen's "websocket" framework output) per run, each
// in its own disposable, resource-limited container — the same
// isolation model as DockerPlaywrightRunner (spec §11.1), just pointed
// at a much smaller image (deploy/docker/Dockerfile.runner-websocket:
// plain Node.js + the "ws" package, no browser stack) since a WebSocket
// smoke test needs neither Chromium/Firefox/WebKit nor Playwright
// itself.
type DockerWebSocketRunner struct {
	Docker                ContainerClient
	Image                 string
	WorkspaceContainerDir string
	WorkspaceHostDir      string
	MemoryBytes           int64
	NanoCPUs              int64
	Timeout               time.Duration
}

func (r *DockerWebSocketRunner) Name() string { return "websocket-docker" }

func (r *DockerWebSocketRunner) Validate(_ context.Context, input RunInput) error {
	if r.WorkspaceHostDir == "" {
		return ErrRunnerNotConfigured
	}
	if strings.TrimSpace(input.SpecContent) == "" {
		return fmt.Errorf("runs: empty spec content")
	}
	return nil
}

func (r *DockerWebSocketRunner) workspaceDir(runID string) string {
	return filepath.Join(r.WorkspaceContainerDir, runID)
}

// Execute writes the generated script into the run's workspace, then
// creates, starts, and waits for a disposable runner container to
// execute it with plain `node`. Mirrors DockerPlaywrightRunner.Execute
// almost exactly — the two intentionally aren't merged into one shared
// helper yet, since a third adapter's Execute may need to diverge
// further (e.g. no bind-mounted workspace at all) and premature sharing
// would make that harder, not easier.
func (r *DockerWebSocketRunner) Execute(ctx context.Context, input RunInput) (*RunResult, error) {
	if err := r.Validate(ctx, input); err != nil {
		return nil, err
	}

	wsDir := r.workspaceDir(input.RunID)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return nil, fmt.Errorf("runs: creating workspace: %w", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, input.SpecFilename), []byte(input.SpecContent), 0o644); err != nil {
		return nil, fmt.Errorf("runs: writing spec file: %w", err)
	}

	hostBind := filepath.Join(r.WorkspaceHostDir, input.RunID) + ":/workspace"
	name := containerName(input.RunID)

	containerID, err := r.Docker.CreateContainer(ctx, name, dockerclient.ContainerConfig{
		Image:       r.Image,
		Cmd:         []string{"node", input.SpecFilename},
		WorkingDir:  "/workspace",
		Binds:       []string{hostBind},
		MemoryBytes: r.MemoryBytes,
		NanoCPUs:    r.NanoCPUs,
		NetworkMode: "bridge",
	})
	if err != nil {
		return nil, fmt.Errorf("runs: creating runner container: %w", err)
	}

	if err := r.Docker.StartContainer(ctx, containerID); err != nil {
		_ = r.Docker.RemoveContainer(context.Background(), containerID)
		return nil, fmt.Errorf("runs: starting runner container: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	result, waitErr := r.Docker.WaitContainer(waitCtx, containerID)
	cancel()
	if waitErr != nil {
		_ = r.Docker.StopContainer(context.Background(), containerID, 5)
		finalCtx, finalCancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, waitErr = r.Docker.WaitContainer(finalCtx, containerID)
		finalCancel()
		if waitErr != nil {
			_ = r.Docker.RemoveContainer(context.Background(), containerID)
			return nil, fmt.Errorf("runs: run did not exit within its timeout: %w", waitErr)
		}
	}

	stdout, stderr, _ := r.Docker.ContainerLogs(context.Background(), containerID)
	_ = r.Docker.RemoveContainer(context.Background(), containerID)

	return &RunResult{ExitCode: result.StatusCode, Stdout: stdout, Stderr: stderr}, nil
}

// CollectArtifacts is a no-op: a WebSocket smoke test produces no
// screenshots/videos/traces — its stdout/stderr (saved generically by
// executeRunAsync for every runner type) is the only evidence.
func (r *DockerWebSocketRunner) CollectArtifacts(_ context.Context, _ string) ([]ArtifactFile, error) {
	return nil, nil
}

func (r *DockerWebSocketRunner) Cancel(ctx context.Context, runID string) error {
	return r.Docker.StopContainer(ctx, containerName(runID), 5)
}

func (r *DockerWebSocketRunner) Cleanup(_ context.Context, runID string) error {
	if err := os.RemoveAll(r.workspaceDir(runID)); err != nil {
		return fmt.Errorf("runs: removing workspace: %w", err)
	}
	return nil
}
