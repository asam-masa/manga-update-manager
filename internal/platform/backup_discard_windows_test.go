package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackupDiscardUnpublishedOrCollision(t *testing.T) {
	for _, mode := range []string{"unpublished", "cancelled", "collision"} {
		t.Run(mode, func(t *testing.T) {
			temp, final := publicationTemp(t)
			if mode == "cancelled" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if r, err := temp.PublishNoReplace(ctx); r.State != BackupPublishNotAttempted || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel %#v %v", r, err)
				}
			}
			if mode == "collision" {
				if err := os.WriteFile(final, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if r, err := temp.PublishNoReplace(context.Background()); r.State != BackupPublishCollision || err == nil {
					t.Fatalf("collision %#v %v", r, err)
				}
			}
			r, err := temp.Discard()
			if err != nil || !r.MarkedForDeletion || !r.Absent || r.TemporaryPath != temp.Path() {
				t.Fatalf("discard %#v %v", r, err)
			}
			if _, err := os.Stat(temp.Path()); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("temp remains: %v", err)
			}
			if mode == "collision" {
				data, err := os.ReadFile(final)
				if err != nil || string(data) != "keep" {
					t.Fatalf("final changed %q %v", data, err)
				}
			}
			if err := os.Rename(filepath.Dir(final), filepath.Dir(final)+"-moved"); err != nil {
				t.Fatalf("parents retained: %v", err)
			}
			if again, err := temp.Discard(); again != r || !errors.Is(err, ErrBackupDiscardAttempted) {
				t.Fatalf("retry lost result %#v %v", again, err)
			}
		})
	}
}

func TestBackupDiscardRefusesPublishedAndUnknown(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		temp, final := publicationTemp(t)
		if unknown {
			cause := errors.New("synthetic unknown before rename")
			_, err := temp.publishNoReplace(context.Background(), backupPublicationOps{backupHandleName, identifyBackupTemp, func(*os.File, string) error { return cause }})
			if !errors.Is(err, ErrBackupPublishUnknown) {
				t.Fatal(err)
			}
		} else {
			if _, err := temp.PublishNoReplace(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		r, err := temp.Discard()
		if !errors.Is(err, ErrBackupDiscardUnsafe) || r.MarkedForDeletion {
			t.Fatalf("unsafe discard %#v %v", r, err)
		}
		if err := temp.VerifyIdentity(); err != nil {
			t.Fatalf("lost retained file: %v", err)
		}
		if err := temp.Close(); err != nil {
			t.Fatal(err)
		}
		retained := final
		if unknown {
			retained = temp.Path()
		}
		data, err := os.ReadFile(retained)
		if err != nil || string(data) != "synthetic publication bytes" {
			t.Fatalf("lost file %q %v", data, err)
		}
	}
}

func TestBackupDiscardFailuresAndReleaseOrder(t *testing.T) {
	for _, stage := range []string{"identity", "name", "mismatch", "renamed", "mark", "close", "absent", "recreated", "parents"} {
		t.Run(stage, func(t *testing.T) {
			temp, createErr := CreateBackupTemp(context.Background(), filepath.Join(t.TempDir(), "backup.zip"))
			if createErr != nil {
				t.Fatal(createErr)
			}
			// Injected release errors below are asserted explicitly.
			t.Cleanup(func() { _ = temp.Close() })
			cause := errors.New("synthetic " + stage)
			var steps []string
			ops := backupDiscardOps{
				identity: func(f *os.File) (BackupTempIdentity, error) {
					if stage == "identity" {
						return BackupTempIdentity{}, cause
					}
					id, err := identifyBackupTemp(f)
					if stage == "mismatch" {
						id.FileID[0] ^= 0xff
					}
					return id, err
				},
				name: func(f *os.File) (string, error) {
					if stage == "name" {
						return "", cause
					}
					if stage == "renamed" {
						return temp.sourceName + "-changed", nil
					}
					return backupHandleName(f)
				},
				mark: func(f *os.File) error {
					steps = append(steps, "mark")
					if stage == "mark" {
						return cause
					}
					return markBackupTempDeleted(f)
				},
				absent: func(name string) error {
					steps = append(steps, "absent")
					if stage == "absent" {
						return cause
					}
					if stage == "recreated" {
						if err := os.WriteFile(name, []byte("replacement"), 0600); err != nil {
							return err
						}
					}
					return confirmBackupTempAbsent(name)
				},
			}
			fileClose, parents := temp.closeFile, temp.directory
			temp.closeFile = func() error {
				steps = append(steps, "close")
				err := fileClose()
				if stage == "close" {
					return errors.Join(err, cause)
				}
				return err
			}
			temp.directory = backupTestCloser(func() error {
				steps = append(steps, "parents")
				err := parents.Close()
				if stage == "parents" {
					return errors.Join(err, cause)
				}
				return err
			})
			r, err := temp.discard(ops)
			if !errors.Is(err, ErrBackupDiscard) {
				t.Fatalf("failure lost %#v %v", r, err)
			}
			if stage == "mismatch" || stage == "renamed" || stage == "recreated" {
				if !errors.Is(err, ErrBackupDiscardUnsafe) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, cause) {
				t.Fatal(err)
			}
			var want []string
			switch stage {
			case "identity", "name", "mismatch", "renamed":
				if r.MarkedForDeletion || temp.closed {
					t.Fatal("closed or marked unverified file")
				}
			case "mark":
				want = []string{"mark"}
				if temp.closed {
					t.Fatal("closed after failed delete request")
				}
			case "close":
				want = []string{"mark", "close", "parents"}
				if r.Absent {
					t.Fatal("claimed absence after Close error")
				}
			default:
				want = []string{"mark", "close", "absent", "parents"}
			}
			if !reflect.DeepEqual(steps, want) {
				t.Fatalf("order %v want %v", steps, want)
			}
			if temp.discardAttempted {
				prior := append([]string(nil), steps...)
				if _, err := temp.discard(ops); !errors.Is(err, ErrBackupDiscardAttempted) || !reflect.DeepEqual(steps, prior) {
					t.Fatalf("retried cleanup %v %v", steps, err)
				}
				if stage == "mark" {
					if _, err := temp.Write([]byte("must not write")); !errors.Is(err, ErrBackupDiscardAttempted) {
						t.Fatal(err)
					}
					if _, err := temp.PublishNoReplace(context.Background()); !errors.Is(err, ErrBackupDiscardAttempted) {
						t.Fatal(err)
					}
				}
			}
			closeErr := temp.Close()
			if stage == "close" || stage == "absent" || stage == "parents" {
				if !errors.Is(closeErr, cause) {
					t.Fatalf("lost cached release/check error: %v", closeErr)
				}
			}
			if stage == "recreated" {
				data, err := os.ReadFile(temp.Path())
				if err != nil || string(data) != "replacement" || r.Absent {
					t.Fatalf("replacement changed: %q %#v %v", data, r, err)
				}
			}
		})
	}
}

func TestBackupDiscardClosedAndAbsenceBoundary(t *testing.T) {
	temp, _ := publicationTemp(t)
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	if r, err := temp.Discard(); !errors.Is(err, os.ErrClosed) || r.MarkedForDeletion || r.Absent {
		t.Fatalf("closed discard %#v %v", r, err)
	}
	if _, err := os.Stat(temp.Path()); err != nil {
		t.Fatalf("closed file removed: %v", err)
	}
	dir := t.TempDir()
	if err := confirmBackupTempAbsent(filepath.Join(dir, "missing.partial")); err != nil {
		t.Fatalf("missing leaf: %v", err)
	}
	if err := confirmBackupTempAbsent(filepath.Join(dir, "missing-parent", "missing.partial")); err == nil {
		t.Fatal("missing parent treated as verified leaf absence")
	}
}
