package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func dbCleanupFixture(t *testing.T) (*backupDBCleanup, string) {
	t.Helper()
	name := filepath.Join(workDirectoryRoot(t), "copy.sqlite")
	parent, err := HoldBackupDirectory(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	})
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("partial output"); err != nil {
		t.Fatal(err)
	}
	id, err := identifyBackupTemp(f)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := captureBackupDBBaseline(f, id, name)
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
	transferred := false
	defer func() {
		if !transferred {
			_ = reference.Close()
		}
	}()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := acquireBackupDBCleanup(name, reference, id, baseline)
	if err != nil {
		t.Fatal(err)
	}
	transferred = true
	t.Cleanup(func() { _ = c.Close() })
	return c, name
}

func TestBackupDBDiscardNative(t *testing.T) {
	for _, mode := range []string{"delete", "release-only", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			c, name := dbCleanupFixture(t)
			if mode == "release-only" {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(name); err != nil {
					t.Fatal("file not preserved", err)
				}
				if _, err := c.Discard(); !errors.Is(err, os.ErrClosed) {
					t.Fatal(err)
				}
				return
			}
			absent := confirmBackupWorkDirectoryAbsent
			if mode == "replacement" {
				absent = func(path string) error {
					if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
						t.Fatal(err)
					}
					return confirmBackupWorkDirectoryAbsent(path)
				}
			}
			r, err := c.discard(markBackupTempDeleted, func(f *os.File) error { return f.Close() }, absent)
			if !r.marked || !r.fileClosed || !r.referenceClosed {
				t.Fatal(r, err)
			}
			if mode == "delete" {
				if err != nil || !r.absent {
					t.Fatal(r, err)
				}
			} else {
				if err == nil || r.absent {
					t.Fatal(r, err)
				}
				if data, err := os.ReadFile(name); err != nil || string(data) != "replacement" {
					t.Fatal("replacement lost", err)
				}
			}
			if again, err := c.Discard(); again != r || !errors.Is(err, errBackupDBDiscardAttempted) {
				t.Fatal(again, err)
			}
		})
	}
}

func TestBackupDBDiscardFailuresAndOrder(t *testing.T) {
	markFault, fileFault, refFault, absentFault := errors.New("mark"), errors.New("file close"), errors.New("reference close"), errors.New("absence")
	for _, mode := range []string{"success", "mark", "file", "reference", "absence", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			file, reference := new(os.File), new(os.File)
			c := &backupDBCleanup{file: file, reference: reference, result: backupDBDiscardResult{path: "candidate"}}
			var calls []string
			var causes []error
			mark := func(f *os.File) error {
				if f != file {
					t.Fatal("wrong mark target")
				}
				calls = append(calls, "mark")
				if mode == "mark" || mode == "multiple" {
					causes = append(causes, markFault)
					return markFault
				}
				return nil
			}
			closeFile := func(f *os.File) error {
				if f == file {
					calls = append(calls, "file")
					if mode == "file" || mode == "multiple" {
						causes = append(causes, fileFault)
						return fileFault
					}
				} else if f == reference {
					calls = append(calls, "reference")
					if mode == "reference" || mode == "multiple" {
						causes = append(causes, refFault)
						return refFault
					}
				} else {
					t.Fatal("unknown close")
				}
				return nil
			}
			absent := func(path string) error {
				if path != "candidate" {
					t.Fatal(path)
				}
				calls = append(calls, "absence")
				if mode == "absence" {
					causes = append(causes, absentFault)
					return absentFault
				}
				return nil
			}
			r, err := c.discard(mark, closeFile, absent)
			want := []string{"mark", "file", "reference"}
			if mode == "success" || mode == "absence" {
				want = append(want, "absence")
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatal(calls, want)
			}
			for _, cause := range causes {
				if !errors.Is(err, cause) {
					t.Fatal("lost cause", cause, err)
				}
			}
			if r.absent != (mode == "success") || r.marked != (mode != "mark" && mode != "multiple") || r.fileClosed != (mode != "file" && mode != "multiple") || r.referenceClosed != (mode != "reference" && mode != "multiple") {
				t.Fatal(r)
			}
			if (err == nil) != (mode == "success") {
				t.Fatal(err)
			}
			before := len(calls)
			if again, err := c.discard(mark, closeFile, absent); again != r || !errors.Is(err, errBackupDBDiscardAttempted) {
				t.Fatal(again, err)
			}
			_ = c.Close()
			if len(calls) != before {
				t.Fatal("repeated I/O", calls)
			}
		})
	}
}
