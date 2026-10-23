package application

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func confirmedDelete(w manga.Work) manga.WorkDeleteInput {
	return manga.WorkDeleteInput{URL: w.URL, Title: w.Title, SiteName: w.SiteName, Notes: w.Notes, ThumbnailPath: w.ThumbnailPath}
}

func hasDeleteWarning(r WorkDeleteResult, code string) bool {
	for _, got := range r.Warnings {
		if got == code {
			return true
		}
	}
	return false
}

func TestWorkDeleterImagesAndStartupCleanup(t *testing.T) {
	for _, mode := range []string{"normal", "shared", "unknown", "changed-file", "image-stop", "forget-failure", "release", "stop-before-delete"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			covers, s, original, dir := importFixture(t)
			selected, err := covers.Select(ctx, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			w := *selected.Work
			name := filepath.Join(dir, filepath.FromSlash(w.ThumbnailPath))
			id := strings.TrimSuffix(strings.TrimPrefix(w.ThumbnailPath, "covers/"), ".png")
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			originalData := bytes.Clone(data)
			// A synthetic original outside managed covers must never be deleted.
			originalName := filepath.Join(dir, "original.png")
			if err := os.WriteFile(originalName, data, 0600); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if mode == "shared" {
				other := w
				other.ID, other.URL = 0, "https://example.com/shared"
				if _, err := s.CreateWork(ctx, other); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unknown" {
				if _, err := db.Exec("DELETE FROM managed_covers WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed-file" {
				data[0] ^= 1
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "image-stop" {
				covers.operations.Stop()
			}
			if mode == "forget-failure" {
				if _, err := db.Exec("CREATE TRIGGER reject_forget BEFORE DELETE ON managed_covers BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "release" {
				covers.delete = func(context.Context, string, string, platform.CoverFileInfo) error { return platform.ErrCoverRelease }
			}
			if mode == "stop-before-delete" {
				covers.repository = cleanupRepositoryHook{CoverRepository: s, afterLookup: covers.operations.Stop}
			}
			result, err := NewWorkDeleter(s, covers).DeleteWork(ctx, w.ID, confirmedDelete(w))
			if err != nil || result.Work == nil || !reflect.DeepEqual(*result.Work, w) || result.Warnings == nil {
				t.Fatal(result, err)
			}
			if _, err := s.WorkByID(ctx, w.ID); !errors.Is(err, storage.ErrWorkNotFound) {
				t.Fatal(err)
			}
			warning := mode != "normal" && mode != "shared"
			if hasDeleteWarning(result, "cover_cleanup_failed") != warning {
				t.Fatal(result)
			}
			gone := mode == "normal" || mode == "forget-failure"
			after, fileErr := os.ReadFile(name)
			if gone != errors.Is(fileErr, os.ErrNotExist) || !gone && (fileErr != nil || !bytes.Equal(after, data)) {
				t.Fatal(fileErr)
			}
			if got, err := os.ReadFile(originalName); err != nil || !bytes.Equal(got, originalData) {
				t.Fatal(err)
			}
			_, recordErr := s.ManagedCoverByID(ctx, id)
			if (mode == "normal" || mode == "unknown") != errors.Is(recordErr, storage.ErrManagedCoverNotFound) {
				t.Fatal(recordErr)
			}
			if mode != "normal" && mode != "unknown" && recordErr != nil {
				t.Fatal(recordErr)
			}
			if mode == "image-stop" || mode == "stop-before-delete" {
				if !hasDeleteWarning(result, "cover_unavailable_after_commit") {
					t.Fatal(result)
				}
			}
			if mode == "release" && !hasDeleteWarning(result, "cover_release_unconfirmed") {
				t.Fatal(result)
			}
			if mode == "image-stop" || mode == "forget-failure" {
				if mode == "forget-failure" {
					if _, err := db.Exec("DROP TRIGGER reject_forget"); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				r, err := storage.Open(ctx, filepath.Join(dir, "fixture.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				var operations CoverOperations
				restarted := NewCoverImporter(r, &operations, dir, nil, time.Now)
				op, err := operations.BeginCleanup()
				if err != nil {
					t.Fatal(err)
				}
				warnings, err := restarted.Cleanup(ctx, op)
				if err != nil || len(warnings) != 0 {
					t.Fatal(warnings, err)
				}
				if _, err := r.ManagedCoverByID(ctx, id); !errors.Is(err, storage.ErrManagedCoverNotFound) {
					t.Fatal(err)
				}
				if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestWorkDeleterRetainsReferencesThroughRelease(t *testing.T) {
	covers, s, old, _ := importFixture(t)
	ctx := context.Background()
	first, err := covers.Select(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := *first.Work
	prepare := covers.prepare
	released := false
	covers.prepare = func(ctx context.Context, directory string) (io.Closer, error) {
		guard, err := prepare(ctx, directory)
		if err != nil {
			return nil, err
		}
		return importCloser{close: func() error {
			if len(covers.operations.references) != 1 {
				t.Fatal("reference gate released before guard")
			}
			if _, err := covers.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) {
				t.Fatal("close admitted before release", err)
			}
			if _, err := covers.operations.BeginChange(w.ID, false); !errors.Is(err, ErrCoverBusy) {
				t.Fatal("same image change admitted", err)
			}
			released = true
			return errors.Join(guard.Close(), errors.New("synthetic guard release failure"))
		}}, nil
	}
	result, err := NewWorkDeleter(s, covers).DeleteWork(ctx, w.ID, confirmedDelete(w))
	if err != nil || result.Work == nil || !released || !hasDeleteWarning(result, "cover_release_unconfirmed") {
		t.Fatal(result, err)
	}
	if len(covers.operations.references) != 0 {
		t.Fatal("reference gate leaked")
	}
	if _, err := covers.operations.BeginRead(); !errors.Is(err, ErrCoverStopped) {
		t.Fatal("release did not stop images", err)
	}
	if _, err := covers.operations.RequestClose(); err != nil {
		t.Fatal("close remains rejected", err)
	}
}

func TestWorkDeleterRealRefusalsAndNoImage(t *testing.T) {
	covers, s, w, _ := importFixture(t)
	covers.prepare = func(context.Context, string) (io.Closer, error) { t.Fatal("unexpected preparation"); return nil, nil }
	a := NewWorkDeleter(s, covers)
	expected := confirmedDelete(w)
	expected.Title += " "
	if result, err := a.DeleteWork(context.Background(), w.ID, expected); !errors.Is(err, storage.ErrWorkDeleteChanged) || result.Work != nil {
		t.Fatal(result, err)
	}
	if result, err := a.DeleteWork(context.Background(), -1, confirmedDelete(w)); !errors.Is(err, storage.ErrWorkNotFound) || result.Work != nil {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := a.DeleteWork(ctx, w.ID, confirmedDelete(w)); !errors.Is(err, context.Canceled) || result.Work != nil {
		t.Fatal(result, err)
	}
	if len(covers.operations.references) != 0 {
		t.Fatal("reference gate leaked")
	}
	plain, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/no-image", Title: "No image"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	plain, err = s.CreateWork(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	covers.operations.Stop()
	result, err := a.DeleteWork(context.Background(), plain.ID, confirmedDelete(plain))
	if err != nil || result.Work == nil || len(result.Warnings) != 0 {
		t.Fatal(result, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := a.DeleteWork(context.Background(), w.ID, confirmedDelete(w)); !errors.Is(err, storage.ErrDBStopped) || result.Work != nil {
		t.Fatal(result, err)
	}
}

type deleteRepositoryFunc func(context.Context, int64, manga.WorkDeleteInput) (manga.Work, error)

func (f deleteRepositoryFunc) DeleteWork(ctx context.Context, id int64, input manga.WorkDeleteInput) (manga.Work, error) {
	return f(ctx, id, input)
}

func TestWorkDeleterOutcomesDoNotCleanOrRetry(t *testing.T) {
	for _, cause := range []error{storage.ErrWorkNotFound, storage.ErrWorkDeleteChanged, storage.ErrDBStopped, errors.Join(storage.ErrWorkDeleteOutcomeUnknown, storage.ErrDBStopped), errors.Join(storage.ErrWorkDeleteCommitted, storage.ErrDBStopped)} {
		t.Run(cause.Error(), func(t *testing.T) {
			covers, _, w, _ := importFixture(t)
			covers.prepare = func(context.Context, string) (io.Closer, error) {
				t.Fatal("unexpected image preparation")
				return nil, nil
			}
			calls := 0
			expected := confirmedDelete(w)
			repo := deleteRepositoryFunc(func(_ context.Context, id int64, input manga.WorkDeleteInput) (manga.Work, error) {
				calls++
				if id != w.ID || input != expected {
					t.Fatal(id, input)
				}
				return w, cause
			})
			result, err := NewWorkDeleter(repo, covers).DeleteWork(context.Background(), w.ID, expected)
			if calls != 1 {
				t.Fatal(calls)
			}
			if errors.Is(cause, storage.ErrWorkDeleteCommitted) {
				if err != nil || result.Work == nil || !hasDeleteWarning(result, "database_stopped_after_commit") || !hasDeleteWarning(result, "cover_cleanup_failed") {
					t.Fatal(result, err)
				}
			} else if !errors.Is(err, cause) || result.Work != nil {
				t.Fatal(result, err)
			}
		})
	}
}
