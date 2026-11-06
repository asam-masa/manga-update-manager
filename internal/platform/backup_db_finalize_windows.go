package platform

import (
	"errors"
	"path/filepath"
	"strings"
)

var errBackupDBFinalize = errors.New("backup DB work cleanup failed")

type backupDBFinalizeResult struct {
	file                backupDBDiscardResult
	directory           BackupWorkDirectoryDiscardResult
	directoryAttempted  bool
	fileCloseError      error
	directoryCloseError error
}

type backupDBFinalizeOps struct {
	verify           func() error
	discardFile      func() (backupDBDiscardResult, error)
	closeFile        func() error
	discardDirectory func() (BackupWorkDirectoryDiscardResult, error)
	closeDirectory   func() error
}

// Consumes both owners. Do not use either concurrently or after this call.
// Results describe cleanup only, never backup success or permission to resume.
func finalizeBackupDBWork(owner *backupDBCleanup, work *BackupWorkDirectory) (backupDBFinalizeResult, error) {
	result, err := finalizeBackupDB(backupDBFinalizeOps{
		verify: func() error {
			if !strings.EqualFold(filepath.Dir(owner.result.path), work.Path()) {
				return ErrBackupWorkDirectoryIdentity
			}
			return work.VerifyIdentity()
		},
		discardFile: owner.Discard, closeFile: owner.Close,
		discardDirectory: work.DiscardEmpty, closeDirectory: work.Close,
	})
	// Close-only refusal paths also update the retained release observations.
	result.file = owner.result
	result.directory.Path = work.Path()
	return result, err
}

func finalizeBackupDB(ops backupDBFinalizeOps) (result backupDBFinalizeResult, err error) {
	defer func() {
		// Cached Close after Discard does not retry native I/O. Refusal before
		// Discard still releases the file before releasing its parents.
		result.fileCloseError = ops.closeFile()
		result.directoryCloseError = ops.closeDirectory()
		err = errors.Join(err, result.fileCloseError, result.directoryCloseError)
		if err != nil {
			err = errors.Join(errBackupDBFinalize, err)
		}
	}()
	if err = ops.verify(); err != nil {
		return result, err
	}
	result.file, err = ops.discardFile()
	if err != nil {
		return result, err
	}
	if !result.file.absent {
		return result, errBackupDBFinalize
	}
	result.directoryAttempted = true
	result.directory, err = ops.discardDirectory()
	return result, err
}
