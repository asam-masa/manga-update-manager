package application

import (
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
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type startupRepositoryHook struct {
	CoverRepository
	list   func(context.Context) ([]storage.ManagedCover, error)
	lookup func(context.Context, string) (storage.ManagedCover, bool, error)
}

func (r startupRepositoryHook) ListManagedCovers(ctx context.Context) ([]storage.ManagedCover, error) {
	if r.list != nil {
		return r.list(ctx)
	}
	return r.CoverRepository.ListManagedCovers(ctx)
}
func (r startupRepositoryHook) UnusedManagedCover(ctx context.Context, id string) (storage.ManagedCover, bool, error) {
	if r.lookup != nil {
		return r.lookup(ctx, id)
	}
	return r.CoverRepository.UnusedManagedCover(ctx, id)
}

func TestStartupCleanupRealFilesAndFailures(t *testing.T) {
	for _, mode := range []string{"normal", "referenced", "reserved", "unknown-reference", "inventory-failure", "late-reference-failure", "missing", "changed", "forget-failure", "release", "stop"} {
		t.Run(mode, func(t *testing.T) {
			a, s, work, dir := importFixture(t)
			ctx := context.Background()
			first, err := a.Select(ctx, work.ID)
			if err != nil {
				t.Fatal(err)
			}
			path := first.Work.ThumbnailPath
			id := strings.TrimSuffix(strings.TrimPrefix(path, "covers/"), ".png")
			name := filepath.Join(dir, filepath.FromSlash(path))
			if mode != "referenced" {
				if _, _, err := s.RemoveWorkCover(ctx, work.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.WorkByID(ctx, work.ID)
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			switch mode {
			case "reserved":
				if err := s.ReserveManagedCover(ctx, strings.Repeat("a", 32)); err != nil {
					t.Fatal(err)
				}
			case "unknown-reference":
				other, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/other", Title: "Other", ThumbnailPath: "legacy/possible-alias.png"}, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.CreateWork(ctx, other); err != nil {
					t.Fatal(err)
				}
			case "inventory-failure":
				a.repository = startupRepositoryHook{CoverRepository: s, list: func(context.Context) ([]storage.ManagedCover, error) {
					return nil, errors.New("synthetic inventory failure")
				}}
			case "late-reference-failure":
				second, err := a.Select(ctx, work.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.RemoveWorkCover(ctx, work.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
				before, err = s.WorkByID(ctx, work.ID)
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				a.repository = startupRepositoryHook{CoverRepository: s, lookup: func(ctx context.Context, id string) (storage.ManagedCover, bool, error) {
					count++
					if count == 2 {
						return storage.ManagedCover{}, false, errors.New("synthetic second reference failure")
					}
					return s.UnusedManagedCover(ctx, id)
				}}
				defer func() {
					if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(second.Work.ThumbnailPath))); err != nil {
						t.Error("second image removed before preflight completed", err)
					}
				}()
			case "missing":
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := os.WriteFile(name, []byte("synthetic changed file"), 0600); err != nil {
					t.Fatal(err)
				}
			case "forget-failure":
				if _, err := db.Exec("CREATE TRIGGER reject_startup_cleanup BEFORE DELETE ON managed_covers BEGIN SELECT RAISE(ABORT,'synthetic cleanup failure'); END"); err != nil {
					t.Fatal(err)
				}
			case "release":
				prepare := a.prepare
				a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
					guard, err := prepare(ctx, dir)
					if err != nil {
						return nil, err
					}
					return importCloser{func() error {
						if len(a.operations.references) != 1 {
							t.Error("gate released before guard")
						}
						if _, err := a.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) {
							t.Error("close accepted during guard release", err)
						}
						return errors.Join(guard.Close(), errors.New("synthetic release failure"))
					}}, nil
				}
			case "stop":
				a.repository = startupRepositoryHook{CoverRepository: s, list: func(ctx context.Context) ([]storage.ManagedCover, error) {
					records, err := s.ListManagedCovers(ctx)
					a.operations.Stop()
					return records, err
				}}
			}
			unknown := filepath.Join(dir, "covers", "unknown.png")
			if err := os.WriteFile(unknown, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			op, err := a.operations.BeginCleanup()
			if err != nil {
				t.Fatal(err)
			}
			warnings, err := a.Cleanup(ctx, op)
			success := mode == "normal" || mode == "referenced" || mode == "reserved" || mode == "missing"
			if (err == nil) != success {
				t.Fatal(warnings, err)
			}
			if (len(warnings) > 0) != (mode == "reserved") {
				t.Fatal(warnings)
			}
			deleted := mode == "normal" || mode == "reserved" || mode == "missing" || mode == "forget-failure" || mode == "release"
			_, statErr := os.Stat(name)
			if deleted {
				if !errors.Is(statErr, os.ErrNotExist) {
					t.Fatal(statErr)
				}
			} else if statErr != nil {
				t.Fatal("unverified image removed", statErr)
			}
			_, recordErr := s.ManagedCoverByID(ctx, id)
			if deleted && mode != "forget-failure" {
				if !errors.Is(recordErr, storage.ErrManagedCoverNotFound) {
					t.Fatal(recordErr)
				}
			} else if recordErr != nil {
				t.Fatal("record lost", recordErr)
			}
			after, dbErr := s.WorkByID(ctx, work.ID)
			if dbErr != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("work changed", after, dbErr)
			}
			data, readErr := os.ReadFile(unknown)
			if readErr != nil || string(data) != "untouched" {
				t.Fatal("unknown file changed", readErr)
			}
			if mode == "reserved" {
				if c, e := s.ManagedCoverByID(ctx, strings.Repeat("a", 32)); e != nil || c.State != "reserved" {
					t.Fatal(c, e)
				}
			}
			if mode == "release" || mode == "stop" {
				if _, e := a.operations.BeginChange(work.ID, false); !errors.Is(e, ErrCoverStopped) {
					t.Fatal(e)
				}
			}
			if mode == "forget-failure" {
				if _, e := db.Exec("DROP TRIGGER reject_startup_cleanup"); e != nil {
					t.Fatal(e)
				}
				if e := s.Close(); e != nil {
					t.Fatal(e)
				}
				reopened, e := storage.Open(ctx, filepath.Join(dir, "fixture.sqlite"))
				if e != nil {
					t.Fatal(e)
				}
				defer reopened.Close()
				a.repository = reopened
				op, e := a.operations.BeginCleanup()
				if e != nil {
					t.Fatal(e)
				}
				if _, e := a.Cleanup(ctx, op); e != nil {
					t.Fatal("absent-file retry failed", e)
				}
			}
		})
	}
}

func TestStartupCleanupCancellationAndInvalidTicket(t *testing.T) {
	a, _, _, _ := importFixture(t)
	if _, err := a.Cleanup(context.Background(), nil); !errors.Is(err, ErrCoverOperationInvalid) {
		t.Fatal(err)
	}
	op, err := a.operations.BeginCleanup()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Cleanup(ctx, op); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if done, err := a.operations.RequestClose(); err != nil {
		t.Fatal(err)
	} else {
		select {
		case <-done:
		default:
			t.Fatal("ticket not drained")
		}
	}
}
