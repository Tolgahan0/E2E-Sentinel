package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalWebSocketRunner is LocalPlaywrightRunner's counterpart for the
// "websocket" framework — executes a generated WebSocket smoke-test
// script with plain `node` on the host instead of inside
// DockerWebSocketRunner's disposable container. Requires only Node.js
// plus a global `npm install -g ws` on the host (no browser stack at
// all, same as why Dockerfile.runner-websocket is a much smaller image
// than Dockerfile.runner-playwright).
type LocalWebSocketRunner struct {
	WorkspaceDir string
	Timeout      time.Duration

	process  processExecutor
	registry *processRegistry
}

func NewLocalWebSocketRunner(workspaceDir string, timeout time.Duration) *LocalWebSocketRunner {
	return &LocalWebSocketRunner{
		WorkspaceDir: workspaceDir, Timeout: timeout,
		process: hostProcessExecutor{}, registry: newProcessRegistry(),
	}
}

func (r *LocalWebSocketRunner) Name() string { return "websocket-local" }

func (r *LocalWebSocketRunner) Validate(_ context.Context, input RunInput) error {
	if r.WorkspaceDir == "" {
		return ErrRunnerNotConfigured
	}
	if strings.TrimSpace(input.SpecContent) == "" {
		return fmt.Errorf("runs: empty spec content")
	}
	if err := r.process.lookPath("node"); err != nil {
		return fmt.Errorf(`%w: "node" (install Node.js, then: npm install -g ws)`, ErrLocalToolMissing)
	}
	return nil
}

func (r *LocalWebSocketRunner) workspaceDir(runID string) string {
	return filepath.Join(r.WorkspaceDir, runID)
}

func (r *LocalWebSocketRunner) Execute(ctx context.Context, input RunInput) (*RunResult, error) {
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

	runCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	r.registry.store(input.RunID, cancel)
	defer func() {
		cancel()
		r.registry.delete(input.RunID)
	}()

	exitCode, stdout, stderr, err := r.process.run(runCtx, wsDir, "node", []string{input.SpecFilename}, localProcessEnv())
	if err != nil {
		return nil, err
	}

	return &RunResult{ExitCode: exitCode, Stdout: stdout, Stderr: stderr}, nil
}

// CollectArtifacts is a no-op, same reasoning as
// DockerWebSocketRunner's: a WebSocket smoke test produces no
// screenshots/videos/traces.
func (r *LocalWebSocketRunner) CollectArtifacts(_ context.Context, _ string) ([]ArtifactFile, error) {
	return nil, nil
}

func (r *LocalWebSocketRunner) Cancel(_ context.Context, runID string) error {
	r.registry.cancel(runID)
	return nil
}

func (r *LocalWebSocketRunner) Cleanup(_ context.Context, runID string) error {
	if err := os.RemoveAll(r.workspaceDir(runID)); err != nil {
		return fmt.Errorf("runs: removing workspace: %w", err)
	}
	return nil
}
