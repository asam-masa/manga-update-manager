package platform

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

var ErrBackupWorkDirectoryDiscard = errors.New("backup work directory cleanup failed")
var ErrBackupWorkDirectoryDiscardUnsafe = errors.New("backup work directory cleanup not authorized")
var ErrBackupWorkDirectoryDiscardAttempted = errors.New("backup work directory cleanup already attempted")

// These are observations, not permission to delete a later same-named object.
type BackupWorkDirectoryDiscardResult struct {
	Path              string
	MarkedForDeletion bool
	Absent            bool
}

// DiscardEmpty checks the retained directory, never enumerates/removes children,
// and relies on NTFS's same-handle disposition to reject a nonempty directory.
// Refusal/native failure retains ownership; the caller must eventually Close.
// After a native attempt it cannot retry. No cancelled context controls cleanup.
func (d *BackupWorkDirectory) DiscardEmpty() (BackupWorkDirectoryDiscardResult, error) {
	return d.discardEmpty(backupWorkDiscardOps{identifyBackupWorkDirectory, backupHandleName, markBackupTempDeleted, confirmBackupWorkDirectoryAbsent})
}

type backupWorkDiscardOps struct {
	identity func(*os.File) (BackupWorkDirectoryIdentity, error)
	name     func(*os.File) (string, error)
	mark     func(*os.File) error
	absent   func(string) error
}

func (d *BackupWorkDirectory) discardEmpty(ops backupWorkDiscardOps) (BackupWorkDirectoryDiscardResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.discardAttempted {
		return d.discardResult, errors.Join(ErrBackupWorkDirectoryDiscardAttempted, d.discardErr)
	}
	r := BackupWorkDirectoryDiscardResult{Path: d.path}
	if d.closed || d.file == nil {
		return r, errors.Join(ErrBackupWorkDirectoryDiscard, os.ErrClosed)
	}
	id, err := ops.identity(d.file)
	if err != nil {
		return r, errors.Join(ErrBackupWorkDirectoryDiscard, err)
	}
	name, err := ops.name(d.file)
	if err != nil {
		return r, errors.Join(ErrBackupWorkDirectoryDiscard, err)
	}
	if id != d.identity || d.path == "" || !strings.EqualFold(name, d.path) {
		return r, errors.Join(ErrBackupWorkDirectoryDiscard, ErrBackupWorkDirectoryDiscardUnsafe)
	}
	d.discardAttempted = true
	err = ops.mark(d.file)
	if err == nil {
		r.MarkedForDeletion = true
		err = d.closeLocked(func() error {
			if err := ops.absent(d.path); err != nil {
				return err
			}
			r.Absent = true
			return nil
		})
	}
	if err != nil {
		err = errors.Join(ErrBackupWorkDirectoryDiscard, err)
	}
	d.discardResult, d.discardErr = r, err
	return r, err
}

func confirmBackupWorkDirectoryAbsent(name string) error {
	_, err := os.Lstat(name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrBackupWorkDirectoryDiscardUnsafe
}
