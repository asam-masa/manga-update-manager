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
	"syscall"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
)

type failureRepository struct {
	CoverRepository
	confirmed, updated int
}

func (r *failureRepository) ConfirmManagedCover(ctx context.Context, id string, info storage.ManagedCoverInfo) error {
	r.confirmed++
	return r.CoverRepository.ConfirmManagedCover(ctx, id, info)
}
func (r *failureRepository) SetWorkCover(ctx context.Context, id int64, cover string, now time.Time) (manga.Work, string, error) {
	r.updated++
	return r.CoverRepository.SetWorkCover(ctx, id, cover, now)
}

func TestCoverFailurePreservesExistingDataAndUnverifiedOutput(t *testing.T) {
	for _, mode := range []string{"generation", "write", "disk-full", "sync"} {
		for _, releaseFailure := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "", true: "/release-failure"}[releaseFailure], func(t *testing.T) {
				a, store, work, dir := importFixture(t)
				ctx := context.Background()
				seed, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				_, seedErr := seed.ExecContext(ctx, "UPDATE works SET site_name=?,notes=? WHERE id=?", "Synthetic Site", "Preserve these notes", work.ID)
				if err := errors.Join(seedErr, seed.Close()); err != nil {
					t.Fatal(err)
				}
				if err := store.RecordLastAccess(ctx, work.ID, work.CreatedAt.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				first, err := a.Select(ctx, work.ID)
				if err != nil || first.Work == nil {
					t.Fatal(first, err)
				}
				before := *first.Work
				if before.SiteName == "" || before.Notes == "" || before.LastAccessedAt == nil {
					t.Fatal("preservation fixture lacks nonzero fields", before)
				}
				beforeRecords, err := store.ListManagedCovers(ctx)
				if err != nil || len(beforeRecords) != 1 {
					t.Fatal(beforeRecords, err)
				}
				oldName := filepath.Join(dir, filepath.FromSlash(before.ThumbnailPath))
				oldBytes, err := os.ReadFile(oldName)
				if err != nil {
					t.Fatal(err)
				}
				sourceBytes := oldBytes
				if mode == "generation" {
					sourceBytes = []byte("invalid source image")
				}
				sourceName := filepath.Join(dir, "source.png")
				if err := os.WriteFile(sourceName, sourceBytes, 0600); err != nil {
					t.Fatal(err)
				}
				a.choose = func(context.Context) (string, error) { return sourceName, nil }
				releaseErr := errors.New("synthetic release failure")
				a.openSource = func(ctx context.Context, path string) (io.ReadCloser, error) {
					source, err := platform.OpenCoverSource(ctx, path)
					if err != nil {
						return nil, err
					}
					if releaseFailure && mode == "generation" {
						return failureSource{source, releaseErr}, nil
					}
					return source, nil
				}
				prepare := a.prepare
				prepared, writes, deletes := 0, 0, 0
				a.prepare = func(ctx context.Context, dir string) (io.Closer, error) {
					prepared++
					guard, err := prepare(ctx, dir)
					if err != nil {
						return nil, err
					}
					return importCloser{func() error {
						err := guard.Close()
						if releaseFailure {
							err = errors.Join(err, releaseErr)
						}
						return err
					}}, nil
				}
				writeErr := errors.New("synthetic write failure")
				if mode == "disk-full" {
					writeErr = syscall.ENOSPC
				}
				if mode == "sync" {
					writeErr = errors.New("synthetic sync failure")
				}
				var outputName string
				var outputBytes []byte
				a.write = func(_ context.Context, dir, id string, data []byte) (platform.CoverFileInfo, error) {
					writes++
					outputName = filepath.Join(dir, "covers", id+".png")
					if mode != "write" {
						outputBytes = append([]byte(nil), data...)
						if mode == "disk-full" {
							outputBytes = outputBytes[:len(outputBytes)/2]
						}
						file, err := os.OpenFile(outputName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
						if err != nil {
							return platform.CoverFileInfo{}, err
						}
						_, writeResult := file.Write(outputBytes)
						if err := errors.Join(writeResult, file.Close()); err != nil {
							return platform.CoverFileInfo{}, err
						}
					}
					// Boundary injection: not a real WriteNewCover/OS Sync fault.
					return platform.CoverFileInfo{}, writeErr
				}
				a.delete = func(context.Context, string, string, platform.CoverFileInfo) error {
					deletes++
					return errors.New("unexpected deletion")
				}
				repository := &failureRepository{CoverRepository: store}
				a.repository = repository
				result, err := a.Select(ctx, work.ID)
				wantErr := writeErr
				if mode == "generation" {
					wantErr = thumbnail.ErrUnsupported
				}
				if !errors.Is(err, wantErr) || result.Work != nil || result.Cancelled || len(result.Warnings) != 0 {
					t.Fatal(result, err)
				}
				if errors.Is(err, platform.ErrCoverRelease) != releaseFailure || errors.Is(err, releaseErr) != releaseFailure {
					t.Fatal("release classification lost", err)
				}
				wantIO := 1
				if mode == "generation" {
					wantIO = 0
				}
				if prepared != wantIO || writes != wantIO || repository.confirmed != 0 || repository.updated != 0 || deletes != 0 {
					t.Fatal("failure continued to later boundary", prepared, writes, repository, deletes)
				}
				after, err := store.WorkByID(ctx, work.ID)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("work changed", after, err)
				}
				for name, data := range map[string][]byte{oldName: oldBytes, sourceName: sourceBytes} {
					got, err := os.ReadFile(name)
					if err != nil || !bytes.Equal(got, data) {
						t.Fatal("preserved file changed", name, err)
					}
				}
				records, err := store.ListManagedCovers(ctx)
				if err != nil || len(records) != 1+wantIO {
					t.Fatal(records, err)
				}
				reserved := 0
				for _, record := range records {
					if record.RelativePath == before.ThumbnailPath {
						if !reflect.DeepEqual(record, beforeRecords[0]) {
							t.Fatal("existing management record changed", record)
						}
					} else {
						if record.State != "reserved" || record.Info != nil {
							t.Fatal("unverified output confirmed", record)
						}
						reserved++
					}
				}
				if reserved != wantIO {
					t.Fatal(records)
				}
				ticket, admissionErr := a.operations.BeginChange(work.ID, true)
				if releaseFailure {
					if !errors.Is(admissionErr, ErrCoverStopped) {
						t.Fatal("release failure did not stop images", admissionErr)
					}
				} else {
					if admissionErr != nil {
						t.Fatal("ordinary failure stopped admission", admissionErr)
					}
					if err := ticket.End(); err != nil {
						t.Fatal(err)
					}
				}
				// Reopen with fresh admission, as the next startup would do.
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := storage.Open(ctx, filepath.Join(dir, "fixture.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				var operations CoverOperations
				cleaner := NewCoverImporter(reopened, &operations, dir, nil, time.Now)
				cleanup, err := operations.BeginCleanup()
				if err != nil {
					t.Fatal(err)
				}
				warnings, err := cleaner.Cleanup(ctx, cleanup)
				wantWarnings := []string{}
				if wantIO == 1 {
					wantWarnings = []string{"cover_cleanup_unverified"}
				}
				if err != nil || !reflect.DeepEqual(warnings, wantWarnings) {
					t.Fatal(warnings, err)
				}
				afterRecords, err := reopened.ListManagedCovers(ctx)
				if err != nil || !reflect.DeepEqual(records, afterRecords) {
					t.Fatal("cleanup changed unverified records", afterRecords, err)
				}
				after, err = reopened.WorkByID(ctx, work.ID)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("cleanup changed work", after, err)
				}
				for name, data := range map[string][]byte{oldName: oldBytes, sourceName: sourceBytes} {
					got, err := os.ReadFile(name)
					if err != nil || !bytes.Equal(got, data) {
						t.Fatal("cleanup changed preserved file", name, err)
					}
				}
				if outputName != "" {
					got, err := os.ReadFile(outputName)
					if mode == "write" {
						if !errors.Is(err, os.ErrNotExist) {
							t.Fatal("output unexpectedly exists", err)
						}
					} else if err != nil || !bytes.Equal(got, outputBytes) {
						t.Fatal("cleanup removed unverified output", err)
					}
				}
			})
		}
	}
}

type failureSource struct {
	io.ReadCloser
	closeErr error
}

func (s failureSource) Close() error { return errors.Join(s.ReadCloser.Close(), s.closeErr) }
