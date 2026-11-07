package platform

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackupDatabaseWorkPublicLifecycle(t *testing.T) {
	workDirectoryOps(t)
	root := workDirectoryRoot(t)
	w, err := CreateBackupDatabaseWork(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Cleanup()
	if err := w.VerifyIdentity(); err != nil {
		t.Fatal(err)
	}
	if size, err := w.Size(); err != nil || size != 0 {
		t.Fatal(size, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY); INSERT INTO sample VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("VACUUM INTO ?", w.Path()); err != nil {
		t.Fatal(err)
	}
	if err := w.VerifyIdentity(); err != nil {
		t.Fatal(err)
	}
	if size, err := w.Size(); err != nil || size <= 0 {
		t.Fatal(size, err)
	}
	header := make([]byte, 16)
	if n, err := w.ReadAt(header, 0); err != nil || n != 16 || string(header) != "SQLite format 3\x00" {
		t.Fatal(n, string(header), err)
	}
	result, err := w.Cleanup()
	if !result.Transition.ReferenceAcquired || !result.Transition.CleanupHandleAcquisitionKnown || !result.Transition.CleanupHandleAcquired || result.Transition.CleanupHandleClosed {
		t.Fatalf("transition ownership transfer not recorded: %+v", result.Transition)
	}
	if err != nil || !result.File.Absent || !result.Directory.Absent || !result.Transition.DataClosed || !result.File.DeleteHandleClosed || !result.File.ReferenceHandleClosed {
		t.Fatal(result, err)
	}
	if again, err := w.Cleanup(); err != nil || !reflect.DeepEqual(again, result) {
		t.Fatal(again, err)
	}
	if _, err := w.ReadAt(header, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := w.Size(); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	if err := w.VerifyIdentity(); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}

func TestBackupDatabaseWorkCleanupFailureIsCached(t *testing.T) {
	workDirectoryOps(t)
	w, err := CreateBackupDatabaseWork(context.Background(), workDirectoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Cleanup()
	// Adding a link makes ownership verification refuse cleanup. Both artifacts
	// must survive even though resources are released and the result is cached.
	if err := os.Link(w.Path(), w.Path()+"-link"); err != nil {
		t.Fatal(err)
	}
	result, err := w.Cleanup()
	if err == nil || result.File.Absent || result.DirectoryAttempted || !result.Transition.DataClosed {
		t.Fatal(result, err)
	}
	if _, statErr := os.Stat(result.File.Path); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(result.Directory.Path); statErr != nil {
		t.Fatal(statErr)
	}
	again, againErr := w.Cleanup()
	if !reflect.DeepEqual(again, result) || againErr != err {
		t.Fatal("cleanup retried/changed", again, againErr)
	}
}

func TestBackupDatabaseWorkEmptyAndCancelled(t *testing.T) {
	workDirectoryOps(t)
	root := workDirectoryRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if w, err := CreateBackupDatabaseWork(ctx, root); w != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(w, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	w, err := CreateBackupDatabaseWork(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := w.Cleanup(); err != nil || !r.File.Absent || !r.Directory.Absent {
		t.Fatal(r, err)
	}
}

func TestBackupDatabaseWorkCreationFailurePreservesDetails(t *testing.T) {
	work := outputWork(t)
	originalClose := work.closeFile
	releaseFault := errors.New("work release failed")
	work.closeFile = func() error { return errors.Join(originalClose(), releaseFault) }
	outputFault := errors.New("output verification failed")
	path := filepath.Join(work.Path(), "database.sqlite")
	w, err := assembleBackupDatabaseWork(context.Background(), work, func(context.Context, *BackupWorkDirectory) (*backupDBOutput, error) {
		if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
		return nil, &backupDBOutputError{path: path, created: true, err: outputFault}
	})
	var detail *BackupDatabaseWorkError
	if w != nil || !errors.As(err, &detail) || !detail.CreatedOutput || detail.OutputPath != path || detail.WorkPath != work.Path() || !errors.Is(err, outputFault) || !errors.Is(err, releaseFault) {
		t.Fatal(w, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "partial" {
		t.Fatal("artifact lost", err)
	}
}
