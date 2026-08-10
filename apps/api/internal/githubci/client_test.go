package githubci

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestCommit_ReturnsSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/commits/main" {
			t.Errorf("path = %q, want /repos/acme/widget/commits/main", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token shh" {
			t.Errorf("Authorization header = %q, want %q", got, "token shh")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": "deadbeef"})
	}))
	defer srv.Close()

	client := NewClient(nil)
	client.BaseURL = srv.URL

	sha, err := client.LatestCommit(context.Background(), "acme/widget", "main", "shh")
	if err != nil {
		t.Fatalf("LatestCommit() error = %v", err)
	}
	if sha != "deadbeef" {
		t.Errorf("sha = %q, want deadbeef", sha)
	}
}

func TestLatestCommit_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(nil)
	client.BaseURL = srv.URL

	if _, err := client.LatestCommit(context.Background(), "acme/widget", "main", "shh"); err == nil {
		t.Fatal("LatestCommit() error = nil, want an error for a 404 response")
	}
}

func TestLatestCommit_NeverLeaksTokenInError(t *testing.T) {
	client := NewClient(nil)
	client.BaseURL = "http://127.0.0.1:0" // unreachable, forces a transport-level error

	_, err := client.LatestCommit(context.Background(), "acme/widget", "main", "super-secret-token")
	if err == nil {
		t.Fatal("LatestCommit() error = nil, want an error for an unreachable host")
	}
	if got := err.Error(); containsToken(got, "super-secret-token") {
		t.Errorf("error message leaked the token: %q", got)
	}
}

func containsToken(s, token string) bool {
	for i := 0; i+len(token) <= len(s); i++ {
		if s[i:i+len(token)] == token {
			return true
		}
	}
	return false
}

func TestSetCommitStatus_PostsExpectedPayload(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/repos/acme/widget/statuses/deadbeef" {
			t.Errorf("path = %q, want /repos/acme/widget/statuses/deadbeef", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewClient(nil)
	client.BaseURL = srv.URL

	err := client.SetCommitStatus(context.Background(), "acme/widget", "deadbeef", "shh", CommitStatus{
		State: StatusSuccess, Description: "2/2 passed",
	})
	if err != nil {
		t.Fatalf("SetCommitStatus() error = %v", err)
	}
	if gotBody["state"] != StatusSuccess || gotBody["description"] != "2/2 passed" || gotBody["context"] != statusContext {
		t.Errorf("posted body = %+v, want state/description/context set", gotBody)
	}
}

func TestSetCommitStatus_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	client := NewClient(nil)
	client.BaseURL = srv.URL

	if err := client.SetCommitStatus(context.Background(), "acme/widget", "deadbeef", "shh", CommitStatus{State: StatusFailure}); err == nil {
		t.Fatal("SetCommitStatus() error = nil, want an error for a 422 response")
	}
}
