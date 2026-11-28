package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// These fixtures use the actual preparation handle but bypass its owner only
// to observe the native rename. Product transfer of ownership is separate.
func TestRestoreSwitchHeldFileIdentity(t *testing.T) {
	const payload = "synthetic image bytes"
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
	if n, err := f.Write([]byte(payload)); err != nil || n != len(payload) {
		t.Fatal(n, err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	before, err := observeBackupDB(f.file)
	if err != nil || before.id != f.Identity() {
		t.Fatal(before, err)
	}

	target := filepath.Join(filepath.Dir(work.Path()), "placed-image.png")
	if err := renameBackupNoReplace(f.file, target); err != nil {
		t.Fatal(err)
	}
	after, err := observeBackupDB(f.file)
	if err != nil || before.id != after.id || !strings.EqualFold(after.name, target) || after.size != int64(len(payload)) {
		t.Fatal(after, err)
	}
	if err := f.Close(); err != nil || !f.ReleaseResult().Confirmed() {
		t.Fatal(f.ReleaseResult(), err)
	}
	if _, err := os.Lstat(before.name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old pathname still present", err)
	}
	if observed, err := observeRestoreFixturePath(target); err != nil || observed.id != before.id {
		t.Fatal(observed, err)
	}

	// Keep the genuine file present, so its ID cannot be recycled when the
	// same bytes are written into a different file at the expected pathname.
	parked := filepath.Join(filepath.Dir(target), "retained-genuine-image.png")
	if err := os.Rename(target, parked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	for path, wantSame := range map[string]bool{target: false, parked: true} {
		observed, err := observeRestoreFixturePath(path)
		if err != nil || (observed.id == before.id) != wantSame {
			t.Fatal(observed, wantSame, err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != payload {
			t.Fatal("fixture contents differ", err)
		}
	}
}

func TestRestoreSwitchHeldFileCollisionPreservesIdentities(t *testing.T) {
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
	target := filepath.Join(filepath.Dir(work.Path()), "occupied-image.png")
	if err := os.WriteFile(target, []byte("occupied fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	existing, err := observeRestoreFixturePath(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := renameBackupNoReplace(f.file, target); !errors.Is(err, windows.ERROR_FILE_EXISTS) && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		t.Fatal("expected native collision", err)
	}
	source, err := observeBackupDB(f.file)
	if err != nil || source.id != f.Identity() || !strings.EqualFold(source.name, f.Path()) {
		t.Fatal(source, err)
	}
	if after, err := observeRestoreFixturePath(target); err != nil || after.id != existing.id {
		t.Fatal(after, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "occupied fixture" {
		t.Fatal("occupied payload changed", err)
	}
}

func observeRestoreFixturePath(name string) (backupDBObservation, error) {
	file, err := os.Open(name)
	if err != nil {
		return backupDBObservation{}, err
	}
	observation, err := observeBackupDB(file)
	return observation, errors.Join(err, file.Close())
}
