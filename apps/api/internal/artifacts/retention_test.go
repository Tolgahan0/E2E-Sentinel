package artifacts

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestMemoryStore_DeleteExpired(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	now := time.Now()

	expired, _ := store.Save(ctx, "run-1", KindStdout, "text/plain", []byte("old"), now.Add(-time.Hour))
	fresh, _ := store.Save(ctx, "run-1", KindStdout, "text/plain", []byte("new"), now.Add(time.Hour))

	deleted, err := store.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired() error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	if _, _, err := store.Read(ctx, expired.ID); err != ErrNotFound {
		t.Errorf("Read(expired) error = %v, want ErrNotFound", err)
	}
	if _, _, err := store.Read(ctx, fresh.ID); err != nil {
		t.Errorf("Read(fresh) error = %v, want nil", err)
	}

	list, err := store.ListByRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListByRun() error: %v", err)
	}
	if len(list) != 1 || list[0].ID != fresh.ID {
		t.Errorf("ListByRun() after DeleteExpired = %+v, want only the fresh artifact", list)
	}
}

func TestMemoryStore_DeleteExpired_NoneExpired(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.Save(ctx, "run-1", KindStdout, "text/plain", []byte("x"), time.Now().Add(time.Hour))

	deleted, err := store.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("DeleteExpired() error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

func TestRunRetentionLoop_StopsOnContextCancellation(t *testing.T) {
	store := NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	logger := zerolog.New(io.Discard)

	done := make(chan struct{})
	go func() {
		RunRetentionLoop(ctx, store, time.Millisecond, logger)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunRetentionLoop did not stop after context cancellation")
	}
}

func TestRunRetentionLoop_DeletesExpiredArtifactsOnTick(t *testing.T) {
	store := NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zerolog.New(io.Discard)

	expired, _ := store.Save(context.Background(), "run-1", KindStdout, "text/plain", []byte("old"), time.Now().Add(-time.Hour))

	go RunRetentionLoop(ctx, store, 10*time.Millisecond, logger)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := store.Read(context.Background(), expired.ID); err == ErrNotFound {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expired artifact was not removed by the retention loop within the deadline")
}
