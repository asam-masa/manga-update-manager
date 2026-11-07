package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var errBackupDBReacquire = errors.New("backup DB cleanup handle not verified")

// Presence of this error confirms the candidate acquisition observation, not
// release of the caller's reference/data/parents or permission to delete a path.
type backupDBReacquireError struct {
	acquired bool
	closed   bool
	err      error
}

func (e *backupDBReacquireError) Error() string { return e.err.Error() }
func (e *backupDBReacquireError) Unwrap() error { return e.err }

type backupDBReacquireOps struct {
	open     func(string) (*os.File, error)
	identity func(*os.File) (BackupTempIdentity, error)
	baseline func(*os.File, BackupTempIdentity, string) (backupDBBaseline, error)
	close    func(*os.File) error
}

// The owner retains reference and parents throughout, after completing SQLite,
// consumers and data-handle Close. Baseline and ID come from the creation handle.
// Success transfers the newly opened handle only; this helper never deletes.
func reacquireBackupDB(name string, reference *os.File, id BackupTempIdentity, baseline backupDBBaseline) (*os.File, error) {
	return verifyBackupDBReacquisition(name, reference, id, baseline, backupDBReacquireOps{
		open: openBackupDBForCleanup, identity: identifyBackupTemp,
		baseline: captureBackupDBBaseline, close: func(f *os.File) error { return f.Close() },
	})
}

func verifyBackupDBReacquisition(name string, reference *os.File, id BackupTempIdentity, baseline backupDBBaseline, ops backupDBReacquireOps) (result *os.File, err error) {
	var file *os.File
	defer func() {
		if err != nil {
			var closeErr error
			if file != nil {
				closeErr = ops.close(file)
			}
			err = &backupDBReacquireError{acquired: file != nil, closed: file != nil && closeErr == nil, err: errors.Join(errBackupDBReacquire, err, closeErr)}
		}
	}()
	if baseline.size < 0 || baseline.size > backupDBBaselineMaxBytes {
		return nil, errBackupDBReacquire
	}
	refID, err := ops.identity(reference)
	if err != nil || refID != id {
		return nil, errors.Join(errBackupDBReacquire, err)
	}
	file, err = ops.open(name)
	if err != nil {
		return nil, errors.Join(errBackupDBReacquire, err)
	}
	actual, err := ops.baseline(file, id, name)
	if err != nil {
		return nil, err
	}
	if actual != baseline {
		return nil, errBackupDBReacquire
	}
	refID, err = ops.identity(reference)
	if err != nil || refID != id {
		return nil, errors.Join(errBackupDBReacquire, err)
	}
	return file, nil
}

func openBackupDBForCleanup(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.DELETE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}
