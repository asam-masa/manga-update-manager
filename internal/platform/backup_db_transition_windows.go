package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var errBackupDBTransition = errors.New("backup DB cleanup transition failed")

type backupDBTransitionResult struct {
	path                    string
	baselineConfirmed       bool
	referenceVerified       bool
	referenceAcquired       bool
	dataClosed              bool
	referenceClosed         bool
	cleanupAcquisitionKnown bool
	cleanupAcquired         bool
	cleanupClosed           bool
}

type backupDBTransitionOps struct {
	baseline  func(*os.File, BackupTempIdentity, string) (backupDBBaseline, error)
	reference func(string) (*os.File, error)
	identity  func(*os.File) (BackupTempIdentity, error)
	close     func(*os.File) error
	acquire   func(string, *os.File, BackupTempIdentity, backupDBBaseline) (*backupDBCleanup, error)
}

// Consumes data on every outcome. The caller has finished SQLite and consumers
// and retains parents until cleanup ends. A successful owner must be finalized.
// Failure preserves artifacts; observations never authorize another path delete.
func transitionBackupDB(data *os.File, id BackupTempIdentity, name string) (*backupDBCleanup, backupDBTransitionResult, error) {
	return moveBackupDBToCleanup(data, id, name, backupDBTransitionOps{
		baseline: captureBackupDBBaseline, reference: openBackupDBReference,
		identity: identifyBackupTemp, close: func(f *os.File) error { return f.Close() },
		acquire: acquireBackupDBCleanup,
	})
}

func moveBackupDBToCleanup(data *os.File, id BackupTempIdentity, name string, ops backupDBTransitionOps) (owner *backupDBCleanup, result backupDBTransitionResult, err error) {
	result.path = name
	// No cleanup acquisition has been attempted yet.
	result.cleanupAcquisitionKnown = true
	var reference *os.File
	dataCloseAttempted := false
	defer func() {
		if !dataCloseAttempted {
			closeErr := ops.close(data)
			result.dataClosed = closeErr == nil
			err = errors.Join(err, closeErr)
		}
		if owner == nil && reference != nil {
			closeErr := ops.close(reference)
			result.referenceClosed = closeErr == nil
			err = errors.Join(err, closeErr)
		}
		if err != nil {
			err = errors.Join(errBackupDBTransition, err)
		}
	}()
	baseline, err := ops.baseline(data, id, name)
	if err != nil {
		return nil, result, err
	}
	result.baselineConfirmed = true
	reference, err = ops.reference(name)
	result.referenceAcquired = reference != nil
	if err != nil {
		return nil, result, err
	}
	refID, err := ops.identity(reference)
	if err != nil {
		return nil, result, err
	}
	if refID != id {
		return nil, result, errBackupDBTransition
	}
	result.referenceVerified = true
	dataCloseAttempted = true
	err = ops.close(data)
	result.dataClosed = err == nil
	if err != nil {
		return nil, result, err
	}
	result.cleanupAcquisitionKnown = false
	owner, err = ops.acquire(name, reference, id, baseline)
	if err != nil {
		var detail *backupDBReacquireError
		if errors.As(err, &detail) {
			result.cleanupAcquisitionKnown = true
			result.cleanupAcquired = detail.acquired
			result.cleanupClosed = detail.closed
		}
	} else if owner != nil {
		result.cleanupAcquisitionKnown = true
		result.cleanupAcquired = true
	}
	return owner, result, err
}

func openBackupDBReference(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}
