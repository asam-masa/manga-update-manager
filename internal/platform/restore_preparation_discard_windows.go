package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrRestorePreparationDiscard = errors.New("restore preparation cleanup failed")
var ErrRestorePreparationDiscardUnsafe = errors.New("restore preparation cleanup not authorized")
var ErrRestorePreparationDiscardAttempted = errors.New("restore preparation cleanup already attempted")

// Observations about this retained child only, not a later same-named object.
type RestorePreparationDiscardResult struct {
	Path              string
	MarkedForDeletion bool
	Absent            bool
}

// Discard is only for preparation before retirement/switching. The caller must
// finish all consumers and retain the original parent. No context is accepted:
// cancellation must not skip cleanup. Refusal/mark failure retains ownership;
// caller must Close. Never retries, deletes by path, or releases the parent.
func (f *RestorePreparationFile) Discard() (RestorePreparationDiscardResult, error) {
	return f.discard(backupDiscardOps{identifyBackupTemp, backupHandleName, markBackupTempDeleted, confirmBackupTempAbsent})
}

func (f *RestorePreparationFile) discard(ops backupDiscardOps) (RestorePreparationDiscardResult, error) {
	if f == nil {
		return RestorePreparationDiscardResult{}, errors.Join(ErrRestorePreparationDiscard, os.ErrClosed)
	}
	work := f.work // Immutable after creation; ownership is borrowed, not a lease.
	if work == nil {
		return RestorePreparationDiscardResult{}, errors.Join(ErrRestorePreparationDiscard, ErrRestorePreparationDiscardUnsafe)
	}
	// Same order as construction: parent before child. Close only locks child.
	work.mu.Lock()
	defer work.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.discardAttempted {
		return f.discardResult, errors.Join(ErrRestorePreparationDiscardAttempted, f.discardErr)
	}
	r := RestorePreparationDiscardResult{Path: f.path}
	fail := func(err error) (RestorePreparationDiscardResult, error) {
		return r, errors.Join(ErrRestorePreparationDiscard, err)
	}
	if f.release.CloseAttempted || f.file == nil || work.closed || work.file == nil {
		return fail(os.ErrClosed)
	}
	id, err := identifyBackupWorkDirectory(work.file)
	if err != nil {
		return fail(err)
	}
	name, err := backupHandleName(work.file)
	if err != nil {
		return fail(err)
	}
	if id != work.identity || !strings.EqualFold(name, work.path) || !strings.EqualFold(filepath.Dir(f.path), work.path) {
		return fail(ErrRestorePreparationDiscardUnsafe)
	}
	childID, err := ops.identity(f.file)
	if err != nil {
		return fail(err)
	}
	childName, err := ops.name(f.file)
	if err != nil {
		return fail(err)
	}
	if childID != f.identity || f.path == "" || !strings.EqualFold(childName, f.path) {
		return fail(ErrRestorePreparationDiscardUnsafe)
	}
	f.discardAttempted = true
	err = ops.mark(f.file)
	if err == nil {
		r.MarkedForDeletion = true
		err = f.closeLocked()
		if err == nil {
			err = ops.absent(f.path)
			r.Absent = err == nil
		}
	}
	if err != nil {
		err = errors.Join(ErrRestorePreparationDiscard, err)
	}
	f.discardResult, f.discardErr = r, err
	return r, err
}
