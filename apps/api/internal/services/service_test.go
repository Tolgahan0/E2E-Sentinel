package services

import (
	"context"
	"testing"

	"e2e-sentinel/apps/api/internal/compose"
)

func TestClassifyKind_KnownImages(t *testing.T) {
	cases := map[string]string{
		"postgres:16-alpine":    KindDatabase,
		"redis:7-alpine":        KindCache,
		"confluentinc/cp-kafka": KindQueue,
		"nginx:latest":          KindProxy,
	}
	for image, want := range cases {
		kind, confidence := ClassifyKind(compose.Service{Image: image})
		if kind != want {
			t.Errorf("ClassifyKind(image=%q) kind = %q, want %q", image, kind, want)
		}
		if confidence != ConfidenceHigh {
			t.Errorf("ClassifyKind(image=%q) confidence = %q, want high (known image name)", image, confidence)
		}
	}
}

func TestClassifyKind_HeuristicFallbacksAreMediumConfidence(t *testing.T) {
	withPort, confidence := ClassifyKind(compose.Service{Image: "routa/api", Ports: []string{"8080:8080"}})
	if withPort != KindAPI || confidence != ConfidenceMedium {
		t.Errorf("service with ports = (%q, %q), want (api, medium)", withPort, confidence)
	}

	worker, confidence := ClassifyKind(compose.Service{HasBuild: true})
	if worker != KindWorker || confidence != ConfidenceMedium {
		t.Errorf("service with build, no ports = (%q, %q), want (worker, medium)", worker, confidence)
	}

	unknown, confidence := ClassifyKind(compose.Service{})
	if unknown != KindUnknown || confidence != ConfidenceMedium {
		t.Errorf("bare service = (%q, %q), want (unknown, medium)", unknown, confidence)
	}
}

func TestFromCompose_NeverLeaksEnvVarValues(t *testing.T) {
	svc := FromCompose("proj-1", "docker-compose.yml", compose.Service{
		Name:        "api",
		EnvVarNames: []string{"DATABASE_URL", "API_KEY"},
	})

	names, ok := svc.Metadata["env_var_names"].([]string)
	if !ok {
		t.Fatalf("Metadata[env_var_names] has wrong type: %T", svc.Metadata["env_var_names"])
	}
	if len(names) != 2 {
		t.Fatalf("got %d env var names, want 2", len(names))
	}
	if svc.ProjectID != "proj-1" || svc.SourcePath != "docker-compose.yml" {
		t.Errorf("Service = %+v, want ProjectID=proj-1, SourcePath=docker-compose.yml", svc)
	}
}

func TestApplyRuntimeStatus_UpdatesPortsAndStatus(t *testing.T) {
	svc := FromCompose("proj-1", "docker-compose.yml", compose.Service{Name: "api", Ports: []string{"8080:8080"}})
	svc = ApplyRuntimeStatus(svc, "routa-api-1", "running", "Up 2 minutes (healthy)", []string{"0.0.0.0:8080->8080/tcp"})

	if svc.ContainerName != "routa-api-1" {
		t.Errorf("ContainerName = %q, want routa-api-1", svc.ContainerName)
	}
	if svc.Metadata["status"] != "running" {
		t.Errorf("Metadata[status] = %v, want running", svc.Metadata["status"])
	}
	if svc.LastSeenAt.IsZero() {
		t.Error("LastSeenAt was not set")
	}
	if len(svc.Ports) != 1 || svc.Ports[0] != "0.0.0.0:8080->8080/tcp" {
		t.Errorf("Ports = %v, want live port mapping", svc.Ports)
	}
}

func TestMemoryStore_UpsertIsIdempotentByProjectAndName(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	first, err := store.Upsert(ctx, Service{ProjectID: "p1", Name: "api", Kind: KindAPI, ConfidenceLevel: ConfidenceHigh})
	if err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}

	second, err := store.Upsert(ctx, Service{ProjectID: "p1", Name: "api", Kind: KindWeb, ConfidenceLevel: ConfidenceHigh})
	if err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("re-upserting the same (project, name) should keep the same ID; got %q then %q", first.ID, second.ID)
	}
	if second.Kind != KindWeb {
		t.Errorf("Kind = %q, want the updated value web", second.Kind)
	}

	list, err := store.ListByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("ListByProject() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d services, want 1 (upsert must not duplicate)", len(list))
	}
}
