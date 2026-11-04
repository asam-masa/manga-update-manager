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

type backupCompletionFake struct {
	result                        platform.BackupPublication
	publishErr, syncErr, closeErr error
	cancel                        context.CancelFunc
	steps                         []string
}

func (f *backupCompletionFake) PublishNoReplace(context.Context) (platform.BackupPublication, error) {
	f.steps = append(f.steps, "publish")
	if f.cancel != nil {
		f.cancel()
	}
	return f.result, f.publishErr
}
func (f *backupCompletionFake) Sync() error  { f.steps = append(f.steps, "sync"); return f.syncErr }
func (f *backupCompletionFake) Close() error { f.steps = append(f.steps, "close"); return f.closeErr }

func TestCompleteBackupArchivePreservesConfirmedAndAllWarnings(t *testing.T) {
	publishCause := errors.New("publication observation warning")
	syncCause := errors.New("synthetic Sync failure")
	closeCause := errors.New("synthetic Close failure")
	for _, tc := range []struct {
		name                 string
		publish, sync, close error
		lateCancel           bool
	}{
		{"success", nil, nil, nil, false},
		{"sync", nil, syncCause, nil, false},
		{"close", nil, nil, closeCause, false},
		{"combined", publishCause, syncCause, closeCause, false},
		{"late-cancel", nil, nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := platform.BackupPublication{State: platform.BackupPublishConfirmed, TemporaryPath: `C:\save\temp.partial`, FinalPath: `C:\save\backup.zip`}
			f := &backupCompletionFake{result: want, publishErr: tc.publish, syncErr: tc.sync, closeErr: tc.close}
			if tc.lateCancel {
				f.cancel = cancel
			}
			got, err := CompleteBackupArchive(ctx, f)
			if got != want || !reflect.DeepEqual(f.steps, []string{"publish", "sync", "close"}) {
				t.Fatalf("lost observation/order: %#v %v", got, f.steps)
			}
			if tc.publish == nil && tc.sync == nil && tc.close == nil {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if !errors.Is(err, ErrBackupPostprocess) || errors.Is(err, ErrBackupCompletion) {
					t.Fatalf("wrong category: %v", err)
				}
				for _, cause := range []error{tc.publish, tc.sync, tc.close} {
					if cause != nil && !errors.Is(err, cause) {
						t.Fatalf("lost cause %v: %v", cause, err)
					}
				}
			}
		})
	}
}

func TestCompleteBackupArchiveNonconfirmedRetainsOwnership(t *testing.T) {
	for _, state := range []platform.BackupPublicationState{platform.BackupPublishNotAttempted, platform.BackupPublishCollision, platform.BackupPublishUnknown} {
		for _, cause := range []error{context.Canceled, platform.ErrBackupPublishUnknown, nil} {
			want := platform.BackupPublication{State: state, TemporaryPath: `C:\save\temp.partial`, FinalPath: `C:\save\backup.zip`}
			f := &backupCompletionFake{result: want, publishErr: cause}
			got, err := CompleteBackupArchive(context.Background(), f)
			if got != want || !errors.Is(err, ErrBackupCompletion) || errors.Is(err, ErrBackupPostprocess) || !reflect.DeepEqual(f.steps, []string{"publish"}) {
				t.Fatalf("changed state/ownership: %#v %v %v", got, f.steps, err)
			}
			if cause != nil && !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
		}
	}
}

func TestCompleteBackupArchiveOnWindowsFile(t *testing.T) {
	for _, collision := range []bool{false, true} {
		final := filepath.Join(t.TempDir(), "保存.zip")
		temp, err := platform.CreateBackupTemp(context.Background(), final)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := temp.Close(); err != nil {
				t.Error(err)
			}
		})
		manifest, data := backupPrepareFixture(t)
		_, err = PrepareBackupArchive(context.Background(), temp, manifest, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
		if err != nil {
			t.Fatal(err)
		}
		if collision {
			if err := os.WriteFile(final, []byte("keep existing"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		result, err := CompleteBackupArchive(context.Background(), temp)
		if collision {
			if result.State != platform.BackupPublishCollision || !errors.Is(err, ErrBackupCompletion) {
				t.Fatalf("collision %#v %v", result, err)
			}
			if err := temp.VerifyIdentity(); err != nil {
				t.Fatalf("ownership not retained: %v", err)
			}
			data, err := os.ReadFile(final)
			if err != nil || string(data) != "keep existing" {
				t.Fatalf("existing changed %q %v", data, err)
			}
			continue
		}
		if err != nil || result.State != platform.BackupPublishConfirmed {
			t.Fatalf("complete %#v %v", result, err)
		}
		if err := temp.VerifyIdentity(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("not closed: %v", err)
		}
		f, err := os.Open(final)
		if err != nil {
			t.Fatal(err)
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			t.Fatal(err)
		}
		m, verifyErr := backup.ValidateArchive(f, info.Size())
		closeErr := f.Close()
		if verifyErr != nil || closeErr != nil || m.Database.Size != int64(len(data)) {
			t.Fatalf("final content %#v %v %v", m, verifyErr, closeErr)
		}
	}
}
