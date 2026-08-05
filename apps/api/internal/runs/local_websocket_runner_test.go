package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestLocalWebSocketRunner(t *testing.T, process *fakeProcessExecutor) (*LocalWebSocketRunner, string) {
	t.Helper()
	dir := t.TempDir()
	r := NewLocalWebSocketRunner(dir, 5*time.Second)
	r.process = process
	return r, dir
}

func TestLocalWebSocketRunner_ValidateRequiresWorkspaceDir(t *testing.T) {
	r := NewLocalWebSocketRunner("", time.Second)
	r.process = &fakeProcessExecutor{}
	if err := r.Validate(context.Background(), RunInput{SpecContent: "x"}); !errors.Is(err, ErrRunnerNotConfigured) {
		t.Fatalf("err = %v, want ErrRunnerNotConfigured", err)
	}
}

func TestLocalWebSocketRunner_ValidateRequiresNodeOnPath(t *testing.T) {
	r, _ := newTestLocalWebSocketRunner(t, &fakeProcessExecutor{missing: map[string]bool{"node": true}})
	err := r.Validate(context.Background(), RunInput{SpecContent: "x"})
	if !errors.Is(err, ErrLocalToolMissing) {
		t.Fatalf("err = %v, want ErrLocalToolMissing", err)
	}
}

func TestLocalWebSocketRunner_Execute_WritesScriptAndRunsNode(t *testing.T) {
	fake := &fakeProcessExecutor{}
	runner, dir := newTestLocalWebSocketRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{
		RunID: "run-1", SpecFilename: "generated-run-1.test.js", SpecContent: "console.log('ok')",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	if fake.lastName != "node" {
		t.Errorf("ran %q, want \"node\"", fake.lastName)
	}
	if len(fake.lastArgs) != 1 || fake.lastArgs[0] != "generated-run-1.test.js" {
		t.Errorf("args = %v, want [generated-run-1.test.js]", fake.lastArgs)
	}

	wantDir := filepath.Join(dir, "run-1")
	data, err := os.ReadFile(filepath.Join(wantDir, "generated-run-1.test.js"))
	if err != nil {
		t.Fatalf("expected script file to be written: %v", err)
	}
	if string(data) != "console.log('ok')" {
		t.Errorf("script content = %q", string(data))
	}
}

func TestLocalWebSocketRunner_Execute_NonZeroExitCodeIsNotAGoError(t *testing.T) {
	fake := &fakeProcessExecutor{runFunc: func(string, string, []string) (int, string, string, error) {
		return 1, "", "connection refused", nil
	}}
	runner, _ := newTestLocalWebSocketRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{RunID: "run-2", SpecFilename: "x.test.js", SpecContent: "x"})
	if err != nil {
		t.Fatalf("Execute() error: %v (a failing test must be a result, not a Go error)", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestLocalWebSocketRunner_CollectArtifacts_IsAlwaysEmpty(t *testing.T) {
	runner, _ := newTestLocalWebSocketRunner(t, &fakeProcessExecutor{})
	files, err := runner.CollectArtifacts(context.Background(), "run-3")
	if err != nil {
		t.Fatalf("CollectArtifacts() error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d artifacts, want 0", len(files))
	}
}

func TestLocalWebSocketRunner_Cleanup_RemovesWorkspace(t *testing.T) {
	runner, dir := newTestLocalWebSocketRunner(t, &fakeProcessExecutor{})

	wsDir := filepath.Join(dir, "run-4")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := runner.Cleanup(context.Background(), "run-4"); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Errorf("expected workspace directory to be removed, stat err = %v", err)
	}
}
