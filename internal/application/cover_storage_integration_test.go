package application_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// The test is the caller: production image operations are not wired yet.
func TestCoverStorageErrorNotification(t *testing.T) {
	for _, mode := range []string{"success", "busy", "input-rejection"} {
		for _, remove := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/remove=%t", mode, remove), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				path := filepath.Join(t.TempDir(), "fixture.sqlite")
				dsn := path + "?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(0)"
				store, err := storage.Open(ctx, dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				at := time.Date(2026, 10, 18, 1, 2, 3, 0, time.UTC)
				work, err := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/synthetic", Title: "Synthetic", ThumbnailPath: "legacy/cover.png"}, at)
				if err != nil {
					t.Fatal(err)
				}
				work, err = store.CreateWork(ctx, work)
				if err != nil {
					t.Fatal(err)
				}
				const id = "0123456789abcdef0123456789abcdef"
				if err := store.ReserveManagedCover(ctx, id); err != nil {
					t.Fatal(err)
				}
				// Synthetic metadata; no image ownership or file safety is asserted.
				if err := store.ConfirmManagedCover(ctx, id, storage.ManagedCoverInfo{ByteSize: 1}); err != nil {
					t.Fatal(err)
				}
				var reader *sql.Tx
				if mode == "busy" {
					observer, err := sql.Open("sqlite", path)
					if err != nil {
						t.Fatal(err)
					}
					defer observer.Close()
					reader, err = observer.BeginTx(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Rollback()
					var title string
					if err := reader.QueryRowContext(ctx, "SELECT title FROM works WHERE id=?", work.ID).Scan(&title); err != nil {
						t.Fatal(err)
					}
				}
				var gate application.CoverOperations
				op, err := gate.BeginChange(work.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				read, err := gate.BeginRead()
				if err != nil {
					t.Fatal(err)
				}
				stage, err := op.BeginStage()
				if err != nil {
					t.Fatal(err)
				}
				when, coverID := at.Add(time.Hour), id
				if mode == "input-rejection" {
					when, coverID = time.Time{}, "../invalid"
				}
				var got manga.Work
				var previous string
				if remove {
					got, previous, err = store.RemoveWorkCover(ctx, work.ID, when)
				} else {
					got, previous, err = store.SetWorkCover(ctx, work.ID, coverID, when)
				}
				// Notify the actual Store result BEFORE advancing/ending tickets.
				gate.ObserveError(err)
				gate.ObserveError(nil)
				want := work
				if mode == "success" {
					want.ThumbnailPath, want.UpdatedAt = "covers/"+id+".png", when
					if remove {
						want.ThumbnailPath = ""
					}
					if err != nil || previous != work.ThumbnailPath || !reflect.DeepEqual(got, want) {
						t.Fatalf("success result: %#v %q %v", got, previous, err)
					}
				} else {
					if err == nil || previous != "" || !reflect.DeepEqual(got, manga.Work{}) {
						t.Fatalf("error returned success data: %#v %q %v", got, previous, err)
					}
					if mode == "busy" {
						var cause *sqlite.Error
						if !errors.Is(err, storage.ErrWorkCoverOutcomeUnknown) || !errors.As(err, &cause) || cause.Code() != sqlite3.SQLITE_BUSY {
							t.Fatalf("wrong commit failure: %v", err)
						}
						for _, begin := range []func() (*application.CoverOperation, error){gate.BeginRead, gate.BeginCleanup, func() (*application.CoverOperation, error) { return gate.BeginChange(work.ID+1, false) }} {
							if ticket, err := begin(); ticket != nil || !errors.Is(err, application.ErrCoverStopped) {
								t.Fatal("new image operation admitted")
							}
						}
					} else if errors.Is(err, storage.ErrWorkCoverOutcomeUnknown) || (!errors.Is(err, storage.ErrInvalidManagedCover) && !errors.Is(err, manga.ErrTimestampInvalid)) {
						t.Fatalf("wrong input rejection: %v", err)
					}
				}
				if err := op.End(); !errors.Is(err, application.ErrCoverBusy) {
					t.Fatal("notification lost active stage")
				}
				if done, err := gate.RequestClose(); done != nil || !errors.Is(err, application.ErrCoverBusy) {
					t.Fatal("notification ended active operation")
				}
				stage.End() // An existing stage can finish even after Stop.
				next, err := op.BeginStage()
				if mode == "busy" {
					if next != nil || !errors.Is(err, application.ErrCoverStopped) {
						t.Fatal("next stage admitted after unknown outcome")
					}
				} else {
					if err != nil {
						t.Fatalf("control case incorrectly stopped: %v", err)
					}
					next.End()
				}
				if err := op.End(); err != nil {
					t.Fatal(err)
				}
				done, err := gate.RequestClose()
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-done:
					t.Fatal("read ticket was completed early")
				default:
				}
				if err := read.End(); err != nil {
					t.Fatal(err)
				}
				select {
				case <-done:
				default:
					t.Fatal("read did not drain")
				}
				if reader != nil {
					if err := reader.Rollback(); err != nil {
						t.Fatal(err)
					}
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := storage.Open(ctx, dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				after, err := reopened.WorkByID(ctx, work.ID)
				if err != nil || !reflect.DeepEqual(after, want) {
					t.Fatalf("persisted state: %#v %v", after, err)
				}
			})
		}
	}
}
