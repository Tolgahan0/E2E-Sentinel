package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalPlaywrightRunner executes a generated Playwright spec as a plain
// host process instead of inside a disposable Docker container — the
// no-Docker execution path (see docs/RUNNER_ISOLATION.md's "Local
// process execution mode"). This is a materially weaker isolation
// boundary than DockerPlaywrightRunner: the test process runs with
// sentinel-api's own privileges, on the same filesystem and network
// namespace as wherever sentinel-api itself runs — never substituted
// for the Docker runner silently; main.go only constructs this when
// SENTINEL_EXECUTION_MODE says to (explicitly "local", or "auto" when
// Docker isn't reachable).
//
// Requires `playwright` (from a global `npm install -g @playwright/test`
// plus `npx playwright install --with-deps` for the browsers) on the
// host's PATH — mirrors exactly what
// deploy/docker/Dockerfile.runner-playwright bakes into the Docker
// runner image, just installed on the real host instead of an image
// layer.
type LocalPlaywrightRunner struct {
	// WorkspaceDir is where each run gets its own subdirectory
	// containing the generated spec + playwright.config.ts, and where
	// the process is executed (cmd.Dir) — there is no host/container
	// path distinction here, unlike DockerPlaywrightRunner's
	// WorkspaceContainerDir/WorkspaceHostDir split, since nothing is
	// bind-mounted into anything else.
	WorkspaceDir string
	Timeout      time.Duration

	process  processExecutor
	registry *processRegistry
}

// NewLocalPlaywrightRunner constructs a ready-to-use runner backed by
// the real host process executor — registry/process are unexported and
// must be initialized, so this isn't just a plain struct literal like
// the Docker runners (which have no such fields).
func NewLocalPlaywrightRunner(workspaceDir string, timeout time.Duration) *LocalPlaywrightRunner {
	return &LocalPlaywrightRunner{
		WorkspaceDir: workspaceDir, Timeout: timeout,
		process: hostProcessExecutor{}, registry: newProcessRegistry(),
	}
}

func (r *LocalPlaywrightRunner) Name() string { return "playwright-local" }

func (r *LocalPlaywrightRunner) Validate(_ context.Context, input RunInput) error {
	if r.WorkspaceDir == "" {
		return ErrRunnerNotConfigured
	}
	if strings.TrimSpace(input.SpecContent) == "" {
		return fmt.Errorf("runs: empty spec content")
	}
	if err := r.process.lookPath("playwright"); err != nil {
		return fmt.Errorf(`%w: "playwright" (run: npm install -g @playwright/test && npx playwright install --with-deps)`, ErrLocalToolMissing)
	}
	return nil
}

func (r *LocalPlaywrightRunner) workspaceDir(runID string) string {
	return filepath.Join(r.WorkspaceDir, runID)
}

// Execute writes the generated spec + the same fixed Playwright config
// DockerPlaywrightRunner uses into the run's workspace, then runs
// `playwright test` there directly as a host process bounded by
// r.Timeout, tracking its cancel func in r.registry so a concurrent
// Cancel call can reach it.
func (r *LocalPlaywrightRunner) Execute(ctx context.Context, input RunInput) (*RunResult, error) {
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

	runCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	r.registry.store(input.RunID, cancel)
	defer func() {
		cancel()
		r.registry.delete(input.RunID)
	}()

	exitCode, stdout, stderr, err := r.process.run(runCtx, wsDir, "playwright", []string{"test", "--config=playwright.config.ts"}, localProcessEnv())
	if err != nil {
		return nil, err
	}

	return &RunResult{ExitCode: exitCode, Stdout: stdout, Stderr: stderr}, nil
}

// CollectArtifacts reads the same test-results/ layout Playwright
// writes regardless of how it was invoked — shared with
// DockerPlaywrightRunner via collectPlaywrightArtifacts.
func (r *LocalPlaywrightRunner) CollectArtifacts(_ context.Context, runID string) ([]ArtifactFile, error) {
	return collectPlaywrightArtifacts(r.workspaceDir(runID))
}

// Cancel cancels the run's context if it's still tracked in this same
// process — exec.CommandContext kills the process on context
// cancellation. Unlike DockerPlaywrightRunner.Cancel (which reaches the
// container via the Docker daemon by a deterministic name from any
// process), this only works from the sentinel-api process that
// actually started the run — see docs/RUNNER_ISOLATION.md.
func (r *LocalPlaywrightRunner) Cancel(_ context.Context, runID string) error {
	r.registry.cancel(runID)
	return nil
}

func (r *LocalPlaywrightRunner) Cleanup(_ context.Context, runID string) error {
	if err := os.RemoveAll(r.workspaceDir(runID)); err != nil {
		return fmt.Errorf("runs: removing workspace: %w", err)
	}
	return nil
}
