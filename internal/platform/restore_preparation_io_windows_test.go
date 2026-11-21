package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func TestRestorePreparationIORoundTrip(t *testing.T) {
	work := outputWork(t)
	f, err := CreateRestorePreparationFile(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	if n, e := f.Write([]byte("abc")); n != 3 || e != nil {
		t.Fatal(n, e)
	}
	buf := make([]byte, 2)
	if n, e := f.ReadAt(buf, 1); n != 2 || e != nil || string(buf) != "bc" {
		t.Fatal(n, string(buf), e)
	}
	if n, e := f.Write([]byte("def")); n != 3 || e != nil {
		t.Fatal(n, e)
	}
	buf = make([]byte, 8)
	n, e := f.ReadAt(buf, 0)
	if n != 6 || !errors.Is(e, io.EOF) || string(buf[:n]) != "abcdef" {
		t.Fatalf("offset/count/EOF: %d %q %v", n, buf, e)
	}
	if n, e = f.ReadAt(buf, 99); n != 0 || !errors.Is(e, io.EOF) {
		t.Fatal(n, e)
	}
	if n, e = f.ReadAt(buf, -1); n != 0 || e == nil {
		t.Fatal("negative offset accepted", n, e)
	}
	if size, e := f.Size(); size != 6 || e != nil {
		t.Fatal(size, e)
	}
	if e = f.Sync(); e != nil {
		t.Fatal(e)
	}
	if e = work.VerifyIdentity(); e != nil || work.ReleaseResult().CloseAttempted {
		t.Fatal("I/O released parent", e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(f.Path())
	if e != nil || string(got) != "abcdef" {
		t.Fatal("Close changed data", e)
	}
	assertRestoreIOClosed(t, f)
}

func assertRestoreIOClosed(t *testing.T, f *RestorePreparationFile) {
	t.Helper()
	if n, e := f.Write([]byte("must not write")); n != 0 || !errors.Is(e, os.ErrClosed) {
		t.Fatal("write after close", n, e)
	}
	if n, e := f.ReadAt(make([]byte, 1), 0); n != 0 || !errors.Is(e, os.ErrClosed) {
		t.Fatal("read after close", n, e)
	}
	if e := f.Sync(); !errors.Is(e, os.ErrClosed) {
		t.Fatal("sync after close", e)
	}
	if n, e := f.Size(); n != 0 || !errors.Is(e, os.ErrClosed) {
		t.Fatal("size after close", n, e)
	}
}

func TestRestorePreparationIORejectsUncreatedAndFailedClose(t *testing.T) {
	assertRestoreIOClosed(t, nil)
	assertRestoreIOClosed(t, &RestorePreparationFile{})
	work := outputWork(t)
	ops := restoreFileOps()
	fault := errors.New("close unconfirmed")
	// Keep the actual handle writable: rejecting I/O must follow the logical
	// CloseAttempted state even when release is unconfirmed, not OS behavior.
	ops.close = func(*os.File) error { return fault }
	f, e := createRestorePreparationFile(context.Background(), work, [16]byte{}, ops)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := f.file.Close(); e != nil {
			t.Error(e)
		}
	})
	if e = f.Close(); !errors.Is(e, fault) || f.ReleaseResult().Confirmed() {
		t.Fatal("unknown close lost", e)
	}
	assertRestoreIOClosed(t, f)
	if n, e := f.file.Stat(); e != nil || n.Size() != 0 {
		t.Fatal("blocked write changed file", e)
	}
	if e = work.VerifyIdentity(); e != nil {
		t.Fatal("parent released", e)
	}
}

func TestRestorePreparationIOPreservesNativeErrors(t *testing.T) {
	work := outputWork(t)
	f, e := CreateRestorePreparationFile(context.Background(), work)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.file.Close(); e != nil {
		t.Fatal(e)
	} // Simulate an invalidated owned handle.
	if n, e := f.Write([]byte("x")); n != 0 || !errors.Is(e, os.ErrClosed) {
		t.Fatal("write native error lost", n, e)
	}
	if n, e := f.ReadAt(make([]byte, 1), 0); n != 0 || !errors.Is(e, os.ErrClosed) {
		t.Fatal("read native error lost", n, e)
	}
	if e = f.Sync(); !errors.Is(e, os.ErrClosed) {
		t.Fatal("sync native error lost", e)
	}
	_, native := f.file.Stat()
	var pathErr *os.PathError
	if !errors.As(native, &pathErr) {
		t.Fatal("fixture native error missing", native)
	}
	if n, e := f.Size(); n != 0 || !errors.Is(e, pathErr.Err) {
		t.Fatal("stat native error lost", n, e)
	}
	// This is not the ordinary Close path: its observation is still unconfirmed.
	if f.ReleaseResult().CloseAttempted {
		t.Fatal("I/O changed release state")
	}
	if e = f.Close(); !errors.Is(e, os.ErrClosed) || f.ReleaseResult().Confirmed() {
		t.Fatal("native error lost", e)
	}
}
