package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
	"golang.org/x/sys/windows"
)

const testNewCoverID = "0123456789abcdef0123456789abcdef"

func generatedCover(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 3, 4))); err != nil {
		t.Fatal(err)
	}
	data, err := thumbnail.Generate(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWriteNewCoverAndReopen(t *testing.T) {
	if unsafe.Sizeof(coverFileIdentity{}) != 24 || unsafe.Offsetof(coverFileIdentity{}.FileID) != 8 {
		t.Fatal("FILE_ID_INFO layout mismatch")
	}
	base := t.TempDir()
	data := generatedCover(t)
	got, err := WriteNewCover(context.Background(), base, testNewCoverID, data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ByteSize != int64(len(data)) || got.SHA256 != sha256.Sum256(data) {
		t.Fatalf("wrong verification: %#v", got)
	}
	name := filepath.Join(base, "covers", testNewCoverID+".png")
	file, err := openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	after, err := captureCoverFile(file, got.ByteSize)
	closeErr := file.Close()
	if err != nil || closeErr != nil || after != got {
		t.Fatalf("reopen changed: %#v %v %v", after, err, closeErr)
	}
	contents, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(contents, data) {
		t.Fatal("stored bytes differ")
	}
	// A colliding write must leave the first successful file untouched.
	failed, err := WriteNewCover(context.Background(), base, testNewCoverID, []byte("replacement"))
	if err == nil || failed != (CoverFileInfo{}) {
		t.Fatal("duplicate succeeded")
	}
	contents, err = os.ReadFile(name)
	if err != nil || !bytes.Equal(contents, data) {
		t.Fatal("duplicate changed existing file")
	}
}

func TestWriteNewCoverRefusals(t *testing.T) {
	for _, mode := range []string{"link", "directory", "held", "invalid-id", "empty", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			covers := filepath.Join(base, "covers")
			if err := os.Mkdir(covers, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "source.png")
			original := generatedCover(t)
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(covers, testNewCoverID+".png")
			ctx := context.Background()
			id, data := testNewCoverID, original
			switch mode {
			case "link":
				if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
			case "held":
				p, err := windows.UTF16PtrFromString(covers)
				if err != nil {
					t.Fatal(err)
				}
				h, err := windows.CreateFile(p, windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer windows.CloseHandle(h)
			case "invalid-id":
				id = "../" + strings.Repeat("a", 32)
			case "empty":
				data = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			got, err := WriteNewCover(ctx, base, id, data)
			if err == nil || errors.Is(err, ErrCoverRelease) || got != (CoverFileInfo{}) {
				t.Fatalf("refusal returned success: %#v %v", got, err)
			}
			contents, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(contents, original) {
				t.Fatal("original changed")
			}
			if mode != "link" && mode != "directory" {
				if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refusal created output")
				}
			}
		})
	}
}

type failingCoverWriter struct {
	short               bool
	writeErr, errorSync error
	synced              bool
}

func (f *failingCoverWriter) Write(data []byte) (int, error) {
	if f.short {
		return len(data) - 1, nil
	}
	return len(data), f.writeErr
}
func (f *failingCoverWriter) Sync() error { f.synced = true; return f.errorSync }

func TestCoverWriteFailures(t *testing.T) {
	failure := errors.New("synthetic I/O failure")
	for _, tc := range []struct {
		name string
		file failingCoverWriter
		want error
		sync bool
	}{
		{"short", failingCoverWriter{short: true}, io.ErrShortWrite, false},
		{"write", failingCoverWriter{writeErr: failure}, failure, false},
		{"sync", failingCoverWriter{errorSync: failure}, failure, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := tc.file
			err := writeAndSyncCover(context.Background(), &file, []byte("payload"))
			if !errors.Is(err, tc.want) || file.synced != tc.sync {
				t.Fatalf("I/O failure lost: %v %v", err, file.synced)
			}
		})
	}
}

func TestCaptureCoverRejectsUnsafeAndDifferentContent(t *testing.T) {
	base := t.TempDir()
	data := generatedCover(t)
	first, err := WriteNewCover(context.Background(), base, testNewCoverID, data)
	if err != nil {
		t.Fatal(err)
	}
	otherID := strings.Repeat("a", 32)
	other, err := WriteNewCover(context.Background(), base, otherID, data)
	if err != nil {
		t.Fatal(err)
	}
	if other == first || other.SHA256 != first.SHA256 {
		t.Fatal("same-content fixture did not isolate file identity")
	}
	name := filepath.Join(base, "covers", testNewCoverID+".png")
	changed := append([]byte(nil), data...)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(name, changed, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := captureCoverFile(file, first.ByteSize)
	file.Close()
	if err != nil || actual == first || actual.FileID != first.FileID || actual.SHA256 == first.SHA256 {
		t.Fatal("changed-content fixture did not isolate hash")
	}
	if err := os.Link(name, filepath.Join(base, "hard.png")); err != nil {
		t.Fatal(err)
	}
	file, err = openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	got, err := captureCoverFile(file, first.ByteSize)
	file.Close()
	if !errors.Is(err, ErrInvalidCoverFile) || got != (CoverFileInfo{}) {
		t.Fatal("hard link accepted")
	}
	got, err = captureCoverFile(file, first.ByteSize)
	if err == nil || got != (CoverFileInfo{}) {
		t.Fatal("closed handle accepted")
	}
}

func TestWriteNewCoverCancellationAfterCreationKeepsPartial(t *testing.T) {
	base := t.TempDir()
	name := filepath.Join(base, "covers", testNewCoverID+".png")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := coverCancelContext{Context: ctx, check: func() {
		if _, err := os.Lstat(name); err == nil {
			cancel()
		}
	}}
	got, err := WriteNewCover(controlled, base, testNewCoverID, generatedCover(t))
	if !errors.Is(err, context.Canceled) || got != (CoverFileInfo{}) {
		t.Fatalf("cancellation: %#v %v", got, err)
	}
	stat, err := os.Stat(name)
	if err != nil || stat.Size() != 0 {
		t.Fatalf("expected retained empty partial file: %v", err)
	}
	if err := os.Rename(base, base+"-moved"); err != nil {
		t.Fatalf("handles leaked: %v", err)
	}
}
