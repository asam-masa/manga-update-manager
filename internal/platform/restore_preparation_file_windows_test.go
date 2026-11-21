package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

func restoreFileOps() restorePreparationFileOps {
	return restorePreparationFileOps{create: createRestorePreparationFileHandle, observe: observeBackupDB, close: func(f *os.File) error { return f.Close() }}
}

func TestRestorePreparationFileNativeLifetime(t *testing.T) {
	work := outputWork(t)
	first, err := CreateRestorePreparationFile(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Error(err)
		}
	})
	second, err := CreateRestorePreparationFile(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	})
	if first.Path() == second.Path() || filepath.Dir(first.Path()) != work.Path() || !strings.HasPrefix(filepath.Base(first.Path()), ".manga-update-restore-") {
		t.Fatal("not dedicated child paths")
	}
	if first.ReleaseResult().Confirmed() {
		t.Fatal("premature release")
	}
	if _, err = first.file.Write([]byte("synthetic payload")); err != nil {
		t.Fatal(err)
	}
	if opened, e := os.Open(first.Path()); e == nil {
		opened.Close()
		t.Fatal("shared reopen accepted")
	} else if !errors.Is(e, windows.ERROR_SHARING_VIOLATION) {
		t.Fatal(e)
	}
	if e := os.Rename(work.Path(), work.Path()+"-moved"); !errors.Is(e, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("parent moved: %v", e)
	}
	if e := first.Close(); e != nil {
		t.Fatal(e)
	}
	if e := first.Close(); e != nil || !first.ReleaseResult().Confirmed() {
		t.Fatal("close not idempotent", e)
	}
	copy := first.ReleaseResult()
	copy.HandleClosed = false
	if !first.ReleaseResult().Confirmed() {
		t.Fatal("release observation aliased")
	}
	got, e := os.ReadFile(first.Path())
	if e != nil || string(got) != "synthetic payload" {
		t.Fatal("Close deleted or changed child", e)
	}
	if e = work.VerifyIdentity(); e != nil || work.ReleaseResult().CloseAttempted {
		t.Fatal("child released parent", e)
	}
	if _, e = second.file.Write([]byte("still owned")); e != nil {
		t.Fatal(e)
	}
}

func TestRestorePreparationFileFailures(t *testing.T) {
	fault := errors.New("synthetic failure")
	for _, mode := range []string{"cancel-before", "create", "create-acquired-error", "observe", "name", "size", "cancel-after", "close"} {
		t.Run(mode, func(t *testing.T) {
			work := outputWork(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ops := restoreFileOps()
			closes, creates := 0, 0
			var held *os.File
			ops.create = func(name string) (*os.File, error) {
				creates++
				if mode == "create" {
					return nil, fault
				}
				f, e := createRestorePreparationFileHandle(name)
				held = f
				if e == nil && mode == "create-acquired-error" {
					return f, fault
				}
				if mode == "cancel-after" {
					cancel()
				}
				return f, e
			}
			ops.observe = func(f *os.File) (backupDBObservation, error) {
				o, e := observeBackupDB(f)
				switch mode {
				case "observe", "close":
					return o, fault
				case "name":
					o.name += "-wrong"
				case "size":
					o.size = 1
				}
				return o, e
			}
			ops.close = func(f *os.File) error {
				closes++
				e := f.Close()
				if mode == "close" {
					return errors.Join(e, fault)
				}
				return e
			}
			if mode == "cancel-before" {
				cancel()
			}
			out, err := createRestorePreparationFile(ctx, work, [16]byte{}, ops)
			var observed *RestorePreparationFileError
			if out != nil || !errors.As(err, &observed) {
				t.Fatalf("missing failure observations: %v", err)
			}
			cause := fault
			if mode == "cancel-before" || mode == "cancel-after" {
				cause = context.Canceled
			}
			if mode == "name" || mode == "size" {
				cause = ErrRestorePreparationFileIdentity
			}
			if !errors.Is(err, cause) {
				t.Fatal("original failure cause lost", err)
			}
			acquired := mode != "cancel-before" && mode != "create"
			if observed.Created != (acquired && mode != "create-acquired-error") || observed.Release.HandleAcquired != acquired || !observed.Release.CloseAttempted {
				t.Fatalf("wrong observation: %+v", observed)
			}
			if (closes == 1) != acquired || closes > 1 {
				t.Fatalf("close count: %d", closes)
			}
			if observed.Release.Confirmed() != (mode != "close") {
				t.Fatalf("release confirmation: %+v", observed.Release)
			}
			if mode == "close" && !errors.Is(err, ErrRestorePreparationFileRelease) {
				t.Fatal("release cause lost")
			}
			if mode == "cancel-before" && (creates != 0 || observed.Path != "") {
				t.Fatal("cancel created output")
			}
			if acquired {
				if held == nil {
					t.Fatal("fixture missing handle")
				}
				if _, e := os.Stat(observed.Path); e != nil {
					t.Fatal("failure deleted artifact", e)
				}
			}
			if e := work.VerifyIdentity(); e != nil || work.ReleaseResult().CloseAttempted {
				t.Fatal("failure released parent", e)
			}
		})
	}
}

func TestRestorePreparationFileRefusesCollisionAndInvalidParent(t *testing.T) {
	work := outputWork(t)
	ops := restoreFileOps()
	first, err := createRestorePreparationFile(context.Background(), work, [16]byte{}, ops)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.file.Write([]byte("collision sentinel")); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = createRestorePreparationFile(context.Background(), work, [16]byte{}, ops)
	if !errors.Is(err, windows.ERROR_FILE_EXISTS) && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		t.Fatalf("collision accepted: %v", err)
	}
	if got, e := os.ReadFile(first.Path()); e != nil || string(got) != "collision sentinel" {
		t.Fatal("collision changed existing file", e)
	}
	oldID, oldPath := work.identity, work.path
	work.identity.FileID[0] ^= 1
	if _, err = CreateRestorePreparationFile(context.Background(), work); !errors.Is(err, ErrBackupWorkDirectoryIdentity) {
		t.Fatal("mismatched parent ID accepted", err)
	}
	work.identity = oldID
	work.path += "-mismatch"
	if _, err = CreateRestorePreparationFile(context.Background(), work); !errors.Is(err, ErrBackupWorkDirectoryIdentity) {
		t.Fatal("mismatched parent name accepted", err)
	}
	work.path = oldPath
	if err = work.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = CreateRestorePreparationFile(context.Background(), work); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed parent accepted", err)
	}
	if _, err = CreateRestorePreparationFile(context.Background(), nil); !errors.Is(err, ErrRestorePreparationFileIdentity) {
		t.Fatal("nil parent accepted", err)
	}
}

func TestRestorePreparationFileCloseFailureIsStable(t *testing.T) {
	work := outputWork(t)
	ops := restoreFileOps()
	fault := errors.New("close result unknown")
	calls := 0
	ops.close = func(f *os.File) error { calls++; return errors.Join(f.Close(), fault) }
	out, err := createRestorePreparationFile(context.Background(), work, [16]byte{}, ops)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if e := out.Close(); !errors.Is(e, fault) {
				t.Error("close cause lost", e)
			}
		})
	}
	wg.Wait()
	if calls != 1 || out.ReleaseResult().Confirmed() || !out.ReleaseResult().CloseAttempted {
		t.Fatalf("close retried/confirmed: %d %+v", calls, out.ReleaseResult())
	}
	if e := work.VerifyIdentity(); e != nil {
		t.Fatal("parent released", e)
	}
}
