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
	if migrationCount != 2 {
		t.Fatalf("migration count = %d, want 2", migrationCount)
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
	if _, err := store.CreateWork(ctx, work); !errors.Is(err, manga.ErrDuplicateURL) {
		t.Fatalf("second CreateWork() error = %v, want duplicate error", err)
	}
}

func TestListWorksOrderEmptyAndFailures(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	list, err := store.ListWorks(ctx)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty list = %#v, %v", list, err)
	}
	for _, url := range []string{"https://example.com/z", "https://example.com/a"} {
		work, err := manga.NewWork(manga.NewWorkInput{URL: url, Title: "作品"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateWork(ctx, work); err != nil {
			t.Fatal(err)
		}
	}
	list, err = store.ListWorks(ctx)
	if err != nil || len(list) != 2 || list[0].ID >= list[1].ID || list[0].URL != "https://example.com/z" {
		t.Fatalf("list = %#v, %v", list, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ListWorks(cancelled); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := store.db.Exec("UPDATE works SET created_at = 'invalid', updated_at = 'invalid' WHERE id = ?", list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListWorks(ctx); err == nil {
		t.Fatal("accepted invalid timestamp")
	}
	store.Close()
	if _, err := store.ListWorks(ctx); err == nil {
		t.Fatal("accepted closed database")
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
