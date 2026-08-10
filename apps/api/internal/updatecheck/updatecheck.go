// Package updatecheck periodically compares the running E2E Sentinel
// version against the latest tag published on GitHub Releases, purely
// to surface "an update is available" to a human via GET /version (the
// panel's Dashboard and scripts/onboard.sh both read it) — it never
// downloads, applies, or auto-restarts anything itself.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// DefaultInterval is how often RunLoop re-checks GitHub Releases.
const DefaultInterval = 6 * time.Hour

// DefaultRepo is the GitHub repository releases are checked against.
const DefaultRepo = "Tolgahan0/E2E-Sentinel"

// Info is the result of the most recent check.
type Info struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	UpdateAvailable bool      `json:"update_available"`
	ReleaseURL      string    `json:"release_url"`
	CheckedAt       time.Time `json:"checked_at"`
	// CheckError is set when the most recent check itself failed (e.g.
	// no network route to GitHub) — never presented as "no update
	// available", so an air-gapped deployment can tell the difference.
	CheckError string `json:"check_error,omitempty"`
}

// Store holds the latest check result. Safe for concurrent use: one
// goroutine (RunLoop) writes, any number of HTTP handlers read.
type Store struct {
	mu   sync.RWMutex
	info Info
}

// NewStore returns a Store pre-populated with currentVersion and no
// check performed yet — used as-is (UpdateAvailable always false)
// until either update checking is disabled or the first check lands.
func NewStore(currentVersion string) *Store {
	return &Store{info: Info{CurrentVersion: currentVersion}}
}

// Get returns the most recent check result.
func (s *Store) Get() Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

// Set overwrites the stored result. RunLoop is the only production
// caller; exported so tests can inject a result without running a real
// check against GitHub.
func (s *Store) Set(info Info) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info = info
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckOnce fetches the latest published release from GitHub and
// compares it against currentVersion. A failed check is reported via
// Info.CheckError, never as a Go error — a caller running an
// always-on-loop (RunLoop) has nothing to do with an error return
// except log it, so the result value carries it instead.
func CheckOnce(ctx context.Context, client *http.Client, repo, currentVersion string) Info {
	info := Info{CurrentVersion: currentVersion, CheckedAt: time.Now()}
	if client == nil {
		client = &http.Client{}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo), nil)
	if err != nil {
		info.CheckError = "building the GitHub releases request failed"
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		info.CheckError = "GitHub releases API unreachable"
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No release published yet (e.g. a fresh fork, or main-only
		// development) — not an error, just nothing to compare against.
		return info
	}
	if resp.StatusCode != http.StatusOK {
		info.CheckError = fmt.Sprintf("GitHub releases API returned %d", resp.StatusCode)
		return info
	}

	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		info.CheckError = "parsing the GitHub releases response failed"
		return info
	}

	info.LatestVersion = release.TagName
	info.ReleaseURL = release.HTMLURL
	if info.ReleaseURL == "" && release.TagName != "" {
		info.ReleaseURL = fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, release.TagName)
	}
	info.UpdateAvailable = IsNewer(release.TagName, currentVersion)
	return info
}

// RunLoop checks once immediately, then on interval, until ctx is
// cancelled. A failed check is logged and never stops the loop — the
// same never-stop-on-a-failed-tick shape as artifacts.RunRetentionLoop.
func RunLoop(ctx context.Context, store *Store, client *http.Client, repo, currentVersion string, interval time.Duration, logger zerolog.Logger) {
	check := func() {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		info := CheckOnce(checkCtx, client, repo, currentVersion)
		store.Set(info)
		switch {
		case info.CheckError != "":
			logger.Warn().Str("error", info.CheckError).Msg("update check failed")
		case info.UpdateAvailable:
			logger.Info().Str("current_version", info.CurrentVersion).Str("latest_version", info.LatestVersion).
				Msg("a newer E2E Sentinel version is available")
		}
	}

	check()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// IsNewer reports whether latest is a newer version than current. Both
// are expected in "v1.2.3" form (a leading "v" is stripped, a
// pre-release/build suffix like "-rc1" is dropped). Either side failing
// to parse as three numeric components (e.g. current == "dev", "latest",
// or "main" during local development or an unpinned install) makes the
// comparison impossible to trust, so it returns false rather than risk
// a false "update available".
func IsNewer(latest, current string) bool {
	lv, ok1 := parseSemver(latest)
	cv, ok2 := parseSemver(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if lv[i] != cv[i] {
			return lv[i] > cv[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return out, false
	}
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
