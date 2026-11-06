//go:build windows

package dbhandle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"golang.org/x/sys/windows"
)

func openExisting(name string, access, share uint32) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, access, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

type contents struct {
	size int64
	hash [32]byte
}

func fingerprint(t *testing.T, f *os.File) contents {
	t.Helper()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1<<20 {
		t.Fatal("probe output exceeds synthetic limit")
	}
	h := sha256.New()
	if n, err := io.Copy(h, io.NewSectionReader(f, 0, info.Size())); err != nil || n != info.Size() {
		t.Fatal(n, err)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return contents{info.Size(), sum}
}

func finalName(t *testing.T, f *os.File) string {
	t.Helper()
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		t.Fatal(n, err)
	}
	return strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
}

// Probe only. The attribute-only reference stays open across data-handle
// release. It does not prevent a rename or write; the new data/delete handle
// requests share=0, then identity, name, links and contents are checked again.
func reacquireAndCheck(t *testing.T, name string, reference *os.File, wantID identity, wantContents contents) string {
	t.Helper()
	file, err := openExisting(name, windows.GENERIC_READ|windows.DELETE, 0)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return "sharing"
	}
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return "missing"
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		t.Fatal(err)
	}
	kind, err := windows.GetFileType(windows.Handle(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if kind != windows.FILE_TYPE_DISK || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1 {
		return "attributes"
	}
	if id(t, file) != wantID || id(t, reference) != wantID || !strings.EqualFold(finalName(t, file), name) {
		return "identity"
	}
	if fingerprint(t, file) != wantContents {
		return "content"
	}
	remove := byte(1)
	if err := windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfo, &remove, 1); err != nil {
		t.Fatal(err)
	}
	// Both object references close before absence is checked; parents stay held.
	if err := errors.Join(file.Close(), reference.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(name); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatal("absence not confirmed", err)
	}
	return "deleted"
}

func TestReacquireDBCopyForCleanup(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"unchanged", "deleted"}, {"replacement", "identity"}, {"missing", "missing"}, {"mutation", "content"}, {"open-writer", "sharing"}, {"hardlink", "attributes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := root(t)
			work, err := platform.CreateBackupWorkDirectory(context.Background(), dir)
			if errors.Is(err, platform.ErrBackupWorkDirectoryUnsupported) {
				t.Skipf("work directory unavailable: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer work.Close()
			source, err := sql.Open("sqlite", filepath.Join(dir, "source.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			source.SetMaxOpenConns(1)
			for _, statement := range []string{"CREATE TABLE works(id INTEGER PRIMARY KEY,title TEXT)", "INSERT INTO works VALUES(7,'合成作品')"} {
				if _, err := source.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			name := filepath.Join(work.Path(), "copy.sqlite")
			out, err := create(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			createdID := id(t, out)
			if _, err := source.Exec("VACUUM INTO ?", name); err != nil {
				t.Fatal(err)
			}
			if id(t, out) != createdID {
				t.Fatal("created output identity changed during copy")
			}
			wantID, wantContents := createdID, fingerprint(t, out)
			reference, err := openExisting(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
			if err != nil {
				t.Fatal("attribute reference", err)
			}
			defer reference.Close()
			if id(t, reference) != wantID {
				t.Fatal("reference mismatch before release")
			}
			if err := out.Close(); err != nil {
				t.Fatal(err)
			}
			switch tc.name {
			case "replacement", "missing":
				if err := os.Rename(name, name+"-displaced"); err != nil {
					t.Fatal(err)
				}
				if tc.name == "replacement" {
					if err := os.WriteFile(name, []byte("replacement must remain"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "mutation":
				writer, err := os.OpenFile(name, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := writer.WriteAt([]byte("X"), 0)
				if err := errors.Join(writeErr, writer.Close()); err != nil {
					t.Fatal(err)
				}
			case "open-writer":
				writer, err := os.OpenFile(name, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
			case "hardlink":
				if err := os.Link(name, name+"-link"); err != nil {
					t.Fatal(err)
				}
			}
			got := reacquireAndCheck(t, name, reference, wantID, wantContents)
			if got != tc.want {
				t.Fatal("unexpected outcome", got, tc.want)
			}
			if got == "deleted" {
				r, err := work.DiscardEmpty()
				if err != nil || !r.Absent {
					t.Fatal(r, err)
				}
			} else {
				if got != "missing" {
					if _, err := os.Stat(name); err != nil {
						t.Fatal("refused file not preserved", err)
					}
				}
				if tc.name == "replacement" {
					b, err := os.ReadFile(name)
					if err != nil || string(b) != "replacement must remain" {
						t.Fatal(b, err)
					}
				}
				if tc.name == "replacement" || tc.name == "missing" {
					original, err := os.Open(name + "-displaced")
					if err != nil {
						t.Fatal("original lost", err)
					}
					if id(t, original) != wantID || fingerprint(t, original) != wantContents {
						original.Close()
						t.Fatal("original modified")
					}
					if err := original.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "mutation" {
					info, err := os.Stat(name)
					if err != nil || info.Size() != wantContents.size {
						t.Fatal("mutation changed byte count", info, err)
					}
				}
			}
			t.Logf("outcome=%s; attribute reference retained across data-handle release", got)
		})
	}
}
