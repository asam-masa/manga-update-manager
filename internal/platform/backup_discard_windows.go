package platform

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

var ErrBackupDiscard = errors.New("backup temporary cleanup failed")
var ErrBackupDiscardUnsafe = errors.New("backup temporary cleanup not authorized")
var ErrBackupDiscardAttempted = errors.New("backup temporary cleanup already attempted")

// These are observations, not permission to delete a later same-named file.
type BackupDiscardResult struct {
	TemporaryPath     string
	MarkedForDeletion bool
	Absent            bool
}

// Discard cleans only a retained, reverified, unpublished temporary file.
// It is independent of the original operation's cancelled context. Refusal or
// delete-API failure retains caller ownership: the caller must eventually Close
// and report release errors. A successful delete request closes/checks/releases
// once; failure never authorizes a pathname delete or a retry.
func (t *BackupTemp) Discard() (BackupDiscardResult, error) {
	return t.discard(backupDiscardOps{identifyBackupTemp, backupHandleName, markBackupTempDeleted, confirmBackupTempAbsent})
}

type backupDiscardOps struct {
	identity func(*os.File) (BackupTempIdentity, error)
	name     func(*os.File) (string, error)
	mark     func(*os.File) error
	absent   func(string) error
}

func (t *BackupTemp) discard(ops backupDiscardOps) (BackupDiscardResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.discardAttempted {
		return t.discardResult, errors.Join(ErrBackupDiscardAttempted, t.discardErr)
	}
	r := BackupDiscardResult{TemporaryPath: t.path}
	if t.closed || t.file == nil {
		return r, errors.Join(ErrBackupDiscard, os.ErrClosed)
	}
	if t.publishAttempted && t.publication.State != BackupPublishCollision {
		return r, errors.Join(ErrBackupDiscard, ErrBackupDiscardUnsafe)
	}
	id, err := ops.identity(t.file)
	if err != nil {
		return r, errors.Join(ErrBackupDiscard, err)
	}
	name, err := ops.name(t.file)
	if err != nil {
		return r, errors.Join(ErrBackupDiscard, err)
	}
	if id != t.identity || t.sourceName == "" || !strings.EqualFold(name, t.sourceName) {
		return r, errors.Join(ErrBackupDiscard, ErrBackupDiscardUnsafe)
	}
	t.discardAttempted = true
	err = ops.mark(t.file)
	if err == nil {
		r.MarkedForDeletion = true
		err = t.closeLocked(func() error {
			if e := ops.absent(t.sourceName); e != nil {
				return e
			}
			r.Absent = true
			return nil
		})
	}
	if err != nil {
		err = errors.Join(ErrBackupDiscard, err)
	}
	t.discardResult, t.discardErr = r, err
	return r, err
}

func markBackupTempDeleted(file *os.File) error {
	deleteFile := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfo, &deleteFile, 1)
}

func confirmBackupTempAbsent(name string) error {
	_, err := os.Lstat(name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrBackupDiscardUnsafe
}
