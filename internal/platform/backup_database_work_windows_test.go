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
	if !result.ReleaseConfirmed() {
		t.Fatalf("release not confirmed: %+v / %v", result, err)
	}
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
	if !result.ReleaseConfirmed() {
		t.Fatalf("refused deletion confused with release: %+v / %v", result, err)
	}
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
	} else {
		var detail *BackupDatabaseWorkError
		if !errors.As(err, &detail) || !detail.ReleaseConfirmed() || detail.OutputAcquired {
			t.Fatal("missing creation refusal release", err)
		}
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
	if detail.ReleaseConfirmed() || detail.DirectoryRelease.HandleClosed || !detail.DirectoryRelease.Parents.Confirmed() {
		t.Fatal("work Close failure lost", detail.DirectoryRelease)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "partial" {
		t.Fatal("artifact lost", err)
	}
}

func TestBackupDatabaseWorkCreationReleaseObservations(t *testing.T) {
	for _, mode := range []string{"unacquired", "released", "unreleased", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			work := outputWork(t)
			cause := errors.New("synthetic output failure")
			_, err := assembleBackupDatabaseWork(context.Background(), work, func(context.Context, *BackupWorkDirectory) (*backupDBOutput, error) {
				if mode == "unknown" {
					return nil, cause
				}
				return nil, &backupDBOutputError{created: mode != "unacquired", acquired: mode != "unacquired", closed: mode == "released", err: cause}
			})
			var detail *BackupDatabaseWorkError
			if !errors.Is(err, cause) || !errors.As(err, &detail) || !detail.DirectoryRelease.Confirmed() || detail.OutputAcquisitionKnown != (mode != "unknown") || detail.OutputAcquired != (mode == "released" || mode == "unreleased") || detail.OutputClosed != (mode == "released") || detail.ReleaseConfirmed() != (mode == "unacquired" || mode == "released") {
				t.Fatalf("wrong creation release observation: %+v / %v", detail, err)
			}
		})
	}
}

func TestBackupDatabaseWorkRetainsUnexpectedChildButReleases(t *testing.T) {
	workDirectoryOps(t)
	w, err := CreateBackupDatabaseWork(context.Background(), workDirectoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Cleanup()
	child := filepath.Join(w.work.Path(), "unexpected.txt")
	if err := os.WriteFile(child, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := w.Cleanup()
	if err == nil || !r.ReleaseConfirmed() || !r.File.Absent || r.Directory.Absent || !r.DirectoryAttempted {
		t.Fatal("retained artifact confused with release failure", r, err)
	}
	if b, err := os.ReadFile(child); err != nil || string(b) != "preserve" {
		t.Fatal("unexpected child modified", string(b), err)
	}
	if again, againErr := w.Cleanup(); againErr != err || !reflect.DeepEqual(r, again) {
		t.Fatal("cleanup result changed", again, againErr)
	}
}

func TestBackupDatabaseReleaseConfirmationRequiresEveryResource(t *testing.T) {
	dir := BackupWorkDirectoryReleaseResult{HandleAcquired: true, HandleClosed: true, CloseAttempted: true, Parents: BackupDirectoryReleaseResult{Acquired: 2, Released: 2, CloseAttempted: true}}
	safe := BackupDatabaseCleanupResult{
		Transition: BackupDatabaseTransitionResult{DataClosed: true, ReferenceAcquired: true, CleanupHandleAcquisitionKnown: true, CleanupHandleAcquired: true},
		File:       BackupDatabaseFileCleanupResult{DeleteHandleClosed: true, ReferenceHandleClosed: true}, DirectoryRelease: dir,
	}
	if !safe.ReleaseConfirmed() {
		t.Fatal("release must not require deletion/absence success")
	}
	for _, tc := range []struct {
		name   string
		change func(*BackupDatabaseCleanupResult)
	}{
		{"data", func(r *BackupDatabaseCleanupResult) { r.Transition.DataClosed = false }},
		{"reference", func(r *BackupDatabaseCleanupResult) { r.File.ReferenceHandleClosed = false }},
		{"candidate", func(r *BackupDatabaseCleanupResult) { r.File.DeleteHandleClosed = false }},
		{"acquisition-unknown", func(r *BackupDatabaseCleanupResult) { r.Transition.CleanupHandleAcquisitionKnown = false }},
		{"work", func(r *BackupDatabaseCleanupResult) { r.DirectoryRelease.HandleClosed = false }},
		{"parent", func(r *BackupDatabaseCleanupResult) { r.DirectoryRelease.Parents.Released-- }},
		{"parents-unconfirmed", func(r *BackupDatabaseCleanupResult) { r.DirectoryRelease.Parents.CloseAttempted = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := safe
			tc.change(&r)
			if r.ReleaseConfirmed() {
				t.Fatal("missing release accepted", r)
			}
		})
	}
	// A failed transition can close its own candidate/reference before finalization.
	r := safe
	r.File = BackupDatabaseFileCleanupResult{}
	r.Transition.ReferenceClosed, r.Transition.CleanupHandleClosed = true, true
	if !r.ReleaseConfirmed() {
		t.Fatal("transition release not recognized", r)
	}
	r.Transition.ReferenceAcquired, r.Transition.ReferenceClosed = false, false
	r.Transition.CleanupHandleAcquired, r.Transition.CleanupHandleClosed = false, false
	if !r.ReleaseConfirmed() {
		t.Fatal("known unacquired handles require no Close", r)
	}
	if (BackupDatabaseCleanupResult{}).ReleaseConfirmed() || (&BackupDatabaseWorkError{}).ReleaseConfirmed() {
		t.Fatal("zero value confirms release")
	}
}
