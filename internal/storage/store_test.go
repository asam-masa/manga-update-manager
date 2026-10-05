package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

func TestStoreMigratesCreatesAndReadsWork(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "test.sqlite")

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	now := time.Date(2026, 10, 5, 0, 30, 0, 123, time.UTC)
	work, err := manga.NewWork(manga.NewWorkInput{
		URL:           "https://example.com/manga/1",
		Title:         "作品名",
		SiteName:      "サイト名",
		ThumbnailPath: "covers/1.webp",
		Notes:         "メモ",
	}, now)
	if err != nil {
		t.Fatalf("NewWork() error = %v", err)
	}
	work.UpdatedAt = work.CreatedAt.Add(100 * time.Millisecond)

	created, err := store.CreateWork(ctx, work)
	if err != nil {
		t.Fatalf("CreateWork() error = %v", err)
	}
	if created.ID <= 0 {
		t.Fatalf("created.ID = %d", created.ID)
	}

	got, err := store.WorkByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("WorkByID() error = %v", err)
	}
	if got != created {
		t.Fatalf("WorkByID() = %#v, want %#v", got, created)
	}

	var createdAt string
	var updatedAt string
	if err := store.db.QueryRowContext(ctx,
		"SELECT created_at, updated_at FROM works WHERE id = ?", created.ID,
	).Scan(&createdAt, &updatedAt); err != nil {
		t.Fatalf("read stored timestamps: %v", err)
	}
	if createdAt != "2026-10-05T00:30:00.000000123Z" {
		t.Fatalf("created_at = %q", createdAt)
	}
	if updatedAt != "2026-10-05T00:30:00.100000123Z" {
		t.Fatalf("updated_at = %q", updatedAt)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open() after migration error = %v", err)
	}
	t.Cleanup(func() { reopened.Close() })

	got, err = reopened.WorkByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("WorkByID() after reopen error = %v", err)
	}
	if got != created {
		t.Fatalf("WorkByID() after reopen = %#v, want %#v", got, created)
	}

	var migrationCount int
	if err := reopened.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count = %d, want 1", migrationCount)
	}
}

func TestStoreRejectsDuplicateURL(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	work, err := manga.NewWork(manga.NewWorkInput{
		URL:   "https://example.com/manga/1",
		Title: "作品名",
	}, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewWork() error = %v", err)
	}
	if _, err := store.CreateWork(ctx, work); err != nil {
		t.Fatalf("first CreateWork() error = %v", err)
	}
	if _, err := store.CreateWork(ctx, work); err == nil {
		t.Fatal("second CreateWork() error = nil, want duplicate error")
	}
}

func TestWorkByIDReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	_, err = store.WorkByID(ctx, 999)
	if !errors.Is(err, ErrWorkNotFound) {
		t.Fatalf("WorkByID() error = %v, want %v", err, ErrWorkNotFound)
	}
}
