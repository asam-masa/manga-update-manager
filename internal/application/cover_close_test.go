package application

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// Checks run at dependency boundaries, never by reentering a progress observer.
type closeCheckRepository struct {
	CoverRepository
	check func(string)
}

func (r closeCheckRepository) WorkByID(ctx context.Context, id int64) (manga.Work, error) {
	r.check("work")
	return r.CoverRepository.WorkByID(ctx, id)
}
func (r closeCheckRepository) ListManagedCovers(ctx context.Context) ([]storage.ManagedCover, error) {
	r.check("inventory")
	return r.CoverRepository.ListManagedCovers(ctx)
}
func (r closeCheckRepository) ReserveManagedCover(ctx context.Context, id string) error {
	r.check("reserve")
	return r.CoverRepository.ReserveManagedCover(ctx, id)
}
func (r closeCheckRepository) ConfirmManagedCover(ctx context.Context, id string, info storage.ManagedCoverInfo) error {
	r.check("confirm")
	return r.CoverRepository.ConfirmManagedCover(ctx, id, info)
}
func (r closeCheckRepository) SetWorkCover(ctx context.Context, id int64, cover string, now time.Time) (manga.Work, string, error) {
	r.check("set")
	return r.CoverRepository.SetWorkCover(ctx, id, cover, now)
}
func (r closeCheckRepository) RemoveWorkCover(ctx context.Context, id int64, now time.Time) (manga.Work, string, error) {
	r.check("remove")
	return r.CoverRepository.RemoveWorkCover(ctx, id, now)
}
func (r closeCheckRepository) UnusedManagedCover(ctx context.Context, id string) (storage.ManagedCover, bool, error) {
	r.check("unused")
	return r.CoverRepository.UnusedManagedCover(ctx, id)
}
func (r closeCheckRepository) ForgetUnusedManagedCover(ctx context.Context, cover storage.ManagedCover) error {
	r.check("forget")
	return r.CoverRepository.ForgetUnusedManagedCover(ctx, cover)
}

func TestCoverCloseRejectedAtDependencyBoundaries(t *testing.T) {
	for _, kind := range []string{"change", "remove", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			a, store, work, _ := importFixture(t)
			ctx := context.Background()
			if _, err := a.Select(ctx, work.ID); err != nil {
				t.Fatal(err)
			}
			if kind == "cleanup" {
				if _, _, err := store.RemoveWorkCover(ctx, work.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			var got []string
			check := func(point string) {
				t.Helper()
				got = append(got, point)
				// Repeated attempts must neither close admission nor end the ticket.
				for range 2 {
					done, err := a.operations.RequestClose()
					if !errors.Is(err, ErrCoverBusy) || done != nil {
						t.Errorf("%s: close accepted during operation: %v", point, err)
					}
				}
			}
			a.repository = closeCheckRepository{store, check}
			choose, open, prepare, write, deleteFile := a.choose, a.openSource, a.prepare, a.write, a.delete
			a.choose = func(ctx context.Context) (string, error) { check("choose"); return choose(ctx) }
			a.openSource = func(ctx context.Context, path string) (io.ReadCloser, error) {
				check("source-open")
				source, err := open(ctx, path)
				if err != nil {
					return nil, err
				}
				return closeCheckSource{source, check}, nil
			}
			a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
				check("prepare")
				guard, err := prepare(ctx, dir)
				if err != nil {
					return nil, err
				}
				return importCloser{func() error { check("guard-close"); return guard.Close() }}, nil
			}
			a.write = func(ctx context.Context, dir, id string, data []byte) (platform.CoverFileInfo, error) {
				check("write")
				return write(ctx, dir, id, data)
			}
			a.delete = func(ctx context.Context, dir, id string, info platform.CoverFileInfo) error {
				check("delete")
				return deleteFile(ctx, dir, id, info)
			}
			var err error
			var result CoverImportResult
			want := []string{"work", "choose", "source-open", "source-read", "source-close", "prepare", "reserve", "write", "confirm", "set", "unused", "delete", "forget", "guard-close"}
			switch kind {
			case "change":
				result, err = a.Select(ctx, work.ID)
			case "remove":
				result, err = a.Remove(ctx, work.ID)
				want = []string{"work", "prepare", "remove", "unused", "delete", "forget", "guard-close"}
			case "cleanup":
				var ticket *CoverOperation
				ticket, err = a.operations.BeginCleanup()
				if err != nil {
					t.Fatal(err)
				}
				var warnings []string
				warnings, err = a.Cleanup(ctx, ticket)
				if len(warnings) != 0 {
					t.Fatal(warnings)
				}
				want = []string{"inventory", "unused", "prepare", "unused", "delete", "forget", "guard-close"}
			}
			// PNG decoding can call Read more than once; collapse consecutive reads.
			var compact []string
			for _, point := range got {
				if len(compact) == 0 || point != "source-read" || compact[len(compact)-1] != point {
					compact = append(compact, point)
				}
			}
			if err != nil || !reflect.DeepEqual(compact, want) {
				t.Fatal(compact, want, err)
			}
			if kind != "cleanup" && (result.Work == nil || len(result.Warnings) != 0) {
				t.Fatal(result)
			}
			done, err := a.operations.RequestClose()
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			default:
				t.Fatal("completed operation still blocks close")
			}
			if _, err := a.operations.BeginChange(work.ID, true); !errors.Is(err, ErrCoverClosing) {
				t.Fatal("close did not stop new admission", err)
			}
		})
	}
}

type closeCheckSource struct {
	io.ReadCloser
	check func(string)
}

func TestCoverCloseRejectedWhileWaitingForReferences(t *testing.T) {
	for _, kind := range []string{"change", "remove", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			a, _, work, _ := importFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			unlock, err := a.operations.LockReferences(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			waiting := make(chan struct{}, 1)
			// Observation only; no blocking or operation reentry in this callback.
			notify := func(stage CoverProgressStage) {
				if stage == CoverProgressWaiting {
					waiting <- struct{}{}
				}
			}
			done := make(chan error, 1)
			var cleanup *CoverOperation
			if kind == "cleanup" {
				cleanup, err = a.operations.BeginCleanup()
				if err != nil {
					t.Fatal(err)
				}
			}
			go func() {
				var err error
				switch kind {
				case "change":
					_, err = a.SelectWithProgress(ctx, work.ID, nil, notify)
				case "remove":
					_, err = a.RemoveWithProgress(ctx, work.ID, notify)
				case "cleanup":
					_, err = a.CleanupWithProgress(ctx, cleanup, notify)
				}
				done <- err
			}()
			select {
			case <-waiting:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			for range 2 {
				if drained, err := a.operations.RequestClose(); !errors.Is(err, ErrCoverBusy) || drained != nil {
					t.Error("waiting operation allowed close", err)
				}
			}
			select {
			case err := <-done:
				t.Fatal("operation bypassed held reference gate", err)
			default:
			}
			unlock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			drained, err := a.operations.RequestClose()
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-drained:
			default:
				t.Fatal("finished wait still blocks close")
			}
		})
	}
}

func (s closeCheckSource) Read(data []byte) (int, error) {
	s.check("source-read")
	return s.ReadCloser.Read(data)
}
func (s closeCheckSource) Close() error { s.check("source-close"); return s.ReadCloser.Close() }
