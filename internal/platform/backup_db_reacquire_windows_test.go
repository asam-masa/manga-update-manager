package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackupDBReacquireNative(t *testing.T) {
	for _, mode := range []string{"unchanged", "replacement", "missing", "mutation", "writer", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			name := filepath.Join(workDirectoryRoot(t), "copy.sqlite")
			file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.WriteString("abc"); err != nil {
				t.Fatal(err)
			}
			id, err := identifyBackupTemp(file)
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := captureBackupDBBaseline(file, id, name)
			if err != nil {
				t.Fatal(err)
			}
			p, err := windows.UTF16PtrFromString(name)
			if err != nil {
				t.Fatal(err)
			}
			h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
			if err != nil {
				t.Fatal(err)
			}
			reference := os.NewFile(uintptr(h), name)
			defer reference.Close()
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "replacement", "missing":
				if err := os.Rename(name, name+"-original"); err != nil {
					t.Fatal(err)
				}
				if mode == "replacement" {
					if err := os.WriteFile(name, []byte("abc"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "mutation":
				if err := os.WriteFile(name, []byte("xyz"), 0600); err != nil {
					t.Fatal(err)
				}
			case "writer":
				writer, err := os.OpenFile(name, os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
			case "hardlink":
				if err := os.Link(name, name+"-link"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := reacquireBackupDB(name, reference, id, baseline)
			if mode == "unchanged" {
				if err != nil || got == nil {
					t.Fatal(got, err)
				}
				if err := got.Close(); err != nil {
					t.Fatal(err)
				}
			} else if got != nil || !errors.Is(err, errBackupDBReacquire) {
				t.Fatal(got, err)
			}
			if gotID, err := identifyBackupTemp(reference); mode != "hardlink" && (err != nil || gotID != id) {
				t.Fatal("reference released/changed", gotID, err)
			}
			path := name
			if mode == "missing" || mode == "replacement" {
				path = name + "-original"
			}
			data, err := os.ReadFile(path)
			if err != nil || (mode != "mutation" && string(data) != "abc") {
				t.Fatal("original lost", string(data), err)
			}
			if mode == "replacement" {
				if data, err := os.ReadFile(name); err != nil || string(data) != "abc" {
					t.Fatal("replacement lost", err)
				}
			}
		})
	}
}

func TestBackupDBReacquireFaults(t *testing.T) {
	fault := errors.New("verification failed")
	closeFault := errors.New("close failed")
	for _, mode := range []string{"reference-before", "reference-after", "open", "baseline", "mismatch", "close"} {
		t.Run(mode, func(t *testing.T) {
			identityCalls, opens, closes := 0, 0, 0
			candidate, reference := new(os.File), new(os.File)
			id := BackupTempIdentity{VolumeSerial: 1}
			ops := backupDBReacquireOps{
				open: func(string) (*os.File, error) {
					opens++
					if mode == "open" {
						return nil, fault
					}
					return candidate, nil
				},
				identity: func(f *os.File) (BackupTempIdentity, error) {
					if f != reference {
						t.Fatal("wrong reference")
					}
					identityCalls++
					if mode == "reference-before" || mode == "reference-after" && identityCalls == 2 {
						return BackupTempIdentity{}, fault
					}
					return id, nil
				},
				baseline: func(f *os.File, _ BackupTempIdentity, _ string) (backupDBBaseline, error) {
					if f != candidate {
						t.Fatal("wrong candidate")
					}
					if mode == "baseline" || mode == "close" {
						return backupDBBaseline{}, fault
					}
					if mode == "mismatch" {
						return backupDBBaseline{size: 1}, nil
					}
					return backupDBBaseline{}, nil
				},
				close: func(f *os.File) error {
					if f != candidate {
						t.Fatal("closed reference")
					}
					closes++
					if mode == "close" {
						return closeFault
					}
					return nil
				},
			}
			got, err := verifyBackupDBReacquisition("copy", reference, id, backupDBBaseline{}, ops)
			if got != nil || !errors.Is(err, errBackupDBReacquire) {
				t.Fatal(got, err)
			}
			wantCloses := 1
			if mode == "reference-before" || mode == "open" {
				wantCloses = 0
			}
			if closes != wantCloses {
				t.Fatal("close count", closes)
			}
			if mode == "reference-before" && opens != 0 {
				t.Fatal("opened after refusal")
			}
			if mode != "mismatch" && !errors.Is(err, fault) {
				t.Fatal("lost cause", err)
			}
			if mode == "close" && !errors.Is(err, closeFault) {
				t.Fatal("lost release failure", err)
			}
		})
	}
}
