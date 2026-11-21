package platform

import (
	"context"
	"errors"
	"os"
	"testing"
)

func restoreDiscardOps() backupDiscardOps {
	return backupDiscardOps{identifyBackupTemp, backupHandleName, markBackupTempDeleted, confirmBackupTempAbsent}
}
func restoreDiscardFile(t *testing.T) (*BackupWorkDirectory, *RestorePreparationFile) {
	t.Helper()
	work := outputWork(t)
	f, e := CreateRestorePreparationFile(context.Background(), work)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, e = f.Write([]byte("synthetic")); e != nil {
		t.Fatal(e)
	}
	return work, f
}
func TestRestorePreparationDiscardNative(t *testing.T) {
	work, f := restoreDiscardFile(t)
	r, e := f.Discard()
	if e != nil || !r.MarkedForDeletion || !r.Absent || !f.ReleaseResult().Confirmed() {
		t.Fatal(r, e)
	}
	assertRestoreIOClosed(t, f)
	if e = work.VerifyIdentity(); e != nil || work.ReleaseResult().CloseAttempted {
		t.Fatal("parent released", e)
	}
	// A later object at the same path is never part of this owner's cleanup.
	if e = os.WriteFile(f.Path(), []byte("keep later file"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Discard(); !errors.Is(e, ErrRestorePreparationDiscardAttempted) {
		t.Fatal("retry accepted", e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(f.Path())
	if e != nil || string(got) != "keep later file" {
		t.Fatal("later object changed", e)
	}
}
func TestRestorePreparationDiscardRefusals(t *testing.T) {
	for _, mode := range []string{"parent-id", "parent-name", "child-id", "child-name", "identity-error", "name-error", "closed-child", "closed-parent"} {
		t.Run(mode, func(t *testing.T) {
			work, f := restoreDiscardFile(t)
			ops := restoreDiscardOps()
			fault := errors.New("verification failed")
			marks := 0
			ops.mark = func(*os.File) error { marks++; return nil }
			switch mode {
			case "parent-id":
				work.identity.FileID[0] ^= 1
			case "parent-name":
				work.path += "-wrong"
			case "child-id":
				f.identity.FileID[0] ^= 1
			case "child-name":
				ops.name = func(*os.File) (string, error) { return f.Path() + "-wrong", nil }
			case "identity-error":
				ops.identity = func(*os.File) (BackupTempIdentity, error) { return BackupTempIdentity{}, fault }
			case "name-error":
				ops.name = func(*os.File) (string, error) { return "", fault }
			case "closed-child":
				if e := f.Close(); e != nil {
					t.Fatal(e)
				}
			case "closed-parent":
				if e := work.Close(); e != nil {
					t.Fatal(e)
				}
			}
			r, e := f.discard(ops)
			if e == nil || marks != 0 || r.MarkedForDeletion || r.Absent {
				t.Fatal("unconfirmed child removed", r, e)
			}
			if e = f.Close(); e != nil {
				t.Fatal(e)
			}
			got, e := os.ReadFile(f.Path())
			if e != nil || string(got) != "synthetic" {
				t.Fatal("refused artifact changed", e)
			}
		})
	}
}
func TestRestorePreparationDiscardFaults(t *testing.T) {
	for _, mode := range []string{"mark", "close", "absence"} {
		t.Run(mode, func(t *testing.T) {
			work, f := restoreDiscardFile(t)
			ops := restoreDiscardOps()
			fault := errors.New("cleanup fault")
			marks, checks := 0, 0
			ops.mark = func(file *os.File) error {
				marks++
				if mode == "mark" {
					return fault
				}
				return markBackupTempDeleted(file)
			}
			ops.absent = func(name string) error {
				checks++
				if mode == "absence" {
					return fault
				}
				return confirmBackupTempAbsent(name)
			}
			if mode == "close" {
				f.closeFile = func() error { return errors.Join(f.file.Close(), fault) }
			}
			r, e := f.discard(ops)
			if !errors.Is(e, fault) || r.Absent || marks != 1 {
				t.Fatal(r, e)
			}
			if r.MarkedForDeletion != (mode != "mark") || checks != map[string]int{"mark": 0, "close": 0, "absence": 1}[mode] {
				t.Fatal("wrong phase", r, checks)
			}
			assertRestoreIOClosed(t, f)
			if _, e = f.discard(ops); !errors.Is(e, ErrRestorePreparationDiscardAttempted) || marks != 1 {
				t.Fatal("native retry", e, marks)
			}
			if e = work.VerifyIdentity(); e != nil {
				t.Fatal("parent released", e)
			}
			if mode == "mark" {
				if e = f.Close(); e != nil {
					t.Fatal(e)
				}
				got, e := os.ReadFile(f.Path())
				if e != nil || string(got) != "synthetic" {
					t.Fatal("mark failure removed data", e)
				}
			}
			if mode == "close" && f.ReleaseResult().Confirmed() {
				t.Fatal("unconfirmed release became success")
			}
		})
	}
}
