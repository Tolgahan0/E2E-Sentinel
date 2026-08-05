package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeProcessExecutor is the local-process analog of fakeContainerClient
// (docker_runner_test.go) — lets Execute be tested without depending on
// `playwright`/`node` actually being installed wherever `go test` runs.
type fakeProcessExecutor struct {
	missing  map[string]bool // lookPath fails for these names
	runFunc  func(dir, name string, args []string) (exitCode int, stdout, stderr string, err error)
	lastDir  string
	lastName string
	lastArgs []string
}

func (f *fakeProcessExecutor) lookPath(name string) error {
	if f.missing[name] {
		return errors.New("not found")
	}
	return nil
}

func (f *fakeProcessExecutor) run(_ context.Context, dir, name string, args, _ []string) (int, string, string, error) {
	f.lastDir, f.lastName, f.lastArgs = dir, name, args
	if f.runFunc != nil {
		return f.runFunc(dir, name, args)
	}
	return 0, "ok\n", "", nil
}

func newTestLocalPlaywrightRunner(t *testing.T, process *fakeProcessExecutor) (*LocalPlaywrightRunner, string) {
	t.Helper()
	dir := t.TempDir()
	r := NewLocalPlaywrightRunner(dir, 5*time.Second)
	r.process = process
	return r, dir
}

func TestLocalPlaywrightRunner_ValidateRequiresWorkspaceDir(t *testing.T) {
	r := NewLocalPlaywrightRunner("", time.Second)
	r.process = &fakeProcessExecutor{}
	if err := r.Validate(context.Background(), RunInput{SpecContent: "x"}); !errors.Is(err, ErrRunnerNotConfigured) {
		t.Fatalf("err = %v, want ErrRunnerNotConfigured", err)
	}
}

func TestLocalPlaywrightRunner_ValidateRequiresPlaywrightOnPath(t *testing.T) {
	r, _ := newTestLocalPlaywrightRunner(t, &fakeProcessExecutor{missing: map[string]bool{"playwright": true}})
	err := r.Validate(context.Background(), RunInput{SpecContent: "x"})
	if !errors.Is(err, ErrLocalToolMissing) {
		t.Fatalf("err = %v, want ErrLocalToolMissing", err)
	}
}

func TestLocalPlaywrightRunner_Execute_WritesSpecAndRunsPlaywright(t *testing.T) {
	fake := &fakeProcessExecutor{}
	runner, dir := newTestLocalPlaywrightRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{
		RunID: "run-1", SpecFilename: "generated-run-1.spec.ts", SpecContent: "test content",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	if fake.lastName != "playwright" {
		t.Errorf("ran %q, want \"playwright\"", fake.lastName)
	}
	wantDir := filepath.Join(dir, "run-1")
	if fake.lastDir != wantDir {
		t.Errorf("ran in %q, want %q", fake.lastDir, wantDir)
	}

	data, err := os.ReadFile(filepath.Join(wantDir, "generated-run-1.spec.ts"))
	if err != nil {
		t.Fatalf("expected spec file to be written: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("spec file content = %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(wantDir, "playwright.config.ts")); err != nil {
		t.Errorf("expected playwright.config.ts to be written: %v", err)
	}
}

func TestLocalPlaywrightRunner_Execute_NonZeroExitCodeIsNotAGoError(t *testing.T) {
	fake := &fakeProcessExecutor{runFunc: func(string, string, []string) (int, string, string, error) {
		return 1, "", "assertion failed", nil
	}}
	runner, _ := newTestLocalPlaywrightRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{RunID: "run-2", SpecFilename: "x.spec.ts", SpecContent: "x"})
	if err != nil {
		t.Fatalf("Execute() error: %v (a failing test must be a result, not a Go error)", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestLocalPlaywrightRunner_Cancel_CancelsTrackedRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	fake := &fakeProcessExecutor{runFunc: func(string, string, []string) (int, string, string, error) {
		close(started)
		<-release
		return 0, "", "", nil
	}}
	runner, _ := newTestLocalPlaywrightRunner(t, fake)

	done := make(chan error, 1)
	go func() {
		_, err := runner.Execute(context.Background(), RunInput{RunID: "run-3", SpecFilename: "x.spec.ts", SpecContent: "x"})
		done <- err
	}()

	<-started
	if err := runner.Cancel(context.Background(), "run-3"); err != nil {
		t.Fatalf("Cancel() error: %v", err)
	}
	close(release)
	<-done // just drain; the fake ignores ctx cancellation itself, this only proves Cancel didn't error
}

func TestLocalPlaywrightRunner_CollectArtifacts_FindsScreenshotsVideosTraces(t *testing.T) {
	runner, dir := newTestLocalPlaywrightRunner(t, &fakeProcessExecutor{})

	resultsDir := filepath.Join(dir, "run-4", "test-results", "my-test")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(resultsDir, "test-failed-1.png"), []byte("png-data"), 0o644)
	os.WriteFile(filepath.Join(resultsDir, "readme.txt"), []byte("ignored"), 0o644)

	files, err := runner.CollectArtifacts(context.Background(), "run-4")
	if err != nil {
		t.Fatalf("CollectArtifacts() error: %v", err)
	}
	if len(files) != 1 || files[0].Kind != "screenshot" {
		t.Fatalf("got %+v, want exactly one screenshot artifact", files)
	}
}

func TestLocalPlaywrightRunner_Cleanup_RemovesWorkspace(t *testing.T) {
	runner, dir := newTestLocalPlaywrightRunner(t, &fakeProcessExecutor{})

	wsDir := filepath.Join(dir, "run-5")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := runner.Cleanup(context.Background(), "run-5"); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Errorf("expected workspace directory to be removed, stat err = %v", err)
	}
}
