package application

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func importFixture(t *testing.T) (*CoverImporter, *storage.Store, manga.Work, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.Open(context.Background(), filepath.Join(dir, "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	w, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/synthetic", Title: "Synthetic", ThumbnailPath: "legacy/old.png"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.CreateWork(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	var operations CoverOperations
	a := NewCoverImporter(s, &operations, dir, func(context.Context) (string, error) { return "selected-source", nil }, time.Now)
	a.openSource = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(source.Bytes())), nil
	}
	return a, s, w, dir
}

func TestCoverImportRealFileAndDB(t *testing.T) {
	a, s, old, dir := importFixture(t)
	got, err := a.Select(context.Background(), old.ID)
	if err != nil || got.Cancelled || got.Work == nil || len(got.Warnings) != 1 || got.Warnings[0] != "cover_cleanup_failed" {
		t.Fatalf("%#v %v", got, err)
	}
	path := got.Work.ThumbnailPath
	if !strings.HasPrefix(path, "covers/") || !strings.HasSuffix(path, ".png") || path == old.ThumbnailPath {
		t.Fatal(path)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "covers/"), ".png")
	record, err := s.ManagedCoverByID(context.Background(), id)
	if err != nil || record.State != "ready" || record.Info == nil || record.Info.ByteSize <= 0 {
		t.Fatal(record, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	stored, err := s.WorkByID(context.Background(), old.ID)
	if err != nil || stored.ThumbnailPath != path || !stored.CreatedAt.Equal(old.CreatedAt) || stored.LastAccessedAt != nil {
		t.Fatal(stored, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(context.Background(), filepath.Join(dir, "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err = reopened.WorkByID(context.Background(), old.ID)
	if err != nil || stored.ThumbnailPath != path {
		t.Fatal(stored, err)
	}
}

func TestCoverImportCancelAndSourceFailureDoNotPrepare(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		a, s, old, dir := importFixture(t)
		if cancel {
			a.choose = func(context.Context) (string, error) { return "", nil }
		} else {
			a.openSource = func(context.Context, string) (io.ReadCloser, error) { return nil, errors.New("synthetic read failure") }
		}
		result, err := a.Select(context.Background(), old.ID)
		if cancel && (err != nil || !result.Cancelled || result.Work != nil || len(result.Warnings) != 0) {
			t.Fatal(result, err)
		}
		if !cancel && err == nil {
			t.Fatal("source failure ignored")
		}
		if _, err := os.Stat(filepath.Join(dir, "covers")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("prepared before source success", err)
		}
		stored, err := s.WorkByID(context.Background(), old.ID)
		if err != nil || stored.ThumbnailPath != old.ThumbnailPath {
			t.Fatal(stored, err)
		}
	}
}

type importCloser struct{ close func() error }

func (c importCloser) Close() error { return c.close() }

type importSource struct{ io.ReadCloser }

func (s importSource) Close() error {
	return errors.Join(s.ReadCloser.Close(), errors.New("synthetic source release"))
}

func TestCoverImportSourceReleaseStopsBeforePrepare(t *testing.T) {
	a, s, old, dir := importFixture(t)
	open := a.openSource
	a.openSource = func(ctx context.Context, path string) (io.ReadCloser, error) {
		r, err := open(ctx, path)
		return importSource{r}, err
	}
	got, err := a.Select(context.Background(), old.ID)
	if !errors.Is(err, platform.ErrCoverRelease) || got.Work != nil {
		t.Fatal(got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "covers")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prepared after source release failure", err)
	}
	if _, err := a.Select(context.Background(), old.ID); !errors.Is(err, ErrCoverStopped) {
		t.Fatal(err)
	}
	stored, err := s.WorkByID(context.Background(), old.ID)
	if err != nil || stored.ThumbnailPath != old.ThumbnailPath {
		t.Fatal(stored, err)
	}
}

func TestCoverImportReleaseOutcomesAndStageStop(t *testing.T) {
	for _, mode := range []string{"prepare-stop", "write-release", "guard-release"} {
		t.Run(mode, func(t *testing.T) {
			a, s, old, _ := importFixture(t)
			prepare := a.prepare
			a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
				guard, err := prepare(ctx, dir)
				if err != nil {
					return nil, err
				}
				if mode == "prepare-stop" {
					a.operations.Stop()
				}
				return importCloser{func() error {
					// The work ticket must survive through final resource release.
					if _, err := a.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) {
						t.Error("operation ended before release", err)
					}
					err := guard.Close()
					if mode == "guard-release" {
						if len(a.operations.references) != 1 {
							t.Error("reference exclusion ended before guard release")
						}
						err = errors.Join(err, errors.New("synthetic guard release"))
					}
					return err
				}}, nil
			}
			writes := 0
			write := a.write
			a.write = func(ctx context.Context, dir, id string, data []byte) (platform.CoverFileInfo, error) {
				writes++
				if mode == "write-release" {
					return platform.CoverFileInfo{}, errors.Join(platform.ErrCoverRelease, errors.New("synthetic write release"))
				}
				return write(ctx, dir, id, data)
			}
			result, err := a.Select(context.Background(), old.ID)
			if mode == "guard-release" {
				if err != nil || result.Work == nil || !containsWarning(result, "cover_release_unconfirmed") {
					t.Fatal(result, err)
				}
			} else if err == nil || result.Work != nil {
				t.Fatal(result, err)
			}
			if mode == "prepare-stop" && writes != 0 {
				t.Fatal("write started after stop")
			}
			stored, dbErr := s.WorkByID(context.Background(), old.ID)
			if dbErr != nil {
				t.Fatal(dbErr)
			}
			if (stored.ThumbnailPath != old.ThumbnailPath) != (mode == "guard-release") {
				t.Fatal(stored)
			}
			if _, err := a.Select(context.Background(), old.ID); !errors.Is(err, ErrCoverStopped) {
				t.Fatal("not stopped", err)
			}
		})
	}
}

func containsWarning(result CoverImportResult, code string) bool {
	for _, warning := range result.Warnings {
		if warning == code {
			return true
		}
	}
	return false
}

type importRepositoryHook struct {
	CoverRepository
	afterConfirm func() error
	set          func(context.Context, int64, string, time.Time) (manga.Work, string, error)
}

func (r importRepositoryHook) ConfirmManagedCover(ctx context.Context, id string, info storage.ManagedCoverInfo) error {
	if err := r.CoverRepository.ConfirmManagedCover(ctx, id, info); err != nil {
		return err
	}
	if r.afterConfirm != nil {
		return r.afterConfirm()
	}
	return nil
}
func (r importRepositoryHook) SetWorkCover(ctx context.Context, id int64, coverID string, at time.Time) (manga.Work, string, error) {
	if r.set != nil {
		return r.set(ctx, id, coverID, at)
	}
	return r.CoverRepository.SetWorkCover(ctx, id, coverID, at)
}

func TestCoverImportRealCommitBusyPreservesOutput(t *testing.T) {
	a, s, old, dir := importFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	observer, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	var read *sql.Tx
	a.repository = importRepositoryHook{CoverRepository: s, afterConfirm: func() error {
		var err error
		read, err = observer.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var title string
		return read.QueryRowContext(ctx, "SELECT title FROM works WHERE id=?", old.ID).Scan(&title)
	}}
	defer func() {
		if read != nil {
			read.Rollback()
		}
	}()
	got, err := a.Select(ctx, old.ID)
	if !errors.Is(err, storage.ErrWorkCoverOutcomeUnknown) || !errors.Is(err, storage.ErrDBStopped) || got.Work != nil {
		t.Fatal(got, err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "covers"))
	if err != nil || len(files) != 1 {
		t.Fatal("new image was not preserved", files, err)
	}
	if _, err := a.Select(ctx, old.ID); !errors.Is(err, ErrCoverStopped) {
		t.Fatal(err)
	}
	if err := read.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkByID(ctx, old.ID); !errors.Is(err, storage.ErrDBStopped) {
		t.Fatal(err)
	}
}

func TestCoverImportCommittedWarningDoesNotBecomeFailure(t *testing.T) {
	a, s, old, _ := importFixture(t)
	a.repository = importRepositoryHook{CoverRepository: s, set: func(ctx context.Context, id int64, coverID string, at time.Time) (manga.Work, string, error) {
		w, previous, err := s.SetWorkCover(ctx, id, coverID, at)
		if err != nil {
			return w, previous, err
		}
		return w, previous, errors.Join(storage.ErrWorkCoverCommitted, storage.ErrDBStopped, errors.New("synthetic post-commit cleanup failure"))
	}}
	got, err := a.Select(context.Background(), old.ID)
	if err != nil || got.Work == nil || !containsWarning(got, "database_stopped_after_commit") || !containsWarning(got, "cover_cleanup_failed") {
		t.Fatal(got, err)
	}
	if _, err := a.Select(context.Background(), old.ID); !errors.Is(err, ErrCoverStopped) {
		t.Fatal(err)
	}
}

func TestCoverImportSelectionExclusionAndClose(t *testing.T) {
	a, _, old, _ := importFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	a.choose = func(context.Context) (string, error) {
		close(entered)
		select {
		case <-release:
			return "", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() { _, err := a.Select(ctx, old.ID); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := a.Select(ctx, old.ID); !errors.Is(err, ErrCoverBusy) {
		t.Fatal(err)
	}
	if _, err := a.Select(ctx, old.ID+1); !errors.Is(err, ErrCoverBusy) {
		t.Fatal(err)
	}
	if _, err := a.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if done, err := a.operations.RequestClose(); err != nil {
		t.Fatal(err)
	} else {
		select {
		case <-done:
		default:
			t.Fatal("not drained")
		}
	}
}

func TestCoverImportReferenceWait(t *testing.T) {
	for _, mode := range []string{"complete", "stop", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, s, old, dir := importFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			other, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/other", Title: "Other"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			other, err = s.CreateWork(ctx, other)
			if err != nil {
				t.Fatal(err)
			}
			unlock, err := a.operations.LockReferences(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			written := make(chan struct{})
			write := a.write
			a.write = func(ctx context.Context, dir, id string, data []byte) (platform.CoverFileInfo, error) {
				info, err := write(ctx, dir, id, data)
				close(written)
				return info, err
			}
			selectCount := 0
			a.choose = func(context.Context) (string, error) {
				selectCount++
				if selectCount == 1 {
					return "selected-source", nil
				}
				return "", nil
			}
			type outcome struct {
				result CoverImportResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() { r, err := a.Select(ctx, old.ID); done <- outcome{r, err} }()
			select {
			case <-written:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Saved but not confirmed: cleanup must not treat this as ready.
			files, err := os.ReadDir(filepath.Join(dir, "covers"))
			if err != nil || len(files) != 1 {
				t.Fatal(files, err)
			}
			id := strings.TrimSuffix(files[0].Name(), ".png")
			record, err := s.ManagedCoverByID(ctx, id)
			if err != nil || record.State != "reserved" {
				t.Fatal("confirmation bypassed reference gate", record, err)
			}
			if r, err := a.Select(ctx, other.ID); err != nil || !r.Cancelled {
				t.Fatal("other selection blocked by reference section", r, err)
			}
			if _, err := a.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) {
				t.Fatal("close accepted during reference wait", err)
			}
			if mode == "stop" {
				a.operations.Stop()
			}
			if mode == "cancel" {
				cancel()
			} else {
				unlock()
			}
			var got outcome
			select {
			case got = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("reference wait did not complete")
			}
			if mode == "complete" {
				if got.err != nil || got.result.Work == nil {
					t.Fatal(got)
				}
			} else {
				want := ErrCoverStopped
				if mode == "cancel" {
					want = context.Canceled
				}
				if !errors.Is(got.err, want) || got.result.Work != nil {
					t.Fatal(got)
				}
				stored, err := s.WorkByID(context.Background(), old.ID)
				if err != nil || stored.ThumbnailPath != old.ThumbnailPath {
					t.Fatal(stored, err)
				}
				record, err := s.ManagedCoverByID(context.Background(), id)
				if err != nil || record.State != "reserved" {
					t.Fatal(record, err)
				}
			}
		})
	}
}
