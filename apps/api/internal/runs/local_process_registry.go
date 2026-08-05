package runs

import "sync"

// processRegistry tracks the cancel function for each in-flight local
// process run, keyed by run ID. A Docker-based runner needs no
// equivalent — Cancel there stops a container by its deterministic name
// via the Docker daemon, which holds that state independently of
// sentinel-api's own process. A local process has no such external
// daemon, so Cancel here only works within the same sentinel-api
// process that started the run (see docs/RUNNER_ISOLATION.md).
type processRegistry struct {
	mu      sync.Mutex
	cancels map[string]func()
}

func newProcessRegistry() *processRegistry {
	return &processRegistry{cancels: make(map[string]func())}
}

func (r *processRegistry) store(runID string, cancel func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[runID] = cancel
}

func (r *processRegistry) delete(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, runID)
}

// cancel calls and removes the stored cancel function, if one exists
// for runID. Returns false if the run isn't tracked (already finished,
// never started here, or started by a different process) — the caller
// treats that as "nothing to do" rather than an error, matching the
// Docker runner's own tolerant Cancel-of-an-already-finished-run
// behavior.
func (r *processRegistry) cancel(runID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.cancels[runID]
	if !ok {
		return false
	}
	delete(r.cancels, runID)
	cancel()
	return true
}
