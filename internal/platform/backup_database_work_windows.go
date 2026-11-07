package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type BackupDatabaseTransitionResult struct {
	BaselineConfirmed             bool
	ReferenceVerified             bool
	DataClosed                    bool
	ReferenceClosed               bool
	ReferenceAcquired             bool
	CleanupHandleAcquisitionKnown bool
	CleanupHandleAcquired         bool
	CleanupHandleClosed           bool
}

type BackupDatabaseFileCleanupResult struct {
	Path                  string
	MarkedForDeletion     bool
	DeleteHandleClosed    bool
	ReferenceHandleClosed bool
	Absent                bool
}

// Observations only; none authorize deleting later same-named objects or resuming
// application operations. Transition and finalization describe distinct phases;
// finalization closes the handles transferred by a successful transition.
type BackupDatabaseCleanupResult struct {
	Transition          BackupDatabaseTransitionResult
	File                BackupDatabaseFileCleanupResult
	Directory           BackupWorkDirectoryDiscardResult
	DirectoryAttempted  bool
	FileCloseError      error
	DirectoryCloseError error
}

// Work was created, but output ownership was not returned. CreatedOutput=false
// does not prove absence. Artifacts are preserved, never guessed safe to delete.
type BackupDatabaseWorkError struct {
	WorkPath      string
	OutputPath    string
	CreatedOutput bool
	Err           error
}

func (e *BackupDatabaseWorkError) Error() string {
	return fmt.Sprintf("prepare backup DB work %q: %v", e.WorkPath, e.Err)
}
func (e *BackupDatabaseWorkError) Unwrap() error { return e.Err }

// Do not copy. The owner calls Cleanup only after SQLite and all consumers have
// finished. Read/verify/cleanup serialize; a path is an observation, not ownership.
type BackupDatabaseWork struct {
	mu       sync.Mutex
	work     *BackupWorkDirectory
	output   *backupDBOutput
	finished bool
	result   BackupDatabaseCleanupResult
	err      error
}

func CreateBackupDatabaseWork(ctx context.Context, directory string) (*BackupDatabaseWork, error) {
	work, err := CreateBackupWorkDirectory(ctx, directory)
	if err != nil {
		return nil, err
	}
	return assembleBackupDatabaseWork(ctx, work, createBackupDBOutput)
}

func assembleBackupDatabaseWork(ctx context.Context, work *BackupWorkDirectory, createOutput func(context.Context, *BackupWorkDirectory) (*backupDBOutput, error)) (*BackupDatabaseWork, error) {
	out, err := createOutput(ctx, work)
	if err != nil {
		detail := &BackupDatabaseWorkError{WorkPath: work.Path(), OutputPath: filepath.Join(work.Path(), "database.sqlite")}
		var outputErr *backupDBOutputError
		if errors.As(err, &outputErr) {
			detail.CreatedOutput = outputErr.created
		}
		detail.Err = errors.Join(err, work.Close())
		return nil, detail
	}
	return &BackupDatabaseWork{work: work, output: out}, nil
}

func (w *BackupDatabaseWork) Path() string { return w.output.path }

func (w *BackupDatabaseWork) observeLocked() (backupDBObservation, error) {
	if w.finished {
		return backupDBObservation{}, os.ErrClosed
	}
	if err := w.work.VerifyIdentity(); err != nil {
		return backupDBObservation{}, err
	}
	o, err := observeBackupDB(w.output.file)
	if err != nil {
		return o, err
	}
	if o.id != w.output.id || !strings.EqualFold(o.name, w.output.path) {
		return backupDBObservation{}, errBackupDBBaseline
	}
	return o, nil
}

func (w *BackupDatabaseWork) VerifyIdentity() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.observeLocked()
	return err
}
func (w *BackupDatabaseWork) Size() (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	o, err := w.observeLocked()
	return o.size, err
}
func (w *BackupDatabaseWork) ReadAt(p []byte, offset int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return 0, os.ErrClosed
	}
	return w.output.file.ReadAt(p, offset)
}

// Cleanup ignores operation cancellation, caches every result, and never retries
// native actions. The caller preserves the original copy/validation/use outcome.
func (w *BackupDatabaseWork) Cleanup() (BackupDatabaseCleanupResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return w.result, w.err
	}
	w.finished = true
	owner, tr, err := transitionBackupDB(w.output.file, w.output.id, w.output.path)
	w.result.Transition = publicBackupDatabaseTransition(tr)
	w.result.File.Path = w.output.path
	w.result.Directory.Path = w.work.Path()
	if err != nil {
		w.result.DirectoryCloseError = w.work.Close()
		w.err = errors.Join(err, w.result.DirectoryCloseError)
		return w.result, w.err
	}
	final, err := finalizeBackupDBWork(owner, w.work)
	w.result.File = BackupDatabaseFileCleanupResult{final.file.path, final.file.marked, final.file.fileClosed, final.file.referenceClosed, final.file.absent}
	w.result.Directory = final.directory
	w.result.DirectoryAttempted = final.directoryAttempted
	w.result.FileCloseError = final.fileCloseError
	w.result.DirectoryCloseError = final.directoryCloseError
	w.err = err
	return w.result, w.err
}

func publicBackupDatabaseTransition(tr backupDBTransitionResult) BackupDatabaseTransitionResult {
	return BackupDatabaseTransitionResult{
		BaselineConfirmed: tr.baselineConfirmed, ReferenceVerified: tr.referenceVerified,
		DataClosed: tr.dataClosed, ReferenceClosed: tr.referenceClosed, ReferenceAcquired: tr.referenceAcquired,
		CleanupHandleAcquisitionKnown: tr.cleanupAcquisitionKnown, CleanupHandleAcquired: tr.cleanupAcquired, CleanupHandleClosed: tr.cleanupClosed,
	}
}
