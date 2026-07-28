package settings

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMemoryStore_GetUnsetKey(t *testing.T) {
	store := NewMemoryStore()
	_, ok, err := store.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if ok {
		t.Error("Get() ok = true for a key that was never set")
	}
}

func TestMemoryStore_SetThenGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if err := store.Set(ctx, "ai.task_routing", json.RawMessage(`{"test_planning":"prov-1"}`)); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	value, ok, err := store.Get(ctx, "ai.task_routing")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if string(value) != `{"test_planning":"prov-1"}` {
		t.Errorf("Get() = %s, want %s", value, `{"test_planning":"prov-1"}`)
	}
}

func TestMemoryStore_SetOverwrites(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k", json.RawMessage(`"first"`))
	_ = store.Set(ctx, "k", json.RawMessage(`"second"`))

	value, _, _ := store.Get(ctx, "k")
	if string(value) != `"second"` {
		t.Errorf("Get() = %s, want %s", value, `"second"`)
	}
}
