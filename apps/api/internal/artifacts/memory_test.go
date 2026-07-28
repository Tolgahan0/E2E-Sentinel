package artifacts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStore_SaveAndRead(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	saved, err := store.Save(ctx, "run-1", KindScreenshot, "image/png", []byte("fake-png-bytes"), time.Now().Add(RetentionDefault))
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if saved.Checksum == "" {
		t.Error("expected a non-empty checksum")
	}
	if saved.SizeBytes != int64(len("fake-png-bytes")) {
		t.Errorf("SizeBytes = %d, want %d", saved.SizeBytes, len("fake-png-bytes"))
	}

	data, meta, err := store.Read(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if string(data) != "fake-png-bytes" {
		t.Errorf("data = %q", string(data))
	}
	if meta.Kind != KindScreenshot {
		t.Errorf("Kind = %q, want screenshot", meta.Kind)
	}

	if _, _, err := store.Read(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_ListByRun(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.Save(ctx, "run-1", KindStdout, "text/plain", []byte("out"), time.Now())
	store.Save(ctx, "run-1", KindStderr, "text/plain", []byte("err"), time.Now())
	store.Save(ctx, "run-2", KindStdout, "text/plain", []byte("other"), time.Now())

	list, err := store.ListByRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListByRun() error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d artifacts, want 2", len(list))
	}
}

func TestRetentionFor(t *testing.T) {
	cases := map[string]time.Duration{
		"failed":  RetentionFailed,
		"error":   RetentionFailed,
		"passed":  RetentionPassed,
		"unknown": RetentionDefault,
	}
	for status, want := range cases {
		if got := RetentionFor(status); got != want {
			t.Errorf("RetentionFor(%q) = %v, want %v", status, got, want)
		}
	}
}
