package projects

import (
	"context"
	"errors"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Routa":        "routa",
		"My Cool App!": "my-cool-app",
		"  spaced  ":   "spaced",
		"":             "project",
		"---":          "project",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateName(t *testing.T) {
	if err := ValidateName(""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ValidateName(\"\") = %v, want ErrInvalidInput", err)
	}
	if err := ValidateName("   "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ValidateName(whitespace) = %v, want ErrInvalidInput", err)
	}
	if err := ValidateName("Routa"); err != nil {
		t.Errorf("ValidateName(\"Routa\") = %v, want nil", err)
	}
}

func TestMemoryStore_CreateGetList(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, err := store.Create(ctx, Project{Name: "Routa", Slug: "routa", RepositoryPath: "/tmp/routa"})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() did not assign an ID")
	}
	if created.DiscoveryStatus != DiscoveryStatusNeverRun {
		t.Errorf("DiscoveryStatus = %q, want %q", created.DiscoveryStatus, DiscoveryStatusNeverRun)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Name != "Routa" {
		t.Errorf("Get().Name = %q, want Routa", got.Name)
	}

	if _, err := store.Get(ctx, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() returned %d projects, want 1", len(list))
	}

	exists, err := store.SlugExists(ctx, "routa")
	if err != nil {
		t.Fatalf("SlugExists() error: %v", err)
	}
	if !exists {
		t.Error("SlugExists(routa) = false, want true")
	}
}

func TestMemoryStore_Create_DefaultsVisualDiffThreshold(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, err := store.Create(ctx, Project{Name: "Routa", Slug: "routa", RepositoryPath: "/tmp/routa"})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if created.VisualDiffThreshold != DefaultVisualDiffThreshold {
		t.Errorf("VisualDiffThreshold = %v, want %v", created.VisualDiffThreshold, DefaultVisualDiffThreshold)
	}
}

func TestMemoryStore_SetVisualDiffThreshold(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, _ := store.Create(ctx, Project{Name: "Routa", Slug: "routa", RepositoryPath: "/tmp/routa"})

	updated, err := store.SetVisualDiffThreshold(ctx, created.ID, 60.0)
	if err != nil {
		t.Fatalf("SetVisualDiffThreshold() error: %v", err)
	}
	if updated.VisualDiffThreshold != 60.0 {
		t.Errorf("VisualDiffThreshold = %v, want 60", updated.VisualDiffThreshold)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.VisualDiffThreshold != 60.0 {
		t.Errorf("Get().VisualDiffThreshold = %v, want 60 (persisted)", got.VisualDiffThreshold)
	}

	if _, err := store.SetVisualDiffThreshold(ctx, "does-not-exist", 60.0); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetVisualDiffThreshold(missing) = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_UpdateNameAndDiscoveryStatus(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, _ := store.Create(ctx, Project{Name: "Old", Slug: "old", RepositoryPath: "/tmp/old"})

	updated, err := store.UpdateName(ctx, created.ID, "New")
	if err != nil {
		t.Fatalf("UpdateName() error: %v", err)
	}
	if updated.Name != "New" {
		t.Errorf("Name = %q, want New", updated.Name)
	}

	if err := store.SetDiscoveryStatus(ctx, created.ID, DiscoveryStatusRunning, nil); err != nil {
		t.Fatalf("SetDiscoveryStatus() error: %v", err)
	}
	got, _ := store.Get(ctx, created.ID)
	if got.DiscoveryStatus != DiscoveryStatusRunning {
		t.Errorf("DiscoveryStatus = %q, want %q", got.DiscoveryStatus, DiscoveryStatusRunning)
	}

	if _, err := store.UpdateName(ctx, "missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateName(missing) = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, _ := store.Create(ctx, Project{Name: "Temp", Slug: "temp", RepositoryPath: "/tmp/temp"})

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after delete = %v, want ErrNotFound", err)
	}

	if err := store.Delete(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(missing) = %v, want ErrNotFound", err)
	}
}
