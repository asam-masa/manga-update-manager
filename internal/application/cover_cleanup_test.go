package application

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type cleanupRepositoryHook struct {
	CoverRepository
	afterLookup func()
}

func (r cleanupRepositoryHook) UnusedManagedCover(ctx context.Context, id string) (storage.ManagedCover, bool, error) {
	c, unused, err := r.CoverRepository.UnusedManagedCover(ctx, id)
	if r.afterLookup != nil {
		r.afterLookup()
	}
	return c, unused, err
}

func TestCoverCleanupPostCommitOutcomes(t *testing.T) {
	for _, removing := range []bool{false, true} {
		t.Run(map[bool]string{false: "select", true: "remove"}[removing], func(t *testing.T) {
			testCoverCleanupPostCommitOutcomes(t, removing)
		})
	}
}

func testCoverCleanupPostCommitOutcomes(t *testing.T, removing bool) {
	for _, mode := range []string{"normal", "shared", "shared-case", "uncertain-alias", "changed", "missing-record", "reserved", "forget-failure", "release", "stop-before-delete", "stop-before-forget"} {
		t.Run(mode, func(t *testing.T) {
			a, s, original, dir := importFixture(t)
			ctx := context.Background()
			first, err := a.Select(ctx, original.ID)
			if err != nil || first.Work == nil {
				t.Fatal(first, err)
			}
			old := *first.Work
			id := strings.TrimSuffix(strings.TrimPrefix(old.ThumbnailPath, "covers/"), ".png")
			name := filepath.Join(dir, filepath.FromSlash(old.ThumbnailPath))
			db, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			switch mode {
			case "shared", "shared-case", "uncertain-alias":
				path := old.ThumbnailPath
				if mode == "shared-case" {
					path = strings.ToUpper(path)
				}
				if mode == "uncertain-alias" {
					path += "."
				}
				other, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/shared", Title: "Other", ThumbnailPath: path}, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.CreateWork(ctx, other); err != nil {
					t.Fatal(err)
				}
			case "changed":
				data, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				data[0] ^= 1
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-record":
				if _, err := db.Exec("DELETE FROM managed_covers WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			case "reserved":
				if _, err := db.Exec("UPDATE managed_covers SET state='reserved',volume_serial=NULL,file_id=NULL,byte_size=NULL,sha256=NULL WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			case "forget-failure":
				if _, err := db.Exec("CREATE TRIGGER reject_old_cleanup BEFORE DELETE ON managed_covers BEGIN SELECT RAISE(ABORT,'synthetic cleanup failure'); END"); err != nil {
					t.Fatal(err)
				}
			case "release":
				a.delete = func(context.Context, string, string, platform.CoverFileInfo) error { return platform.ErrCoverRelease }
			case "stop-before-delete":
				a.repository = cleanupRepositoryHook{CoverRepository: s, afterLookup: a.operations.Stop}
			case "stop-before-forget":
				remove := a.delete
				a.delete = func(ctx context.Context, dir, id string, info platform.CoverFileInfo) error {
					err := remove(ctx, dir, id, info)
					a.operations.Stop()
					return err
				}
			}
			var got CoverImportResult
			if removing {
				got, err = a.Remove(ctx, old.ID)
			} else {
				got, err = a.Select(ctx, old.ID)
			}
			if err != nil || got.Work == nil || got.Work.ThumbnailPath == old.ThumbnailPath {
				t.Fatal("committed result lost", got, err)
			}
			warning := mode != "normal" && mode != "shared" && mode != "shared-case"
			if containsWarning(got, "cover_cleanup_failed") != warning {
				t.Fatal(got)
			}
			if mode == "release" && !containsWarning(got, "cover_release_unconfirmed") {
				t.Fatal(got)
			}
			if strings.HasPrefix(mode, "stop-") && !containsWarning(got, "cover_unavailable_after_commit") {
				t.Fatal(got)
			}
			fileGone := mode == "normal" || mode == "forget-failure" || mode == "stop-before-forget"
			_, fileErr := os.Lstat(name)
			if fileGone != errors.Is(fileErr, os.ErrNotExist) || (!fileGone && fileErr != nil) {
				t.Fatal("wrong file outcome", fileErr)
			}
			_, recordErr := s.ManagedCoverByID(ctx, id)
			if mode == "normal" || mode == "missing-record" {
				if !errors.Is(recordErr, storage.ErrManagedCoverNotFound) {
					t.Fatal(recordErr)
				}
			} else if recordErr != nil {
				t.Fatal("cleanup record lost", recordErr)
			}
			stored, err := s.WorkByID(ctx, old.ID)
			if err != nil || stored.ThumbnailPath != got.Work.ThumbnailPath || !stored.UpdatedAt.Equal(got.Work.UpdatedAt) || !stored.CreatedAt.Equal(old.CreatedAt) {
				t.Fatal(stored, err)
			}
			if mode == "release" || strings.HasPrefix(mode, "stop-") {
				if _, err := a.Select(ctx, old.ID); !errors.Is(err, ErrCoverStopped) {
					t.Fatal(err)
				}
			}
			if mode == "forget-failure" {
				if _, err := db.Exec("DROP TRIGGER reject_old_cleanup"); err != nil {
					t.Fatal(err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := storage.Open(ctx, filepath.Join(dir, "fixture.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				a.repository = reopened
				runAbsentCleanup(t, a, old.ThumbnailPath)
				if _, err := reopened.ManagedCoverByID(ctx, id); !errors.Is(err, storage.ErrManagedCoverNotFound) {
					t.Fatal("absence retry did not forget", err)
				}
			}
		})
	}
}

// Exercise the production cleanup helper after reopening, with its required
// exclusion, retained ancestors, stage admission and release notification.
// This is not an automatic startup cleanup implementation.
func runAbsentCleanup(t *testing.T, a *CoverImporter, path string) {
	t.Helper()
	ctx := context.Background()
	op, err := a.operations.BeginCleanup()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := op.End(); err != nil {
			t.Error(err)
		}
	}()
	unlock, err := a.operations.LockReferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	guard, err := platform.PrepareCoverDirectory(ctx, a.directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := guard.Close()
		a.operations.ObserveError(err)
		if err != nil {
			t.Error(err)
		}
	}()
	stage := func(fn func() error) error {
		ticket, err := op.BeginStage()
		if err != nil {
			return err
		}
		err = fn()
		a.operations.ObserveError(err)
		ticket.End()
		return err
	}
	if err := a.cleanupPrevious(ctx, path, stage); err != nil {
		t.Fatal(err)
	}
}
