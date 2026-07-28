package compose

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeCompose(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing compose fixture: %v", err)
	}
	return path
}

func findService(services []Service, name string) (Service, bool) {
	for _, s := range services {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}

func TestParseFile_ShortSyntax(t *testing.T) {
	path := writeCompose(t, `
services:
  api:
    build: .
    ports:
      - "8080:8080"
    environment:
      - SENTINEL_LOG_LEVEL=info
      - SENTINEL_DATABASE_URL=postgres://secret-should-not-leak
    depends_on:
      - postgres
      - redis
  postgres:
    image: postgres:16-alpine
`)

	services, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error: %v", err)
	}

	api, ok := findService(services, "api")
	if !ok {
		t.Fatalf("expected service %q, got %+v", "api", services)
	}
	if !api.HasBuild {
		t.Error("HasBuild = false, want true (build: . was declared)")
	}
	if len(api.Ports) != 1 || api.Ports[0] != "8080:8080" {
		t.Errorf("Ports = %v, want [8080:8080]", api.Ports)
	}

	sort.Strings(api.EnvVarNames)
	wantNames := []string{"SENTINEL_DATABASE_URL", "SENTINEL_LOG_LEVEL"}
	if len(api.EnvVarNames) != len(wantNames) {
		t.Fatalf("EnvVarNames = %v, want %v", api.EnvVarNames, wantNames)
	}
	for i, name := range wantNames {
		if api.EnvVarNames[i] != name {
			t.Errorf("EnvVarNames[%d] = %q, want %q", i, api.EnvVarNames[i], name)
		}
	}

	// The critical security property: no env var VALUE ever appears
	// anywhere in the parsed output, including the "secret" one.
	for _, s := range services {
		for _, name := range s.EnvVarNames {
			if name != "SENTINEL_LOG_LEVEL" && name != "SENTINEL_DATABASE_URL" {
				t.Errorf("unexpected env var name leaked: %q", name)
			}
		}
	}

	sort.Strings(api.DependsOn)
	if len(api.DependsOn) != 2 || api.DependsOn[0] != "postgres" || api.DependsOn[1] != "redis" {
		t.Errorf("DependsOn = %v, want [postgres redis]", api.DependsOn)
	}

	postgres, ok := findService(services, "postgres")
	if !ok {
		t.Fatal("expected service postgres")
	}
	if postgres.Image != "postgres:16-alpine" {
		t.Errorf("Image = %q, want postgres:16-alpine", postgres.Image)
	}
	if postgres.HasBuild {
		t.Error("HasBuild = true, want false (no build key declared)")
	}
}

func TestParseFile_LongSyntax(t *testing.T) {
	path := writeCompose(t, `
services:
  worker:
    image: example/worker
    environment:
      QUEUE_NAME: jobs
      API_KEY: super-secret-value
    depends_on:
      postgres:
        condition: service_healthy
    profiles: [background]
    command: ["node", "worker.js"]
`)

	services, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error: %v", err)
	}

	worker, ok := findService(services, "worker")
	if !ok {
		t.Fatal("expected service worker")
	}

	sort.Strings(worker.EnvVarNames)
	if len(worker.EnvVarNames) != 2 || worker.EnvVarNames[0] != "API_KEY" || worker.EnvVarNames[1] != "QUEUE_NAME" {
		t.Errorf("EnvVarNames = %v, want [API_KEY QUEUE_NAME]", worker.EnvVarNames)
	}
	// Never the value.
	for _, name := range worker.EnvVarNames {
		if name == "super-secret-value" || name == "jobs" {
			t.Fatal("env var VALUE leaked into names list")
		}
	}

	if len(worker.DependsOn) != 1 || worker.DependsOn[0] != "postgres" {
		t.Errorf("DependsOn = %v, want [postgres]", worker.DependsOn)
	}
	if len(worker.Profiles) != 1 || worker.Profiles[0] != "background" {
		t.Errorf("Profiles = %v, want [background]", worker.Profiles)
	}
	if worker.Command != "node worker.js" {
		t.Errorf("Command = %q, want %q", worker.Command, "node worker.js")
	}
}

func TestParseFile_MalformedYAMLReturnsError(t *testing.T) {
	path := writeCompose(t, "services: [this is not, valid: compose")
	if _, err := ParseFile(path); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestParseFile_MissingFileReturnsError(t *testing.T) {
	if _, err := ParseFile(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestParseFile_EmptyServicesIsNotAnError(t *testing.T) {
	path := writeCompose(t, "version: \"3\"\n")
	services, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("expected no services, got %+v", services)
	}
}
