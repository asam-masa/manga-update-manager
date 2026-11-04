package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func publicationTemp(t *testing.T) (*BackupTemp, string) {
	t.Helper()
	final := filepath.Join(t.TempDir(), "保存.zip")
	temp, err := CreateBackupTemp(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := temp.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := temp.Write([]byte("synthetic publication bytes")); err != nil {
		t.Fatal(err)
	}
	if err := temp.Sync(); err != nil {
		t.Fatal(err)
	}
	return temp, final
}

func TestBackupPublishNoReplaceConfirmsSameFile(t *testing.T) {
	temp, final := publicationTemp(t)
	r, err := temp.PublishNoReplace(context.Background())
	if err != nil || r.State != BackupPublishConfirmed {
		t.Fatalf("publication %#v %v", r, err)
	}
	if err := temp.VerifyIdentity(); err != nil {
		t.Fatal(err)
	}
	if _, err := temp.Write([]byte("must not change")); !errors.Is(err, ErrBackupPublishAttempted) {
		t.Fatalf("write after publication: %v", err)
	}
	if again, err := temp.PublishNoReplace(context.Background()); again != r || !errors.Is(err, ErrBackupPublishAttempted) {
		t.Fatalf("repeated publication: %#v %v", again, err)
	}
	if err := os.Rename(filepath.Dir(final), filepath.Dir(final)+"-moved"); err == nil {
		t.Fatal("parent released")
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(final)
	if err != nil {
		t.Fatal(err)
	}
	id, idErr := identifyBackupTemp(f)
	closeErr := f.Close()
	if idErr != nil || closeErr != nil || id != temp.Identity() {
		t.Fatalf("final identity: %#v %v %v", id, idErr, closeErr)
	}
	if _, err := os.Stat(temp.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary name remains: %v", err)
	}
}

func TestBackupPublishCollisionPreservesBothFiles(t *testing.T) {
	temp, final := publicationTemp(t)
	// Introduce the destination immediately before the rename; no existence
	// check in the publisher may authorize replacement.
	if err := os.WriteFile(final, []byte("keep existing"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := temp.PublishNoReplace(context.Background())
	if r.State != BackupPublishCollision || (!errors.Is(err, windows.ERROR_FILE_EXISTS) && !errors.Is(err, windows.ERROR_ALREADY_EXISTS)) {
		t.Fatalf("collision %#v %v", r, err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{final: "keep existing", temp.Path(): "synthetic publication bytes"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("changed %s: %q %v", path, data, err)
		}
	}
}

func TestBackupPublicationUnknownAndCancellation(t *testing.T) {
	for _, stage := range []string{"before-error", "after-error", "post-id", "post-name", "cancel-before", "cancel-after"} {
		t.Run(stage, func(t *testing.T) {
			temp, final := publicationTemp(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic unknown failure")
			calls := 0
			ops := backupPublicationOps{name: backupHandleName, identity: identifyBackupTemp, rename: func(f *os.File, target string) error {
				calls++
				if stage == "before-error" {
					return cause
				}
				if err := renameBackupNoReplace(f, target); err != nil {
					return err
				}
				if stage == "after-error" {
					return cause
				}
				if stage == "cancel-after" {
					cancel()
				}
				return nil
			}}
			ids, names := 0, 0
			if stage == "post-id" {
				ops.identity = func(f *os.File) (BackupTempIdentity, error) {
					ids++
					if ids == 2 {
						return BackupTempIdentity{}, cause
					}
					return identifyBackupTemp(f)
				}
			}
			if stage == "post-name" {
				ops.name = func(f *os.File) (string, error) {
					names++
					if names == 2 {
						return "", cause
					}
					return backupHandleName(f)
				}
			}
			if stage == "cancel-before" {
				cancel()
			}
			r, err := temp.publishNoReplace(ctx, ops)
			switch stage {
			case "cancel-before":
				if calls != 0 || r.State != BackupPublishNotAttempted || !errors.Is(err, context.Canceled) {
					t.Fatalf("early cancel %#v %v calls=%d", r, err, calls)
				}
			case "cancel-after":
				if calls != 1 || r.State != BackupPublishConfirmed || err != nil {
					t.Fatalf("late cancel %#v %v", r, err)
				}
			default:
				if calls != 1 || r.State != BackupPublishUnknown || !errors.Is(err, ErrBackupPublishUnknown) || !errors.Is(err, cause) {
					t.Fatalf("unknown %#v %v", r, err)
				}
			}
			if calls != 0 {
				if _, err := temp.publishNoReplace(context.Background(), ops); !errors.Is(err, ErrBackupPublishAttempted) || calls != 1 {
					t.Fatalf("retried rename: %d %v", calls, err)
				}
			}
			if err := temp.Close(); err != nil {
				t.Fatal(err)
			}
			retained := final
			if stage == "before-error" || stage == "cancel-before" {
				retained = temp.Path()
			}
			data, err := os.ReadFile(retained)
			if err != nil || string(data) != "synthetic publication bytes" {
				t.Fatalf("output lost %q %v", data, err)
			}
		})
	}
}

func TestBackupPublishRejectsPrecheckMismatchAndClosedFile(t *testing.T) {
	temp, _ := publicationTemp(t)
	temp.identity.FileID[0] ^= 0xff
	r, err := temp.PublishNoReplace(context.Background())
	if r.State != BackupPublishNotAttempted || !errors.Is(err, ErrBackupPublishIdentity) || temp.publishAttempted {
		t.Fatalf("precheck %#v %v", r, err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = temp.PublishNoReplace(context.Background())
	if r.State != BackupPublishNotAttempted || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed %#v %v", r, err)
	}
}
