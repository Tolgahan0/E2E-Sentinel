package runs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// ErrLocalToolMissing is returned when a local-process runner's
// required host tool isn't on PATH. Local execution mode has no
// container to guarantee this — it needs the same tool installed
// globally on whatever host sentinel-api itself runs on (mirrors the
// Docker runner images' own global npm installs, see
// deploy/docker/Dockerfile.runner-playwright and
// Dockerfile.runner-websocket).
var ErrLocalToolMissing = errors.New("runs: required tool not found on PATH")

// processExecutor runs one command to completion and reports its
// outcome — the local-process analog of ContainerClient (docker_runner.go),
// so LocalPlaywrightRunner/LocalWebSocketRunner can be unit tested
// without depending on `playwright`/`node` actually being installed
// wherever `go test` runs, the same reason the Docker runner tests use
// a fake ContainerClient instead of a real Docker daemon.
type processExecutor interface {
	// lookPath reports an error if name isn't available to run at all
	// — called from Validate.
	lookPath(name string) error
	// run executes name with args in dir using env, bounded by ctx, and
	// returns its outcome. A normal nonzero exit is (code, out, errOut,
	// nil) — never a Go error (spec §2.4: pass/fail comes only from the
	// process's own exit code); a Go error means the process never
	// produced an exit code at all (didn't start, or runCtx's
	// timeout/cancellation killed it).
	run(ctx context.Context, dir, name string, args, env []string) (exitCode int, stdout, stderr string, err error)
}

// hostProcessExecutor is the real, production processExecutor.
type hostProcessExecutor struct{}

func (hostProcessExecutor) lookPath(name string) error {
	_, err := exec.LookPath(name)
	return err
}

func (hostProcessExecutor) run(ctx context.Context, dir, name string, args, env []string) (int, string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	exitCode, err := exitCodeFromRunError(cmd.Run(), ctx)
	return exitCode, stdout.String(), stderr.String(), err
}

var (
	globalNodePathOnce  sync.Once
	globalNodePathValue string
)

// globalNodePath returns `npm root -g`'s output, cached for the process
// lifetime. A global npm install (e.g. `npm install -g @playwright/test`
// or `npm install -g ws`) puts a package's CLI on $PATH but does NOT put
// the package itself on Node's require()/import resolution path for an
// arbitrary working directory — that needs NODE_PATH set explicitly,
// same trick the Docker runner images use (their comments note the
// Debian- and Alpine-based images' global prefixes differ; resolving it
// with the actual command rather than assuming a path is why this does
// the same here). Empty if npm isn't on PATH or the command fails —
// callers just don't set NODE_PATH in that case; the real "tool
// missing" failure already surfaces from Validate's exec.LookPath with
// a clearer, more actionable message.
func globalNodePath() string {
	globalNodePathOnce.Do(func() {
		out, err := exec.Command("npm", "root", "-g").Output()
		if err == nil {
			globalNodePathValue = strings.TrimSpace(string(out))
		}
	})
	return globalNodePathValue
}

// localProcessEnv returns the environment a local-process runner's
// child command should use: the current process's own environment plus
// NODE_PATH, if resolvable.
func localProcessEnv() []string {
	env := os.Environ()
	if p := globalNodePath(); p != "" {
		env = append(env, "NODE_PATH="+p)
	}
	return env
}

// exitCodeFromRunError turns a *exec.Cmd.Run() error into (exitCode,
// nil) for a normal nonzero exit (spec §2.4: pass/fail is decided by
// the process's own exit code, never by anything else), or (-1, err)
// for a Go-level error — the process never producing an exit code at
// all, either because runCtx's timeout/cancellation killed it or it
// never started (tool missing, permissions, etc).
func exitCodeFromRunError(err error, runCtx context.Context) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if runCtx.Err() != nil {
		return 0, fmt.Errorf("runs: run did not exit within its timeout: %w", runCtx.Err())
	}
	return 0, fmt.Errorf("runs: starting local process: %w", err)
}
