package application

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
	"golang.org/x/sys/windows"
)

type databaseCopyFunc func(context.Context, string) error

func (f databaseCopyFunc) CopyDatabase(ctx context.Context, path string) error { return f(ctx, path) }

var databaseStageFault = errors.New("database stage failed")
var databaseCleanupFault = errors.New("database cleanup failed")

type databaseWorkFake struct {
	calls           []string
	mode            string
	sizes, verifies int
	cancel          context.CancelFunc
}

func (f *databaseWorkFake) Path() string { return "prepared.sqlite" }
func (f *databaseWorkFake) ReadAt(p []byte, n int64) (int, error) {
	return strings.NewReader("synthetic").ReadAt(p, n)
}
func (f *databaseWorkFake) VerifyIdentity() error {
	f.calls = append(f.calls, "verify")
	f.verifies++
	if f.mode == "verify" {
		return databaseStageFault
	}
	if f.mode == "cancel-verify" && f.verifies == 3 {
		f.cancel()
	}
	return nil
}
func (f *databaseWorkFake) Size() (int64, error) {
	f.calls = append(f.calls, "size")
	f.sizes++
	if f.sizes == 1 {
		if f.mode == "nonempty" {
			return 1, nil
		}
		return 0, nil
	}
	if f.mode == "size" {
		return 0, databaseStageFault
	}
	if f.mode == "empty" {
		return 0, nil
	}
	if f.mode == "oversize" {
		return backup.MaxDatabaseBytes + 1, nil
	}
	if f.mode == "changed" && f.sizes == 3 {
		return 10, nil
	}
	return 9, nil
}
func (f *databaseWorkFake) Cleanup() (platform.BackupDatabaseCleanupResult, error) {
	f.calls = append(f.calls, "cleanup")
	r := platform.BackupDatabaseCleanupResult{File: platform.BackupDatabaseFileCleanupResult{Path: f.Path()}}
	if f.mode == "cleanup" || f.mode == "multiple" {
		return r, databaseCleanupFault
	}
	return r, nil
}

func TestUseBackupDatabaseStages(t *testing.T) {
	for _, mode := range []string{"success", "verify", "nonempty", "copy", "size", "empty", "oversize", "validation", "use", "cleanup", "multiple", "cancel-before", "cancel-copy", "cancel-verify", "changed"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &databaseWorkFake{mode: mode, cancel: cancel}
			if mode == "cancel-before" {
				cancel()
			}
			copier := databaseCopyFunc(func(_ context.Context, path string) error {
				f.calls = append(f.calls, "copy")
				if path != f.Path() {
					t.Fatal(path)
				}
				if mode == "copy" {
					return databaseStageFault
				}
				if mode == "cancel-copy" {
					cancel()
				}
				return nil
			})
			validate := func(_ context.Context, path string) error {
				f.calls = append(f.calls, "validate")
				if path != f.Path() {
					t.Fatal(path)
				}
				if mode == "validation" || mode == "multiple" {
					return databaseStageFault
				}
				return nil
			}
			use := func(_ context.Context, r io.ReaderAt, size int64) error {
				f.calls = append(f.calls, "use")
				if r != f || size != 9 {
					t.Fatal(r, size)
				}
				if mode == "use" {
					return databaseStageFault
				}
				return nil
			}
			result, err := UseBackupDatabase(ctx, f, copier, validate, use)
			if (err == nil) != (mode == "success") {
				t.Fatal(result, err)
			}
			if err != nil && !errors.Is(err, ErrBackupDatabase) {
				t.Fatal(err)
			}
			count := 0
			used := false
			for _, call := range f.calls {
				if call == "cleanup" {
					count++
				}
				if call == "use" {
					used = true
				}
			}
			if count != 1 || f.calls[len(f.calls)-1] != "cleanup" {
				t.Fatal(f.calls)
			}
			if used && mode != "success" && mode != "use" && mode != "cleanup" {
				t.Fatal("advanced after refusal", f.calls)
			}
			if mode == "multiple" && (!errors.Is(result.OperationError, databaseStageFault) || !errors.Is(result.CleanupError, databaseCleanupFault) || !errors.Is(err, databaseStageFault) || !errors.Is(err, databaseCleanupFault)) {
				t.Fatal(result, err)
			}
			if strings.HasPrefix(mode, "cancel-") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if mode == "success" {
				want := []string{"verify", "size", "copy", "verify", "size", "validate", "verify", "size", "use", "verify", "size", "cleanup"}
				if !reflect.DeepEqual(f.calls, want) || !result.Copied || !result.Validated || !result.Consumed {
					t.Fatal(f.calls, result)
				}
			}
		})
	}
}

func TestUseBackupDatabaseRealComponents(t *testing.T) {
	// Normalize the fixture only: Windows CI TEMP can contain an 8.3 alias.
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var buf [32768]uint16
	n, nameErr := windows.GetFinalPathNameByHandle(windows.Handle(dir.Fd()), &buf[0], uint32(len(buf)), 0)
	if err := errors.Join(nameErr, dir.Close()); err != nil || n == 0 || n >= uint32(len(buf)) {
		t.Fatal(n, err)
	}
	root := strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
	work, err := platform.CreateBackupDatabaseWork(context.Background(), root)
	if errors.Is(err, platform.ErrBackupWorkDirectoryUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer work.Cleanup()
	s, err := storage.Open(context.Background(), filepath.Join(root, "source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	used := false
	r, err := UseBackupDatabase(context.Background(), work, s, storage.ValidateDatabaseCopy, func(_ context.Context, reader io.ReaderAt, size int64) error {
		used = true
		header := make([]byte, 16)
		if _, err := reader.ReadAt(header, 0); err != nil {
			return err
		}
		if size < 16 || string(header) != "SQLite format 3\x00" {
			t.Fatal(size, string(header))
		}
		return nil
	})
	if err != nil || !used || !r.Copied || !r.Validated || !r.Consumed || !r.Cleanup.File.Absent || !r.Cleanup.Directory.Absent {
		t.Fatal(r, err)
	}
}
