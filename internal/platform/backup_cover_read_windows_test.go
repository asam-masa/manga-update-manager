package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadBackupCoverClassification(t *testing.T) {
	for _, mode := range []string{"present", "missing", "changed", "short", "empty", "larger"} {
		t.Run(mode, func(t *testing.T) {
			dir, data := t.TempDir(), generatedCover(t)
			info, err := WriteNewCover(context.Background(), dir, testNewCoverID, data)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(dir, "covers", testNewCoverID+".png")
			want := BackupCoverChanged
			switch mode {
			case "present":
				want = BackupCoverPresent
			case "missing":
				want = BackupCoverMissing
				err = os.Remove(name)
			case "changed":
				data[0] ^= 1
				err = os.WriteFile(name, data, 0600)
			case "short":
				err = os.WriteFile(name, data[:len(data)/2], 0600)
			case "empty":
				err = os.WriteFile(name, nil, 0600)
			case "larger":
				err = os.WriteFile(name, append(data, 1), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadBackupCover(context.Background(), dir, testNewCoverID, info, int64(len(data)+1))
			if err != nil || got.Status != want {
				t.Fatal(got.Status, err)
			}
			if want == BackupCoverPresent {
				if !bytes.Equal(got.Data, data) {
					t.Fatal("wrong content")
				}
				got.Data[0] ^= 1
				after, err := os.ReadFile(name)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatal("source changed", err)
				}
			} else if got.Data != nil {
				t.Fatal("returned omitted content")
			}
			if mode != "missing" {
				f, err := openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
				if err != nil {
					t.Fatal("handle retained", err)
				}
				f.Close()
			}
		})
	}
}

func TestReadBackupCoverRefusesUnsafeAndOversized(t *testing.T) {
	for _, mode := range []string{"replacement", "hard-link", "symlink", "directory", "held", "identity", "volume", "invalid-id", "upper-id", "ancestor-link", "folder-missing", "oversized", "expected-size", "max-zero", "max-negative", "max-overflow", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir, data := t.TempDir(), generatedCover(t)
			info, err := WriteNewCover(context.Background(), dir, testNewCoverID, data)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(dir, "covers", testNewCoverID+".png")
			old := filepath.Join(dir, "original.png")
			id, limit, ctx := testNewCoverID, int64(len(data)), context.Background()
			switch mode {
			case "replacement", "symlink", "directory":
				if err = os.Rename(name, old); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "replacement":
					err = os.WriteFile(name, data, 0600)
				case "symlink":
					err = os.Symlink(old, name)
				case "directory":
					err = os.Mkdir(name, 0700)
				}
			case "hard-link":
				err = os.Link(name, old)
			case "held":
				f, e := openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
			case "identity":
				info.FileID[0] ^= 1
			case "volume":
				info.VolumeSerial++
			case "invalid-id":
				id = "../original"
			case "upper-id":
				id = strings.ToUpper(id)
			case "ancestor-link":
				link := filepath.Join(t.TempDir(), "linked")
				err = os.Symlink(dir, link)
				dir = link
			case "folder-missing":
				err = os.Rename(filepath.Join(dir, "covers"), filepath.Join(dir, "saved-covers"))
			case "oversized":
				err = os.WriteFile(name, append(data, 1), 0600)
			case "expected-size":
				info.ByteSize++
			case "max-zero":
				limit = 0
			case "max-negative":
				limit = -1
			case "max-overflow":
				limit = 1<<63 - 1
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadBackupCover(ctx, dir, id, info, limit)
			if err == nil || got.Status != "" || got.Data != nil {
				t.Fatal("unsafe accepted", got.Status, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if mode == "folder-missing" {
				if _, err := os.Stat(filepath.Join(dir, "covers")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("folder recreated", err)
				}
			}
			if mode == "replacement" || mode == "hard-link" || mode == "symlink" || mode == "directory" {
				after, err := os.ReadFile(old)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatal("original changed", err)
				}
			}
		})
	}
}

func TestReadBackupCoverReleaseFailureAndLateCancel(t *testing.T) {
	fault := errors.New("synthetic release failure")
	for _, mode := range []string{"present", "missing", "changed", "unsafe", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir, data := t.TempDir(), generatedCover(t)
			info, err := WriteNewCover(context.Background(), dir, testNewCoverID, data)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(dir, "covers", testNewCoverID+".png")
			if mode == "missing" {
				err = os.Remove(name)
			}
			if mode == "changed" {
				data[0] ^= 1
				err = os.WriteFile(name, data, 0600)
			}
			if mode == "unsafe" {
				info.FileID[0] ^= 1
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			got, err := readBackupCover(ctx, dir, testNewCoverID, info, int64(len(data)), func(c io.Closer) error {
				calls++
				e := closeCoverResource(c)
				if mode == "cancel" {
					cancel()
					return e
				}
				return errors.Join(e, coverReleaseError(fault))
			})
			if got.Status != "" || got.Data != nil {
				t.Fatal("result survived release failure", got.Status)
			}
			if mode == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, fault) || !errors.Is(err, ErrCoverRelease) {
				t.Fatal(err)
			}
			if mode == "unsafe" && !errors.Is(err, ErrInvalidCoverFile) {
				t.Fatal("original failure lost", err)
			}
			want := 2
			if mode == "missing" {
				want = 1
			}
			if calls != want {
				t.Fatal("release count", calls)
			}
			if err := os.Rename(dir, dir+"-moved"); err != nil {
				t.Fatal("parent retained", err)
			}
			if err := os.Rename(dir+"-moved", dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}
