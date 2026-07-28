package environments

import (
	"context"
	"errors"
	"testing"
)

func TestRestrictForClassification_ProductionForcesFlagsOff(t *testing.T) {
	env := Environment{
		Classification:          ClassificationProduction,
		AllowMutations:          true,
		AllowLoadTests:          true,
		AllowActiveSecurityScan: true,
	}
	restricted := RestrictForClassification(env)
	if !restricted.IsProduction {
		t.Error("IsProduction = false, want true for production classification")
	}
	if restricted.AllowMutations || restricted.AllowLoadTests || restricted.AllowActiveSecurityScan {
		t.Errorf("production environment must force all mutation-class flags off, got %+v", restricted)
	}
}

func TestRestrictForClassification_UnknownForcesFlagsOff(t *testing.T) {
	env := Environment{Classification: ClassificationUnknown, AllowMutations: true}
	restricted := RestrictForClassification(env)
	if restricted.AllowMutations {
		t.Error("unknown classification must be handled restrictively (AllowMutations forced off)")
	}
	if restricted.IsProduction {
		t.Error("unknown classification is not production")
	}
}

func TestRestrictForClassification_LocalPreservesFlags(t *testing.T) {
	env := Environment{Classification: ClassificationLocal, AllowMutations: true}
	restricted := RestrictForClassification(env)
	if !restricted.AllowMutations {
		t.Error("local classification should not force AllowMutations off")
	}
}

func TestDefaultForProject(t *testing.T) {
	env := DefaultForProject("proj-1")
	if env.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want proj-1", env.ProjectID)
	}
	if env.Classification != ClassificationLocal {
		t.Errorf("Classification = %q, want local", env.Classification)
	}
	if env.IsProduction {
		t.Error("default environment must not be production")
	}
}

func TestMemoryStore_CreateRejectsInvalidClassification(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Create(context.Background(), Environment{ProjectID: "p1", Name: "x", Classification: "not-real"})
	if !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("err = %v, want ErrInvalidClassification", err)
	}
}

func TestMemoryStore_UpdateClassificationAppliesRestrictions(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, err := store.Create(ctx, Environment{ProjectID: "p1", Name: "default", Classification: ClassificationLocal})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	updated, err := store.UpdateClassification(ctx, created.ID, ClassificationProduction)
	if err != nil {
		t.Fatalf("UpdateClassification() error: %v", err)
	}
	if !updated.IsProduction {
		t.Error("IsProduction = false after classifying as production")
	}
	if updated.AllowMutations {
		t.Error("AllowMutations should be forced off after classifying as production")
	}

	if _, err := store.UpdateClassification(ctx, "missing", ClassificationLocal); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
