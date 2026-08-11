package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"e2e-sentinel/apps/api/internal/dockerclient"
)

// ErrRunnerNotConfigured is returned when the runner's host workspace
// directory hasn't been set — see config.RunnerWorkspaceHostDir's doc
// comment for why this is required for Docker-outside-of-Docker.
var ErrRunnerNotConfigured = errors.New("runs: runner not configured (SENTINEL_RUNNER_HOST_WORKSPACE_DIR is unset)")

// ContainerClient is the subset of *dockerclient.Client the runner uses,
// so it can be unit tested with a fake Docker daemon.
type ContainerClient interface {
	CreateContainer(ctx context.Context, name string, cfg dockerclient.ContainerConfig) (string, error)
	StartContainer(ctx context.Context, id string) error
	WaitContainer(ctx context.Context, id string) (dockerclient.WaitResult, error)
	StopContainer(ctx context.Context, id string, timeoutSeconds int) error
	RemoveContainer(ctx context.Context, id string) error
	ContainerLogs(ctx context.Context, id string) (stdout, stderr string, err error)
}

// DockerPlaywrightRunner executes one generated Playwright spec per run,
// each in its own disposable, resource-limited container (spec §11.1).
type DockerPlaywrightRunner struct {
	Docker                ContainerClient
	Image                 string
	WorkspaceContainerDir string // sentinel-api's own view (for reading/writing files)
	WorkspaceHostDir      string // the SAME directory's path on the Docker host (for bind mounts)
	MemoryBytes           int64
	NanoCPUs              int64
	Timeout               time.Duration
}

func (r *DockerPlaywrightRunner) Name() string { return "playwright-docker" }

func (r *DockerPlaywrightRunner) Validate(_ context.Context, input RunInput) error {
	if r.WorkspaceHostDir == "" {
		return ErrRunnerNotConfigured
	}
	if strings.TrimSpace(input.SpecContent) == "" {
		return fmt.Errorf("runs: empty spec content")
	}
	return nil
}

func containerName(runID string) string {
	var b strings.Builder
	for _, c := range runID {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		}
	}
	return "sentinel-run-" + b.String()
}

func (r *DockerPlaywrightRunner) workspaceDir(runID string) string {
	return filepath.Join(r.WorkspaceContainerDir, runID)
}

// screenshot is { mode: 'on', fullPage: true } rather than the simpler
// 'only-on-failure' — visual regression testing (internal/visualdiff)
// needs a screenshot from every run, pass or fail, to diff against a
// stored baseline. fullPage matters here specifically: a viewport-only
// shot would make an unrelated below-the-fold change invisible to the
// diff.
const playwrightConfigTemplate = `import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: '.',
  timeout: 60_000,
  reporter: [['list']],
  use: {
    screenshot: { mode: 'on', fullPage: true },
    video: 'retain-on-failure',
    trace: 'retain-on-failure',
  },
});
`

// Execute writes the generated spec + a fixed Playwright config into the
// run's workspace, then creates, starts, and waits for a disposable
// runner container to execute it. The container is removed before
// Execute returns; the workspace directory is left in place for
// CollectArtifacts (removed later by Cleanup).
func (r *DockerPlaywrightRunner) Execute(ctx context.Context, input RunInput) (*RunResult, error) {
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
	if err := os.WriteFile(filepath.Join(wsDir, "playwright.config.ts"), []byte(playwrightConfigTemplate), 0o644); err != nil {
		return nil, fmt.Errorf("runs: writing playwright config: %w", err)
	}

	hostBind := filepath.Join(r.WorkspaceHostDir, input.RunID) + ":/workspace"
	name := containerName(input.RunID)

	containerID, err := r.Docker.CreateContainer(ctx, name, dockerclient.ContainerConfig{
		Image:       r.Image,
		Cmd:         []string{"playwright", "test", "--config=/workspace/playwright.config.ts"},
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
		// Either our own timeout fired or the run was cancelled (Cancel
		// stops the container directly by name, independent of this
		// context) — either way, make sure it's actually stopped, then
		// fetch the resulting exit code with a fresh, short-lived wait.
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

// CollectArtifacts walks the run's workspace for Playwright's own output
// (screenshots/videos/traces, written under test-results/ when a test
// fails — spec §10.1's "Screenshot on failure", "Video on failure").
// Call after Execute, before Cleanup.
func (r *DockerPlaywrightRunner) CollectArtifacts(_ context.Context, runID string) ([]ArtifactFile, error) {
	return collectPlaywrightArtifacts(r.workspaceDir(runID))
}

// Cancel stops the run's container by its deterministic name. This
// works across processes/requests — the HTTP handler serving
// POST /runs/{id}/cancel doesn't need any in-memory state to find the
// container Execute (running in a different goroutine, possibly
// started by a different request) created.
func (r *DockerPlaywrightRunner) Cancel(ctx context.Context, runID string) error {
	return r.Docker.StopContainer(ctx, containerName(runID), 5)
}

// Cleanup removes the run's workspace directory. Call only after
// CollectArtifacts has read whatever it needs.
func (r *DockerPlaywrightRunner) Cleanup(_ context.Context, runID string) error {
	if err := os.RemoveAll(r.workspaceDir(runID)); err != nil {
		return fmt.Errorf("runs: removing workspace: %w", err)
	}
	return nil
}
