package platform

import (
	"errors"
	"os"
	"sync"
)

var errBackupDBDiscard = errors.New("backup DB cleanup failed")
var errBackupDBDiscardAttempted = errors.New("backup DB cleanup already attempted")

type backupDBDiscardResult struct {
	path            string
	marked          bool
	fileClosed      bool
	referenceClosed bool
	absent          bool
}

// Do not copy. Only acquireBackupDBCleanup creates a production owner.
// Parents remain owned by the caller until Discard/Close returns.
type backupDBCleanup struct {
	mu        sync.Mutex
	file      *os.File
	reference *os.File
	result    backupDBDiscardResult
	closed    bool
	attempted bool
	err       error
}

// Failure leaves reference with the caller. Success transfers both handles.
func acquireBackupDBCleanup(name string, reference *os.File, id BackupTempIdentity, baseline backupDBBaseline) (*backupDBCleanup, error) {
	file, err := reacquireBackupDB(name, reference, id, baseline)
	if err != nil {
		return nil, err
	}
	return &backupDBCleanup{file: file, reference: reference, result: backupDBDiscardResult{path: name}}, nil
}

func (c *backupDBCleanup) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked(func(f *os.File) error { return f.Close() })
}

func (c *backupDBCleanup) closeLocked(closeFile func(*os.File) error) error {
	if !c.closed {
		c.closed = true
		fileErr := closeFile(c.file)
		refErr := closeFile(c.reference)
		c.result.fileClosed = fileErr == nil
		c.result.referenceClosed = refErr == nil
		if err := errors.Join(fileErr, refErr); err != nil {
			c.err = errors.Join(c.err, errBackupDBDiscard, err)
		}
	}
	return c.err
}

func (c *backupDBCleanup) Discard() (backupDBDiscardResult, error) {
	return c.discard(markBackupTempDeleted, func(f *os.File) error { return f.Close() }, confirmBackupWorkDirectoryAbsent)
}

func (c *backupDBCleanup) discard(mark func(*os.File) error, closeFile func(*os.File) error, absent func(string) error) (backupDBDiscardResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.attempted {
		return c.result, errors.Join(errBackupDBDiscardAttempted, c.err)
	}
	if c.closed {
		return c.result, errors.Join(errBackupDBDiscard, os.ErrClosed, c.err)
	}
	c.attempted = true
	markErr := mark(c.file)
	c.result.marked = markErr == nil
	if markErr != nil {
		c.err = errors.Join(errBackupDBDiscard, markErr)
	}
	c.closeLocked(closeFile)
	if c.result.marked && c.result.fileClosed && c.result.referenceClosed {
		if err := absent(c.result.path); err != nil {
			c.err = errors.Join(c.err, errBackupDBDiscard, err)
		} else {
			c.result.absent = true
		}
	}
	return c.result, c.err
}
