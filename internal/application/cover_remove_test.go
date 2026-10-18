package application

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type removeRepositoryHook struct {
	CoverRepository
	remove func(context.Context, int64, time.Time) (manga.Work, string, error)
}

func (r removeRepositoryHook) RemoveWorkCover(ctx context.Context, id int64, at time.Time) (manga.Work, string, error) {
	return r.remove(ctx, id, at)
}

func TestCoverRemoveNoOpAndLegacy(t *testing.T) {
	a, s, old, _ := importFixture(t)
	a.choose = func(context.Context) (string, error) { t.Fatal("removal opened chooser"); return "", nil }
	a.openSource = func(context.Context, string) (io.ReadCloser, error) { t.Fatal("removal read source"); return nil, nil }
	got, err := a.Remove(context.Background(), old.ID)
	if err != nil || got.Work == nil || got.Cancelled || got.Work.ThumbnailPath != "" || !containsWarning(got, "cover_cleanup_failed") {
		t.Fatal(got, err)
	}
	if !got.Work.CreatedAt.Equal(old.CreatedAt) || got.Work.URL != old.URL || got.Work.Title != old.Title || got.Work.LastAccessedAt != nil {
		t.Fatal("unrelated fields changed", got.Work)
	}
	again, err := a.Remove(context.Background(), old.ID)
	if err != nil || again.Work == nil || !reflect.DeepEqual(*again.Work, *got.Work) || len(again.Warnings) != 0 {
		t.Fatal(again, err)
	}
	stored, err := s.WorkByID(context.Background(), old.ID)
	if err != nil || !reflect.DeepEqual(stored, *got.Work) {
		t.Fatal(stored, err)
	}
}

func TestCoverRemoveFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"prepare", "unsafe", "cancel", "stop", "unknown", "committed", "guard-release"} {
		t.Run(mode, func(t *testing.T) {
			a, s, original, dir := importFixture(t)
			first, err := a.Select(context.Background(), original.ID)
			if err != nil {
				t.Fatal(err)
			}
			old := *first.Work
			name := filepath.Join(dir, filepath.FromSlash(old.ThumbnailPath))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			prepare := a.prepare
			a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
				if mode == "prepare" {
					return nil, errors.New("synthetic prepare")
				}
				if mode == "unsafe" {
					return nil, platform.ErrUnsafeCoverDirectory
				}
				guard, err := prepare(ctx, dir)
				if err != nil {
					return nil, err
				}
				if mode == "cancel" {
					cancel()
				}
				if mode == "stop" {
					a.operations.Stop()
				}
				return importCloser{func() error {
					if _, err := a.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) {
						t.Error("close accepted before release", err)
					}
					err := guard.Close()
					if mode == "guard-release" {
						if len(a.operations.references) != 1 {
							t.Error("reference gate released before guard")
						}
						return errors.Join(err, errors.New("synthetic release"))
					}
					return err
				}}, nil
			}
			if mode == "unknown" || mode == "committed" {
				a.repository = removeRepositoryHook{CoverRepository: s, remove: func(ctx context.Context, id int64, at time.Time) (manga.Work, string, error) {
					if mode == "unknown" {
						return manga.Work{}, "", errors.Join(storage.ErrWorkCoverOutcomeUnknown, storage.ErrDBStopped)
					}
					w, previous, err := s.RemoveWorkCover(ctx, id, at)
					if err != nil {
						return w, previous, err
					}
					return w, previous, errors.Join(storage.ErrWorkCoverCommitted, storage.ErrDBStopped)
				}}
			}
			got, err := a.Remove(ctx, old.ID)
			committed := mode == "committed" || mode == "guard-release"
			if committed {
				if err != nil || got.Work == nil || got.Work.ThumbnailPath != "" {
					t.Fatal(got, err)
				}
				code := "database_stopped_after_commit"
				if mode == "guard-release" {
					code = "cover_release_unconfirmed"
				}
				if !containsWarning(got, code) {
					t.Fatal(got)
				}
			} else if err == nil || got.Work != nil {
				t.Fatal(got, err)
			}
			stored, dbErr := s.WorkByID(context.Background(), old.ID)
			if dbErr != nil || (stored.ThumbnailPath == "") != committed {
				t.Fatal(stored, dbErr)
			}
			_, statErr := os.Stat(name)
			if mode == "guard-release" {
				if !errors.Is(statErr, os.ErrNotExist) {
					t.Fatal(statErr)
				}
			} else if statErr != nil {
				t.Fatal("old image removed before confirmed commit", statErr)
			}
			if mode != "prepare" && mode != "cancel" {
				if _, err := a.Remove(context.Background(), old.ID); !errors.Is(err, ErrCoverStopped) {
					t.Fatal("image operation not stopped", err)
				}
			} else {
				op, err := a.operations.BeginChange(old.ID, false)
				if err != nil {
					t.Fatal("operation ticket leaked", err)
				}
				op.End()
			}
		})
	}
}

func TestCoverRemoveReferenceWaitAndExclusion(t *testing.T) {
	for _, mode := range []string{"complete", "cancel", "stop"} {
		t.Run(mode, func(t *testing.T) {
			a, s, old, _ := importFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			unlock, err := a.operations.LockReferences(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			prepared := make(chan struct{})
			prepare := a.prepare
			a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
				guard, err := prepare(ctx, dir)
				close(prepared)
				return guard, err
			}
			done := make(chan error, 1)
			go func() { _, err := a.Remove(ctx, old.ID); done <- err }()
			select {
			case <-prepared:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if _, err := a.Select(ctx, old.ID); !errors.Is(err, ErrCoverBusy) {
				t.Fatal(err)
			}
			if _, err := a.Remove(ctx, old.ID); !errors.Is(err, ErrCoverBusy) {
				t.Fatal(err)
			}
			if _, err := a.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) {
				t.Fatal(err)
			}
			stored, err := s.WorkByID(ctx, old.ID)
			if err != nil || stored.ThumbnailPath != old.ThumbnailPath {
				t.Fatal("reference bypassed exclusion", stored, err)
			}
			// A removal never occupies the global chooser slot.
			op, err := a.operations.BeginChange(old.ID+1, true)
			if err != nil {
				t.Fatal(err)
			}
			op.End()
			if mode == "cancel" {
				cancel()
			} else {
				if mode == "stop" {
					a.operations.Stop()
				}
				unlock()
			}
			select {
			case err := <-done:
				if mode == "complete" && err != nil {
					t.Fatal(err)
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if mode == "stop" && !errors.Is(err, ErrCoverStopped) {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("removal did not finish")
			}
		})
	}
}

func TestCoverRemoveRealCommitBusyKeepsImage(t *testing.T) {
	a, s, original, dir := importFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first, err := a.Select(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := *first.Work
	observer, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	read, err := observer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	var title string
	if err := read.QueryRowContext(ctx, "SELECT title FROM works WHERE id=?", old.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	got, err := a.Remove(ctx, old.ID)
	if !errors.Is(err, storage.ErrWorkCoverOutcomeUnknown) || !errors.Is(err, storage.ErrDBStopped) || got.Work != nil {
		t.Fatal(got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(old.ThumbnailPath))); err != nil {
		t.Fatal("image removed after unknown outcome", err)
	}
	if _, err := a.Remove(ctx, old.ID); !errors.Is(err, ErrCoverStopped) {
		t.Fatal(err)
	}
	if _, err := s.WorkByID(ctx, old.ID); !errors.Is(err, storage.ErrDBStopped) {
		t.Fatal(err)
	}
	if err := read.Rollback(); err != nil {
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
	stored, err := reopened.WorkByID(ctx, old.ID)
	if err != nil || stored.ThumbnailPath != old.ThumbnailPath {
		t.Fatal(stored, err)
	}
}
