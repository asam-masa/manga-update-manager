package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"golang.org/x/sys/windows"
)

func TestRestoreReadbackHeldPreparationFile(t *testing.T) {
	ctx := context.Background()
	// Normalize only this synthetic fixture: CI TEMP may use an 8.3 alias.
	// Production keeps rejecting names that differ from the retained handle.
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var pathBuf [32768]uint16
	n, nameErr := windows.GetFinalPathNameByHandle(windows.Handle(dir.Fd()), &pathBuf[0], uint32(len(pathBuf)), 0)
	if err := errors.Join(nameErr, dir.Close()); err != nil || n == 0 || n >= uint32(len(pathBuf)) {
		t.Fatal(n, err)
	}
	root := strings.TrimPrefix(windows.UTF16ToString(pathBuf[:n]), `\\?\`)
	p, err := platform.CreateRestorePreparation(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Cleanup(nil); err != nil {
			t.Error(err)
		}
	})
	f, err := p.CreateFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("synthetic restore output"), 2048)
	if n, err := f.Write(data); n != len(data) || err != nil {
		t.Fatal(n, err)
	}
	if err := VerifyRestorePreparationFile(ctx, f, int64(len(data)), sha256.Sum256(data)); err != nil {
		t.Fatal(err)
	}
	if f.ReleaseResult().CloseAttempted {
		t.Fatal("borrowed file released")
	}
	// Sequential write position is unchanged by readback. The file remains owned.
	if n, err := f.Write([]byte("!")); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if size, err := f.Size(); size != int64(len(data)+1) || err != nil {
		t.Fatal(size, err)
	}
	buf := make([]byte, len(data)+1)
	if n, err := f.ReadAt(buf, 0); n != len(buf) || err != nil || !bytes.Equal(buf, append(bytes.Clone(data), '!')) {
		t.Fatal(n, err)
	}
	if _, err := os.Stat(f.Path()); err != nil {
		t.Fatal("borrowed file removed", err)
	}
}
