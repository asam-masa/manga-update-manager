package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"golang.org/x/sys/windows"
)

const sourceBytes = "synthetic source, not an application ZIP"

func restoreSourceFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(workDirectoryRoot(t), "input")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "日本語 空白.zip")
	if err := os.WriteFile(name, []byte(sourceBytes), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestRestoreSourceNativeOwnership(t *testing.T) {
	name := restoreSourceFixture(t)
	s, err := OpenRestoreSource(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.Path() != name || s.Identity() == (BackupTempIdentity{}) || s.size != int64(len(sourceBytes)) || s.ReleaseResult().CloseAttempted {
		t.Fatal(s)
	}
	got := make([]byte, len(sourceBytes))
	if _, err := s.file.ReadAt(got, 0); err != nil || string(got) != sourceBytes {
		t.Fatal("read access", err)
	}
	if _, err := s.file.Write([]byte("bad")); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("write access", err)
	}
	if err := markBackupTempDeleted(s.file); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("delete access", err)
	}
	dir := filepath.Dir(name)
	if err := os.Rename(dir, dir+"-moved"); err == nil {
		t.Fatal("parents not held")
	}
	if err := os.Rename(name, name+"-moved"); err == nil {
		t.Fatal("source not held")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if !s.ReleaseResult().Confirmed() {
		t.Fatal(s.ReleaseResult())
	}
	if got, err := os.ReadFile(name); err != nil || string(got) != sourceBytes {
		t.Fatal("source changed", err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal("parents not released", err)
	}
	if err := os.Rename(dir+"-moved", dir); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSourceNativeRefusals(t *testing.T) {
	for _, mode := range []string{"missing", "directory", "short", "hardlink", "symlink", "parent-link", "prior-writer", "prior-delete"} {
		t.Run(mode, func(t *testing.T) {
			name := restoreSourceFixture(t)
			selected := name
			var prior *os.File
			switch mode {
			case "missing":
				selected += ".missing"
			case "directory":
				selected = filepath.Dir(name)
			case "short":
				if err := os.WriteFile(name, make([]byte, 21), 0600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(name, name+".alias"); err != nil {
					t.Fatal(err)
				}
			case "symlink", "parent-link":
				selected = name + ".link"
				target := name
				if mode == "parent-link" {
					target = filepath.Dir(name)
				}
				if err := os.Symlink(target, selected); err != nil {
					if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
						t.Skipf("symlink capability not confirmed: %v", err)
					}
					t.Fatal(err)
				}
				if mode == "parent-link" {
					selected = filepath.Join(selected, filepath.Base(name))
				}
			case "prior-writer", "prior-delete":
				p, err := windows.UTF16PtrFromString(name)
				if err != nil {
					t.Fatal(err)
				}
				access := uint32(windows.GENERIC_WRITE)
				if mode == "prior-delete" {
					access = windows.DELETE
				}
				h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				prior = os.NewFile(uintptr(h), name)
				t.Cleanup(func() { _ = prior.Close() })
			}
			s, err := OpenRestoreSource(context.Background(), selected)
			var detail *RestoreSourceError
			if s != nil || !errors.As(err, &detail) || !errors.Is(err, ErrRestoreSource) || !detail.Release.Confirmed() {
				t.Fatal(s, err)
			}
			// A DELETE handle also conflicts with os.ReadFile's sharing flags.
			if prior != nil {
				if err := prior.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "short" {
				got, e := os.ReadFile(name)
				if e != nil || string(got) != sourceBytes {
					t.Fatal("refused source changed", e)
				}
			}
		})
	}
}

type restoreSourceTestParents struct {
	release BackupDirectoryReleaseResult
	closes  int
	err     error
	unknown bool
	order   *[]string
}

func (p *restoreSourceTestParents) Close() error {
	p.closes++
	*p.order = append(*p.order, "parents")
	p.release.CloseAttempted = true
	if p.err == nil && !p.unknown {
		p.release.Released = p.release.Acquired
	}
	return p.err
}
func (p *restoreSourceTestParents) ReleaseResult() BackupDirectoryReleaseResult { return p.release }

func sourceTestOps(order *[]string) (restoreSourceOps, *restoreSourceTestParents) {
	p := &restoreSourceTestParents{release: BackupDirectoryReleaseResult{Acquired: 1}, order: order}
	return restoreSourceOps{
		hold:    func(context.Context, string) (restoreSourceParents, error) { return p, nil },
		open:    openRestoreSourceFile,
		observe: observeRestoreSource,
		close:   func(f *os.File) error { *order = append(*order, "file"); return f.Close() },
	}, p
}

func TestRestoreSourceAcquisitionFaults(t *testing.T) {
	for _, mode := range []string{"hold-unknown", "hold-released", "hold-owner-error", "open", "open-owner-error", "nil-open", "observe", "name", "short", "oversize", "cancel-before", "cancel-held", "cancel-opened", "cancel-observed", "close", "parents", "unconfirmed-parents", "cancel-close-parents"} {
		t.Run(mode, func(t *testing.T) {
			name := restoreSourceFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			order := []string{}
			ops, parents := sourceTestOps(&order)
			fault, fileFault, parentFault := errors.New("acquisition fault"), errors.New("file Close uncertainty"), errors.New("parent Close uncertainty")
			switch mode {
			case "hold-unknown":
				ops.hold = func(context.Context, string) (restoreSourceParents, error) { return nil, fault }
			case "hold-released":
				ops.hold = func(context.Context, string) (restoreSourceParents, error) {
					return nil, &BackupDirectoryHoldError{Release: BackupDirectoryReleaseResult{Acquired: 2, Released: 2, CloseAttempted: true}, Err: fault}
				}
			case "hold-owner-error":
				ops.hold = func(context.Context, string) (restoreSourceParents, error) { return parents, fault }
			case "open":
				ops.open = func(string) (*os.File, error) { return nil, fault }
			case "open-owner-error":
				ops.open = func(path string) (*os.File, error) {
					f, e := openRestoreSourceFile(path)
					return f, errors.Join(e, fault)
				}
			case "nil-open":
				ops.open = func(string) (*os.File, error) { return nil, nil }
			case "observe", "close", "parents", "unconfirmed-parents":
				ops.observe = func(*os.File) (restoreSourceObservation, error) { return restoreSourceObservation{}, fault }
			case "name", "short", "oversize":
				ops.observe = func(f *os.File) (restoreSourceObservation, error) {
					o, err := observeRestoreSource(f)
					if mode == "name" {
						o.name += ".different"
					}
					if mode == "short" {
						o.size = 21
					}
					if mode == "oversize" {
						o.size = backup.MaxArchiveBytes + 1
					}
					return o, err
				}
			case "cancel-before":
				cancel()
			case "cancel-held":
				ops.hold = func(context.Context, string) (restoreSourceParents, error) { cancel(); return parents, nil }
			case "cancel-opened", "cancel-close-parents":
				ops.open = func(path string) (*os.File, error) { f, e := openRestoreSourceFile(path); cancel(); return f, e }
			case "cancel-observed":
				ops.observe = func(f *os.File) (restoreSourceObservation, error) {
					o, e := observeRestoreSource(f)
					cancel()
					return o, e
				}
			}
			if mode == "close" || mode == "cancel-close-parents" {
				ops.close = func(f *os.File) error { order = append(order, "file"); return errors.Join(f.Close(), fileFault) }
			}
			if mode == "parents" || mode == "cancel-close-parents" {
				parents.err = parentFault
			}
			parents.unknown = mode == "unconfirmed-parents"
			s, err := openRestoreSource(ctx, name, ops)
			var detail *RestoreSourceError
			if s != nil || !errors.As(err, &detail) || detail.Path != name {
				t.Fatal(s, err)
			}
			unconfirmed := mode == "hold-unknown" || mode == "close" || mode == "parents" || mode == "unconfirmed-parents" || mode == "cancel-close-parents"
			if detail.Release.Confirmed() == unconfirmed || errors.Is(err, ErrRestoreSourceRelease) != unconfirmed {
				t.Fatal("wrong release classification", detail, err)
			}
			if mode == "close" || mode == "cancel-close-parents" {
				if !errors.Is(err, fileFault) || detail.Release.FileClosed {
					t.Fatal(err)
				}
			}
			if mode == "parents" || mode == "cancel-close-parents" {
				if !errors.Is(err, parentFault) {
					t.Fatal(err)
				}
			}
			if len(mode) >= 6 && mode[:6] == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation", err)
				}
			}
			if len(order) == 2 && (order[0] != "file" || order[1] != "parents") {
				t.Fatal("release order", order)
			}
			if parents.closes > 1 {
				t.Fatal("retried parent release")
			}
			if got, e := os.ReadFile(name); e != nil || string(got) != sourceBytes {
				t.Fatal("source changed", e)
			}
		})
	}
}

func TestRestoreSourceBoundsAndRepeatedClose(t *testing.T) {
	for _, size := range []int64{22, backup.MaxDatabaseBytes + 1, backup.MaxArchiveBytes} {
		name := restoreSourceFixture(t)
		order := []string{}
		ops, parents := sourceTestOps(&order)
		ops.observe = func(f *os.File) (restoreSourceObservation, error) {
			o, e := observeRestoreSource(f)
			o.size = size
			return o, e
		}
		s, err := openRestoreSource(context.Background(), name, ops)
		if err != nil || s.size != size {
			t.Fatal(s, err)
		}
		if err = s.Close(); err != nil || !s.ReleaseResult().Confirmed() {
			t.Fatal(err)
		}
		if err = s.Close(); err != nil || parents.closes != 1 || len(order) != 2 {
			t.Fatal("Close retried", err, order)
		}
	}
	var s *RestoreSource
	if s.Close() != nil || s.ReleaseResult().Confirmed() {
		t.Fatal("nil owner claimed release")
	}
}

func TestRestoreSourceRejectsPathsBeforeAcquiring(t *testing.T) {
	for _, name := range []string{"", "relative.zip", `C:\a\..\source.zip`, `C:\a\.\source.zip`, `\\server\share\source.zip`, `\\?\C:\source.zip`, `C:\source.zip:stream`, `C:\NUL.zip`, `C:\source.zip `} {
		order := []string{}
		ops, _ := sourceTestOps(&order)
		ops.hold = func(context.Context, string) (restoreSourceParents, error) {
			t.Fatal("invalid path acquired resources")
			return nil, nil
		}
		s, err := openRestoreSource(context.Background(), name, ops)
		var detail *RestoreSourceError
		if s != nil || !errors.As(err, &detail) || !errors.Is(err, ErrUnsafeRestoreSource) || !detail.Release.Confirmed() || len(order) != 0 {
			t.Fatal(s, err)
		}
	}
}

func TestRestoreSourceCloseFailuresCached(t *testing.T) {
	name := restoreSourceFixture(t)
	order := []string{}
	ops, parents := sourceTestOps(&order)
	s, err := openRestoreSource(context.Background(), name, ops)
	if err != nil {
		t.Fatal(err)
	}
	fault, parentFault := errors.New("file Close uncertainty"), errors.New("parent Close uncertainty")
	s.closeFile = func() error { order = append(order, "file"); return errors.Join(s.file.Close(), fault) }
	parents.err = parentFault
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			err := s.Close()
			if !errors.Is(err, fault) || !errors.Is(err, parentFault) || !errors.Is(err, ErrRestoreSourceRelease) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	r := s.ReleaseResult()
	if r.Confirmed() || r.FileClosed || !r.CloseAttempted || !r.Parents.CloseAttempted || parents.closes != 1 || len(order) != 2 || order[0] != "file" || order[1] != "parents" {
		t.Fatal(r, order)
	}
	if got, err := os.ReadFile(name); err != nil || string(got) != sourceBytes {
		t.Fatal("source changed", err)
	}
}
