package application

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
)

func TestPrepareBackupArchiveOnHeldWindowsFile(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "backup.zip")
	temp, err := platform.CreateBackupTemp(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := temp.Close(); err != nil {
			t.Error(err)
		}
	})
	manifest, data := backupPrepareFixture(t)
	m, err := PrepareBackupArchive(context.Background(), temp, manifest, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	if err != nil || m.Database.Size != int64(len(data)) {
		t.Fatalf("prepare %#v %v", m, err)
	}
	if err := temp.VerifyIdentity(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("final created: %v", err)
	}
	if err := os.Rename(dir, dir+"-moved"); err == nil {
		t.Fatal("parent released during preparation")
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(temp.Path())
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	_, verifyErr := backup.ValidateArchive(f, info.Size())
	closeErr := f.Close()
	if verifyErr != nil || closeErr != nil {
		t.Fatalf("closed file validation: %v %v", verifyErr, closeErr)
	}
}

func TestPrepareThenPublishBackupArchive(t *testing.T) {
	final := filepath.Join(t.TempDir(), "保存.zip")
	temp, err := platform.CreateBackupTemp(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := temp.Close(); err != nil {
			t.Error(err)
		}
	})
	manifest, data := backupPrepareFixture(t)
	_, err = PrepareBackupArchive(context.Background(), temp, manifest, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	if err != nil {
		t.Fatal(err)
	}
	r, err := temp.PublishNoReplace(context.Background())
	if err != nil || r.State != platform.BackupPublishConfirmed {
		t.Fatalf("publish %#v %v", r, err)
	}
	if err := temp.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(final)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	m, verifyErr := backup.ValidateArchive(f, info.Size())
	closeErr := f.Close()
	if verifyErr != nil || closeErr != nil || m.Database.Size != int64(len(data)) {
		t.Fatalf("final validation %#v %v %v", m, verifyErr, closeErr)
	}
}
