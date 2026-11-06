package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackupWorkDiscardEmptyAndNonempty(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[nonempty], func(t *testing.T) {
			ops := workDirectoryOps(t)
			root := workDirectoryRoot(t)
			d, err := createBackupWorkDirectory(context.Background(), root, [16]byte{}, ops)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			child := filepath.Join(d.Path(), "unexpected.txt")
			if nonempty {
				if err := os.WriteFile(child, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r, err := d.DiscardEmpty()
			if r.Path != d.Path() {
				t.Fatal(r)
			}
			if nonempty {
				if !errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) || r.MarkedForDeletion || r.Absent {
					t.Fatal(r, err)
				}
				if err := d.VerifyIdentity(); err != nil {
					t.Fatal("failed deletion lost ownership", err)
				}
				if b, err := os.ReadFile(child); err != nil || string(b) != "keep" {
					t.Fatal(b, err)
				}
			} else {
				if err != nil || !r.MarkedForDeletion || !r.Absent {
					t.Fatal(r, err)
				}
				if _, err := os.Lstat(d.Path()); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
					t.Fatal(err)
				}
			}
			again, err := d.DiscardEmpty()
			if again != r || !errors.Is(err, ErrBackupWorkDirectoryDiscardAttempted) {
				t.Fatal(again, err)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(root, root+"-moved"); err != nil {
				t.Fatal("parents retained", err)
			}
		})
	}
}

func workDiscardModel(order *string) (*BackupWorkDirectory, backupWorkDiscardOps) {
	d := &BackupWorkDirectory{file: &os.File{}, path: `C:\synthetic\work`, closeFile: func() error { *order += "close;"; return nil }, parent: backupTestCloser(func() error { *order += "parents;"; return nil })}
	ops := backupWorkDiscardOps{
		identity: func(*os.File) (BackupWorkDirectoryIdentity, error) { *order += "id;"; return d.identity, nil },
		name:     func(*os.File) (string, error) { *order += "name;"; return d.path, nil },
		mark:     func(*os.File) error { *order += "mark;"; return nil },
		absent:   func(string) error { *order += "absent;"; return nil },
	}
	return d, ops
}

func TestBackupWorkDiscardRefusesUnverified(t *testing.T) {
	for _, kind := range []string{"id", "name", "inspection", "closed"} {
		t.Run(kind, func(t *testing.T) {
			order := ""
			d, ops := workDiscardModel(&order)
			switch kind {
			case "id":
				ops.identity = func(*os.File) (BackupWorkDirectoryIdentity, error) {
					return BackupWorkDirectoryIdentity{VolumeSerial: 1}, nil
				}
			case "name":
				ops.name = func(*os.File) (string, error) { return `C:\synthetic\different`, nil }
			case "inspection":
				ops.identity = func(*os.File) (BackupWorkDirectoryIdentity, error) {
					return BackupWorkDirectoryIdentity{}, errors.New("inspection")
				}
			case "closed":
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			marked := false
			ops.mark = func(*os.File) error { marked = true; return nil }
			r, err := d.discardEmpty(ops)
			if err == nil || marked || r.MarkedForDeletion || r.Absent || d.discardAttempted {
				t.Fatal(r, err, order)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBackupWorkDiscardFailuresAndOrder(t *testing.T) {
	for _, stage := range []string{"none", "mark", "close", "absent", "parents", "close+parents"} {
		t.Run(stage, func(t *testing.T) {
			order := ""
			d, ops := workDiscardModel(&order)
			failure, parentFailure := errors.New("injected"), errors.New("parents injected")
			switch stage {
			case "mark":
				ops.mark = func(*os.File) error { order += "mark;"; return failure }
			case "close", "close+parents":
				d.closeFile = func() error { order += "close;"; return failure }
			case "absent":
				ops.absent = func(string) error { order += "absent;"; return failure }
			}
			if stage == "parents" || stage == "close+parents" {
				d.parent = backupTestCloser(func() error { order += "parents;"; return parentFailure })
			}
			r, err := d.discardEmpty(ops)
			if stage == "none" {
				if err != nil || !r.Absent {
					t.Fatal(r, err)
				}
			} else {
				if !errors.Is(err, ErrBackupWorkDirectoryDiscard) {
					t.Fatal(err)
				}
				if stage != "parents" && !errors.Is(err, failure) {
					t.Fatal("lost cause", err)
				}
				if (stage == "parents" || stage == "close+parents") && !errors.Is(err, parentFailure) {
					t.Fatal("lost parent cause", err)
				}
			}
			if r.MarkedForDeletion != (stage != "mark") || r.Absent != (stage == "none" || stage == "parents") {
				t.Fatal(r, stage)
			}
			want := "id;name;mark;close;absent;parents;"
			if stage == "mark" {
				want = "id;name;mark;"
			}
			if stage == "close" || stage == "close+parents" {
				want = "id;name;mark;close;parents;"
			}
			if order != want {
				t.Fatal(order, want)
			}
			again, repeated := d.discardEmpty(ops)
			if again != r || !errors.Is(repeated, ErrBackupWorkDirectoryDiscardAttempted) || order != want {
				t.Fatal(again, repeated, order)
			}
			_ = d.Close()
			if stage == "mark" {
				want += "close;parents;"
			}
			if order != want {
				t.Fatal("repeated release", order, want)
			}
		})
	}
}

func TestBackupWorkDiscardPreservesReplacement(t *testing.T) {
	ops := workDirectoryOps(t)
	root := workDirectoryRoot(t)
	d, err := createBackupWorkDirectory(context.Background(), root, [16]byte{}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	closeOriginal := d.closeFile
	d.closeFile = func() error {
		if err := closeOriginal(); err != nil {
			return err
		}
		return os.Mkdir(d.Path(), 0700)
	}
	r, err := d.DiscardEmpty()
	if !errors.Is(err, ErrBackupWorkDirectoryDiscardUnsafe) || !r.MarkedForDeletion || r.Absent {
		t.Fatal(r, err)
	}
	_, _ = d.DiscardEmpty()
	_ = d.Close()
	if info, err := os.Stat(d.Path()); err != nil || !info.IsDir() {
		t.Fatal("replacement removed", err)
	}
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal("parent not released", err)
	}
}

func TestBackupWorkDiscardRequiresLeafAbsence(t *testing.T) {
	root := workDirectoryRoot(t)
	if err := confirmBackupWorkDirectoryAbsent(filepath.Join(root, "missing")); err != nil {
		t.Fatal(err)
	}
	if err := confirmBackupWorkDirectoryAbsent(root); !errors.Is(err, ErrBackupWorkDirectoryDiscardUnsafe) {
		t.Fatal(err)
	}
	if err := confirmBackupWorkDirectoryAbsent(filepath.Join(root, "missing", "work")); err == nil {
		t.Fatal("missing parent accepted as leaf absence")
	}
}
