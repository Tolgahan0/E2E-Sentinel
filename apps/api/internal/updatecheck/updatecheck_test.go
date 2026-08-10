package updatecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.3.0", "v1.2.3", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.3.0", false},
		{"v2.0.0", "v1.9.9", true},
		{"v1.2.3", "dev", false},    // unparseable current — never claim an update
		{"latest", "v1.0.0", false}, // unparseable latest (e.g. no releases yet)
		{"v1.2.3-rc1", "v1.2.2", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestCheckOnce_UpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v9.9.9",
			"html_url": "https://example.invalid/releases/tag/v9.9.9",
		})
	}))
	defer srv.Close()

	info := checkAgainst(t, srv.URL, "v1.0.0")
	if !info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
	if info.LatestVersion != "v9.9.9" {
		t.Errorf("LatestVersion = %q, want v9.9.9", info.LatestVersion)
	}
	if info.CheckError != "" {
		t.Errorf("CheckError = %q, want empty", info.CheckError)
	}
}

func TestCheckOnce_NoReleaseYet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	info := checkAgainst(t, srv.URL, "v1.0.0")
	if info.UpdateAvailable {
		t.Errorf("UpdateAvailable = true, want false (no release published)")
	}
	if info.CheckError != "" {
		t.Errorf("CheckError = %q, want empty (404 is not a check failure)", info.CheckError)
	}
}

func TestCheckOnce_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	info := checkAgainst(t, srv.URL, "v1.0.0")
	if info.UpdateAvailable {
		t.Errorf("UpdateAvailable = true, want false")
	}
	if info.CheckError == "" {
		t.Errorf("CheckError = empty, want a message describing the failed check")
	}
}

// checkAgainst calls CheckOnce against a test server's own base URL
// rather than the real GitHub API — repo here is deliberately just the
// server's own address, since CheckOnce builds the request URL as
// https://api.github.com/repos/<repo>/... which a real network call
// would need; instead this drives the underlying HTTP client at the
// test server directly via a custom Transport.
func checkAgainst(t *testing.T, serverURL, currentVersion string) Info {
	t.Helper()
	client := &http.Client{
		Transport: rewriteTransport{targetBase: serverURL},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return CheckOnce(ctx, client, "owner/repo", currentVersion)
}

// rewriteTransport redirects every request to targetBase, ignoring the
// scheme/host CheckOnce built in (api.github.com) — the only way to
// point CheckOnce at an httptest.Server without changing its
// hard-coded GitHub URL.
type rewriteTransport struct {
	targetBase string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := http.NewRequestWithContext(req.Context(), req.Method, rt.targetBase+req.URL.Path, req.Body)
	if err != nil {
		return nil, err
	}
	target.Header = req.Header
	return http.DefaultTransport.RoundTrip(target)
}
