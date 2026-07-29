package providers

import (
	"context"
	"testing"
)

func TestMemoryStore_CreateValidatesType(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Create(context.Background(), Provider{Type: "not-a-real-type", Name: "x"})
	if err != ErrInvalidType {
		t.Fatalf("Create() error = %v, want ErrInvalidType", err)
	}
}

func TestMemoryStore_CreateRequiresName(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Create(context.Background(), Provider{Type: TypeOllama})
	if err != ErrNameRequired {
		t.Fatalf("Create() error = %v, want ErrNameRequired", err)
	}
}

func TestMemoryStore_CreateDefaultsTimeoutAndHealth(t *testing.T) {
	store := NewMemoryStore()
	p, err := store.Create(context.Background(), Provider{Type: TypeOllama, Name: "Local Ollama", IsLocal: true})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if p.TimeoutSeconds != DefaultTimeoutSeconds {
		t.Errorf("TimeoutSeconds = %d, want %d", p.TimeoutSeconds, DefaultTimeoutSeconds)
	}
	if p.HealthStatus != HealthUnknown {
		t.Errorf("HealthStatus = %q, want %q", p.HealthStatus, HealthUnknown)
	}
	if p.ID == "" {
		t.Error("Create() returned empty ID")
	}
}

func TestMemoryStore_ListAndGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	p, _ := store.Create(ctx, Provider{Type: TypeOpenAI, Name: "OpenAI"})

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() returned %d providers, want 1", len(list))
	}

	got, err := store.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Name != "OpenAI" {
		t.Errorf("Get().Name = %q, want %q", got.Name, "OpenAI")
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Get(context.Background(), "does-not-exist"); err != ErrNotFound {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	p, _ := store.Create(ctx, Provider{Type: TypeOllama, Name: "Temp"})

	if err := store.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := store.Get(ctx, p.ID); err != ErrNotFound {
		t.Errorf("Get() after delete = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "missing"); err != ErrNotFound {
		t.Errorf("Delete(missing) = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_UpdatePartialFields(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	p, _ := store.Create(ctx, Provider{Type: TypeOpenAI, Name: "OpenAI", Model: "gpt-4"})

	newModel := "gpt-4-turbo"
	updated, err := store.Update(ctx, p.ID, Patch{Model: &newModel})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.Model != "gpt-4-turbo" {
		t.Errorf("Model = %q, want %q", updated.Model, "gpt-4-turbo")
	}
	if updated.Name != "OpenAI" {
		t.Errorf("Name should be unchanged, got %q", updated.Name)
	}
}

func TestMemoryStore_UpdateSecretReference(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	p, _ := store.Create(ctx, Provider{Type: TypeOpenAI, Name: "OpenAI"})

	ref := "secret-ref-1"
	updated, err := store.Update(ctx, p.ID, Patch{SecretReferenceID: &ref})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.SecretReferenceID != "secret-ref-1" {
		t.Errorf("SecretReferenceID = %q, want %q", updated.SecretReferenceID, "secret-ref-1")
	}

	cleared, err := store.Update(ctx, p.ID, Patch{ClearSecretReference: true})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if cleared.SecretReferenceID != "" {
		t.Errorf("SecretReferenceID = %q, want empty after clear", cleared.SecretReferenceID)
	}
}

func TestMemoryStore_UpdateRejectsEmptyName(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	p, _ := store.Create(ctx, Provider{Type: TypeOpenAI, Name: "OpenAI"})

	empty := ""
	if _, err := store.Update(ctx, p.ID, Patch{Name: &empty}); err != ErrNameRequired {
		t.Fatalf("Update() error = %v, want ErrNameRequired", err)
	}
}

func TestMemoryStore_UpdateNotFound(t *testing.T) {
	store := NewMemoryStore()
	name := "x"
	if _, err := store.Update(context.Background(), "does-not-exist", Patch{Name: &name}); err != ErrNotFound {
		t.Fatalf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_UpdateHealth(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	p, _ := store.Create(ctx, Provider{Type: TypeOllama, Name: "Local"})

	updated, err := store.UpdateHealth(ctx, p.ID, HealthOK, p.CreatedAt)
	if err != nil {
		t.Fatalf("UpdateHealth() error: %v", err)
	}
	if updated.HealthStatus != HealthOK {
		t.Errorf("HealthStatus = %q, want %q", updated.HealthStatus, HealthOK)
	}
}

func TestValidType(t *testing.T) {
	for _, tt := range []string{TypeOllama, TypeOpenAI, TypeAnthropic, TypeGemini, TypeAzureOpenAI, TypeOpenAICompatible} {
		if !ValidType(tt) {
			t.Errorf("ValidType(%q) = false, want true", tt)
		}
	}
	if ValidType("bogus") {
		t.Error("ValidType(bogus) = true, want false")
	}
}

func TestValidTask(t *testing.T) {
	for _, tt := range AllTasks {
		if !ValidTask(tt) {
			t.Errorf("ValidTask(%q) = false, want true", tt)
		}
	}
	if ValidTask("bogus") {
		t.Error("ValidTask(bogus) = true, want false")
	}
}
