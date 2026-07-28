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

type fakeContainerClient struct {
	created  []string // names passed to CreateContainer
	started  []string
	stopped  []string
	removed  []string
	waitFunc func(id string) (dockerclient.WaitResult, error)
	binds    []string
}

func (f *fakeContainerClient) CreateContainer(_ context.Context, name string, cfg dockerclient.ContainerConfig) (string, error) {
	f.created = append(f.created, name)
	f.binds = cfg.Binds
	return "container-for-" + name, nil
}

func (f *fakeContainerClient) StartContainer(_ context.Context, id string) error {
	f.started = append(f.started, id)
	return nil
}

func (f *fakeContainerClient) WaitContainer(_ context.Context, id string) (dockerclient.WaitResult, error) {
	if f.waitFunc != nil {
		return f.waitFunc(id)
	}
	return dockerclient.WaitResult{StatusCode: 0}, nil
}

func (f *fakeContainerClient) StopContainer(_ context.Context, id string, _ int) error {
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeContainerClient) RemoveContainer(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeContainerClient) ContainerLogs(_ context.Context, id string) (string, string, error) {
	return "ok\n", "", nil
}

func newTestRunner(t *testing.T, docker ContainerClient) (*DockerPlaywrightRunner, string) {
	t.Helper()
	hostDir := t.TempDir()
	return &DockerPlaywrightRunner{
		Docker: docker, Image: "e2e-sentinel-playwright-runner:latest",
		WorkspaceContainerDir: hostDir, WorkspaceHostDir: hostDir,
		MemoryBytes: 1 << 30, NanoCPUs: 1_000_000_000, Timeout: 5 * time.Second,
	}, hostDir
}

func TestDockerPlaywrightRunner_ValidateRequiresHostWorkspaceDir(t *testing.T) {
	r := &DockerPlaywrightRunner{}
	if err := r.Validate(context.Background(), RunInput{SpecContent: "x"}); !errors.Is(err, ErrRunnerNotConfigured) {
		t.Fatalf("err = %v, want ErrRunnerNotConfigured", err)
	}
}

func TestDockerPlaywrightRunner_Execute_FullLifecycle(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, hostDir := newTestRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{
		RunID: "run-1", SpecFilename: "generated-run-1.spec.ts", SpecContent: "test content",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	if len(fake.created) != 1 || fake.created[0] != "sentinel-run-run-1" {
		t.Errorf("created = %v, want [sentinel-run-run-1]", fake.created)
	}
	if len(fake.started) != 1 {
		t.Errorf("expected exactly one StartContainer call, got %v", fake.started)
	}
	if len(fake.removed) != 1 {
		t.Errorf("expected the container to be removed after Execute, got %v", fake.removed)
	}

	// The bind mount source must be the HOST path, not sentinel-api's
	// own container-local workspace path (Docker-outside-of-Docker).
	wantBind := filepath.Join(hostDir, "run-1") + ":/workspace"
	if len(fake.binds) != 1 || fake.binds[0] != wantBind {
		t.Errorf("binds = %v, want [%s]", fake.binds, wantBind)
	}

	specPath := filepath.Join(hostDir, "run-1", "generated-run-1.spec.ts")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("expected spec file to be written at %q: %v", specPath, err)
	}
	if string(data) != "test content" {
		t.Errorf("spec file content = %q", string(data))
	}

	if _, err := os.Stat(filepath.Join(hostDir, "run-1", "playwright.config.ts")); err != nil {
		t.Errorf("expected playwright.config.ts to be written: %v", err)
	}
}

func TestDockerPlaywrightRunner_Execute_NonZeroExitCodeIsNotAGoError(t *testing.T) {
	fake := &fakeContainerClient{waitFunc: func(string) (dockerclient.WaitResult, error) {
		return dockerclient.WaitResult{StatusCode: 1}, nil
	}}
	runner, _ := newTestRunner(t, fake)

	result, err := runner.Execute(context.Background(), RunInput{RunID: "run-2", SpecFilename: "x.spec.ts", SpecContent: "x"})
	if err != nil {
		t.Fatalf("Execute() error: %v (a failing test must be a result, not a Go error)", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestDockerPlaywrightRunner_Cancel_StopsByDeterministicName(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, _ := newTestRunner(t, fake)

	if err := runner.Cancel(context.Background(), "run-3"); err != nil {
		t.Fatalf("Cancel() error: %v", err)
	}
	if len(fake.stopped) != 1 || fake.stopped[0] != "sentinel-run-run-3" {
		t.Errorf("stopped = %v, want [sentinel-run-run-3]", fake.stopped)
	}
}

func TestDockerPlaywrightRunner_CollectArtifacts_NoFailuresYieldsNoArtifacts(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, _ := newTestRunner(t, fake)

	files, err := runner.CollectArtifacts(context.Background(), "run-4")
	if err != nil {
		t.Fatalf("CollectArtifacts() error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no artifacts when test-results/ doesn't exist, got %d", len(files))
	}
}

func TestDockerPlaywrightRunner_CollectArtifacts_FindsScreenshotsVideosTraces(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, hostDir := newTestRunner(t, fake)

	resultsDir := filepath.Join(hostDir, "run-5", "test-results", "my-test")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(resultsDir, "test-failed-1.png"), []byte("png-data"), 0o644)
	os.WriteFile(filepath.Join(resultsDir, "video.webm"), []byte("webm-data"), 0o644)
	os.WriteFile(filepath.Join(resultsDir, "trace.zip"), []byte("zip-data"), 0o644)
	os.WriteFile(filepath.Join(resultsDir, "readme.txt"), []byte("ignored"), 0o644)

	files, err := runner.CollectArtifacts(context.Background(), "run-5")
	if err != nil {
		t.Fatalf("CollectArtifacts() error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d artifacts, want 3 (readme.txt must be ignored): %+v", len(files), files)
	}

	kinds := map[string]bool{}
	for _, f := range files {
		kinds[f.Kind] = true
	}
	for _, want := range []string{"screenshot", "video", "trace"} {
		if !kinds[want] {
			t.Errorf("expected an artifact of kind %q, got %+v", want, files)
		}
	}
}

func TestDockerPlaywrightRunner_Cleanup_RemovesWorkspace(t *testing.T) {
	fake := &fakeContainerClient{}
	runner, hostDir := newTestRunner(t, fake)

	wsDir := filepath.Join(hostDir, "run-6")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := runner.Cleanup(context.Background(), "run-6"); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Errorf("expected workspace directory to be removed, stat err = %v", err)
	}
}
