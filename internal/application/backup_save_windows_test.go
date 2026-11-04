package application

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

type backupSaveFake struct {
	*backupPrepareMemory
	publication                                   platform.BackupPublication
	cleanup                                       platform.BackupDiscardResult
	publishErr, cleanupErr, closeErr, postSyncErr error
	steps                                         []string
	cancelPublish                                 context.CancelFunc
	posted                                        bool
}

func (f *backupSaveFake) Path() string { return `C:\save\temp.partial` }
func (f *backupSaveFake) PublishNoReplace(context.Context) (platform.BackupPublication, error) {
	f.steps = append(f.steps, "publish")
	f.posted = true
	if f.cancelPublish != nil {
		f.cancelPublish()
	}
	return f.publication, f.publishErr
}
func (f *backupSaveFake) Sync() error {
	if !f.posted {
		return f.backupPrepareMemory.Sync()
	}
	f.steps = append(f.steps, "sync")
	return f.postSyncErr
}
func (f *backupSaveFake) Discard() (platform.BackupDiscardResult, error) {
	f.steps = append(f.steps, "discard")
	// Marked means the platform already attempted Close/check/parent release.
	if f.cleanup.MarkedForDeletion {
		f.steps = append(f.steps, "discard-release")
	}
	return f.cleanup, f.cleanupErr
}
func (f *backupSaveFake) Close() error {
	f.steps = append(f.steps, "close")
	return f.closeErr
}

func TestSaveBackupArchiveFailureAndOwnership(t *testing.T) {
	cause := errors.New("synthetic operation failure")
	cleanupCause := errors.New("synthetic cleanup failure")
	closeCause := errors.New("synthetic release failure")
	manifest, data := backupPrepareFixture(t)
	for _, mode := range []string{"source", "source-cleanup", "cancel", "cancel-preparing", "not-attempted", "collision", "unknown", "future", "refused", "delete-failed", "check-failed", "confirmed", "confirmed-warning"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &backupSaveFake{backupPrepareMemory: &backupPrepareMemory{}, publication: platform.BackupPublication{State: platform.BackupPublishCollision, TemporaryPath: `C:\save\temp.partial`, FinalPath: `C:\save\backup.zip`}, cleanup: platform.BackupDiscardResult{TemporaryPath: `C:\save\temp.partial`, MarkedForDeletion: true, Absent: true}}
			want := []string{"publish", "discard", "discard-release"}
			var causes []error
			switch mode {
			case "source", "source-cleanup":
				want = []string{"discard", "discard-release"}
				causes = []error{ErrBackupPrepare, cause}
				if mode == "source-cleanup" {
					f.cleanup.MarkedForDeletion, f.cleanup.Absent = false, false
					f.cleanupErr, f.closeErr = cleanupCause, closeCause
					want = []string{"discard", "close"}
					causes = append(causes, cleanupCause, closeCause)
				}
			case "cancel", "cancel-preparing":
				if mode == "cancel" {
					cancel()
				} else {
					f.stage, f.cancel = "cancel-sync", cancel
				}
				want = []string{"discard", "discard-release"}
				causes = []error{ErrBackupPrepare, context.Canceled}
			case "not-attempted":
				f.publication.State, f.publishErr = platform.BackupPublishNotAttempted, context.Canceled
				f.cancelPublish = cancel
				causes = []error{ErrBackupCompletion, context.Canceled}
			case "unknown", "future":
				f.publication.State = platform.BackupPublishUnknown
				if mode == "future" {
					f.publication.State = platform.BackupPublicationState(255)
				}
				f.publishErr, f.closeErr = cause, closeCause
				want = []string{"publish", "close"}
				causes = []error{ErrBackupCompletion, cause, closeCause}
			case "refused", "delete-failed":
				f.cleanup.MarkedForDeletion, f.cleanup.Absent = false, false
				f.cleanupErr, f.closeErr = cleanupCause, closeCause
				if mode == "refused" {
					f.cleanupErr = platform.ErrBackupDiscardUnsafe
				}
				want = []string{"publish", "discard", "close"}
				causes = []error{ErrBackupCompletion, f.cleanupErr, closeCause}
			case "check-failed":
				f.cleanup.Absent, f.cleanupErr = false, cleanupCause
				causes = []error{ErrBackupCompletion, cleanupCause}
			case "confirmed", "confirmed-warning":
				f.publication.State, f.cancelPublish = platform.BackupPublishConfirmed, cancel
				want = []string{"publish", "sync", "close"}
				if mode == "confirmed-warning" {
					f.postSyncErr, f.closeErr = cause, closeCause
					causes = []error{ErrBackupPostprocess, cause, closeCause}
				}
			default:
				causes = []error{ErrBackupCompletion} // Even nil publication error is not success.
			}
			r, err := SaveBackupArchive(ctx, f, manifest, func(string) (io.ReadCloser, error) {
				if mode == "source" || mode == "source-cleanup" {
					return nil, cause
				}
				return io.NopCloser(bytes.NewReader(data)), nil
			})
			if !reflect.DeepEqual(f.steps, want) {
				t.Fatalf("order %v want %v", f.steps, want)
			}
			if len(causes) == 0 && err != nil {
				t.Fatal(err)
			}
			for _, c := range causes {
				if !errors.Is(err, c) {
					t.Fatalf("lost %v: %v", c, err)
				}
			}
			if r.Publication.TemporaryPath != f.Path() {
				t.Fatalf("lost location: %#v", r)
			}
			if mode != "source" && mode != "source-cleanup" && mode != "cancel" && mode != "cancel-preparing" && r.Publication != f.publication {
				t.Fatalf("changed observation: %#v", r)
			}
			calledDiscard := false
			for _, s := range want {
				calledDiscard = calledDiscard || s == "discard"
			}
			if calledDiscard != (r.Cleanup != nil) || (r.Cleanup != nil && *r.Cleanup != f.cleanup) {
				t.Fatalf("cleanup observation: %#v", r)
			}
		})
	}
}

func TestSaveBackupArchiveOnWindowsFiles(t *testing.T) {
	for _, mode := range []string{"success", "collision", "source", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			final := filepath.Join(dir, "保存.zip")
			file, err := platform.CreateBackupTemp(context.Background(), final)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := file.Close(); err != nil {
					t.Error(err)
				}
			})
			if mode == "collision" {
				if err := os.WriteFile(final, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			manifest, data := backupPrepareFixture(t)
			cause := errors.New("synthetic source error")
			r, err := SaveBackupArchive(ctx, file, manifest, func(string) (io.ReadCloser, error) {
				if mode == "source" {
					return nil, cause
				}
				return io.NopCloser(bytes.NewReader(data)), nil
			})
			if mode == "success" {
				if err != nil || r.Publication.State != platform.BackupPublishConfirmed || r.Cleanup != nil {
					t.Fatalf("save %#v %v", r, err)
				}
				f, e := os.Open(final)
				if e != nil {
					t.Fatal(e)
				}
				info, e := f.Stat()
				if e != nil {
					f.Close()
					t.Fatal(e)
				}
				_, e = backup.ValidateArchive(f, info.Size())
				if closeErr := f.Close(); e != nil || closeErr != nil {
					t.Fatalf("final validation %v %v", e, closeErr)
				}
			} else {
				if err == nil || r.Cleanup == nil || !r.Cleanup.MarkedForDeletion || !r.Cleanup.Absent {
					t.Fatalf("cleanup %#v %v", r, err)
				}
				if mode == "source" && !errors.Is(err, cause) {
					t.Fatal(err)
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if mode == "collision" {
					b, e := os.ReadFile(final)
					if e != nil || string(b) != "keep" {
						t.Fatalf("existing %q %v", b, e)
					}
				} else if _, e := os.Stat(final); !errors.Is(e, os.ErrNotExist) {
					t.Fatalf("unexpected final: %v", e)
				}
			}
			if _, e := os.Stat(file.Path()); !errors.Is(e, os.ErrNotExist) {
				t.Fatalf("temp remains: %v", e)
			}
			if e := file.VerifyIdentity(); !errors.Is(e, os.ErrClosed) {
				t.Fatalf("not released: %v", e)
			}
			if e := os.Rename(dir, dir+"-moved"); e != nil {
				t.Fatalf("parent retained: %v", e)
			}
		})
	}
}
