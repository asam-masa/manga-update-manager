package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackupDBFinalizeNative(t *testing.T) {
	for _, mode := range []string{"success", "extra-child", "outside"} {
		t.Run(mode, func(t *testing.T) {
			root := workDirectoryRoot(t)
			work, err := createBackupWorkDirectory(context.Background(), root, [16]byte{}, workDirectoryOps(t))
			if err != nil {
				t.Fatal(err)
			}
			defer work.Close()
			name := filepath.Join(work.Path(), "copy.sqlite")
			if mode == "outside" {
				name = filepath.Join(root, "outside.sqlite")
			}
			data, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			if _, err := data.WriteString("partial copy"); err != nil {
				t.Fatal(err)
			}
			id, err := identifyBackupTemp(data)
			if err != nil {
				t.Fatal(err)
			}
			owner, _, err := transitionBackupDB(data, id, name)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			extra := filepath.Join(work.Path(), "unrelated.txt")
			if mode == "extra-child" {
				if err := os.WriteFile(extra, []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := finalizeBackupDBWork(owner, work)
			if result.file.path != name || result.directory.Path != work.Path() {
				t.Fatal("candidate locations lost", result)
			}
			if mode == "success" {
				if err != nil || !result.file.absent || !result.directory.Absent {
					t.Fatal(result, err)
				}
				if _, err := os.Stat(work.Path()); !os.IsNotExist(err) {
					t.Fatal("directory remains", err)
				}
			} else {
				if !errors.Is(err, errBackupDBFinalize) {
					t.Fatal(result, err)
				}
				if mode == "outside" {
					if !result.file.fileClosed || !result.file.referenceClosed {
						t.Fatal("release observations lost", result.file)
					}
					if result.directoryAttempted {
						t.Fatal("directory deletion attempted")
					}
					if _, err := os.Stat(name); err != nil {
						t.Fatal("outside file lost", err)
					}
				} else if data, err := os.ReadFile(extra); err != nil || string(data) != "preserve" {
					t.Fatal("extra child lost", err)
				}
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal("parent lost", err)
			}
		})
	}
}

func TestBackupDBFinalizeFaultsAndOrder(t *testing.T) {
	stage, fileClose, dirClose := errors.New("stage"), errors.New("file close"), errors.New("directory close")
	for _, mode := range []string{"success", "verify", "file", "unconfirmed", "directory", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			var calls []string
			result, err := finalizeBackupDB(backupDBFinalizeOps{
				verify: func() error {
					calls = append(calls, "verify")
					if mode == "verify" || mode == "multiple" {
						return stage
					}
					return nil
				},
				discardFile: func() (backupDBDiscardResult, error) {
					calls = append(calls, "file")
					if mode == "file" {
						return backupDBDiscardResult{}, stage
					}
					return backupDBDiscardResult{absent: mode != "unconfirmed"}, nil
				},
				closeFile: func() error {
					calls = append(calls, "file-close")
					if mode == "multiple" {
						return fileClose
					}
					return nil
				},
				discardDirectory: func() (BackupWorkDirectoryDiscardResult, error) {
					calls = append(calls, "directory")
					if mode == "directory" {
						return BackupWorkDirectoryDiscardResult{}, stage
					}
					return BackupWorkDirectoryDiscardResult{Absent: true}, nil
				},
				closeDirectory: func() error {
					calls = append(calls, "directory-close")
					if mode == "multiple" {
						return dirClose
					}
					return nil
				},
			})
			want := []string{"verify"}
			if mode != "verify" && mode != "multiple" {
				want = append(want, "file")
			}
			if mode == "success" || mode == "directory" {
				want = append(want, "directory")
			}
			want = append(want, "file-close", "directory-close")
			if !reflect.DeepEqual(calls, want) {
				t.Fatal(calls, want)
			}
			if (err == nil) != (mode == "success") {
				t.Fatal(result, err)
			}
			if mode != "success" && !errors.Is(err, errBackupDBFinalize) {
				t.Fatal(err)
			}
			if mode != "success" && mode != "unconfirmed" && !errors.Is(err, stage) {
				t.Fatal("stage cause lost", err)
			}
			if mode == "multiple" && (!errors.Is(err, fileClose) || !errors.Is(err, dirClose)) {
				t.Fatal("release causes lost", err)
			}
			if result.directoryAttempted != (mode == "success" || mode == "directory") {
				t.Fatal(result)
			}
		})
	}
}
