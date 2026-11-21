package platform

import (
	"context"
	"errors"
	"os"
	"sync"
)

var ErrRestorePreparationCleanup = errors.New("restore preparation cleanup incomplete")
var ErrRestorePreparationCleanupAttempted = errors.New("restore preparation cleanup already attempted")

type RestorePreparationChildCleanup struct {
	Path    string
	Discard RestorePreparationDiscardResult
	Release RestorePreparationFileRelease
	Err     error
}

// OriginalErr is separate from cleanup errors. Release confirmation does not
// grant application admission. Paths describe observations, not delete rights.
type RestorePreparationCleanupResult struct {
	OriginalErr     error
	Children        []RestorePreparationChildCleanup
	DeletionStopped bool
	Parent          BackupWorkDirectoryDiscardResult
	ParentRelease   BackupWorkDirectoryReleaseResult
	ParentErr       error
}

// Do not copy. This owner creates its own parent and records every child it
// creates. There is no path-list/handle adoption API. Callers may borrow child
// I/O but must finish all consumers before Cleanup, and cannot reuse this owner.
type RestorePreparation struct {
	mu       sync.Mutex
	work     *BackupWorkDirectory
	children []restorePreparationChild
	finished bool
	result   RestorePreparationCleanupResult
	err      error
}

type restorePreparationChild struct {
	file   *RestorePreparationFile
	failed RestorePreparationChildCleanup
}

func CreateRestorePreparation(ctx context.Context, directory string) (*RestorePreparation, error) {
	work, err := CreateBackupWorkDirectory(ctx, directory)
	if err != nil {
		return nil, err // Existing creation error retains parent release evidence.
	}
	return &RestorePreparation{work: work}, nil
}

func (p *RestorePreparation) CreateFile(ctx context.Context) (*RestorePreparationFile, error) {
	return p.createFile(ctx, CreateRestorePreparationFile)
}

func (p *RestorePreparation) createFile(ctx context.Context, create func(context.Context, *BackupWorkDirectory) (*RestorePreparationFile, error)) (*RestorePreparationFile, error) {
	if p == nil {
		return nil, os.ErrClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished || p.work == nil {
		return nil, os.ErrClosed
	}
	f, err := create(ctx, p.work)
	if err == nil {
		p.children = append(p.children, restorePreparationChild{file: f})
	} else {
		var detail *RestorePreparationFileError
		if errors.As(err, &detail) && detail.Path == "" && detail.Release.Confirmed() {
			return nil, err // Failure before a child pathname/handle was acquired.
		}
		failed := RestorePreparationChildCleanup{Err: err}
		if detail != nil {
			failed.Path, failed.Release = detail.Path, detail.Release
		}
		// No retained child ownership was returned: never delete by this path.
		p.children = append(p.children, restorePreparationChild{failed: failed})
	}
	return f, err
}

type restorePreparationCleanupOps struct {
	verifyParent  func(*BackupWorkDirectory) error
	discardChild  func(*RestorePreparationFile) (RestorePreparationDiscardResult, error)
	discardParent func(*BackupWorkDirectory) (BackupWorkDirectoryDiscardResult, error)
}

func restorePreparationCleanupOperations() restorePreparationCleanupOps {
	return restorePreparationCleanupOps{
		(*BackupWorkDirectory).VerifyIdentity,
		(*RestorePreparationFile).Discard,
		(*BackupWorkDirectory).DiscardEmpty,
	}
}

// Cleanup is only for preparation before retirement/switching. It finishes even
// after cancellation, without retrying native operations. Parent uncertainty is
// latched: later recovery cannot resume deletion. Releases are still attempted.
func (p *RestorePreparation) Cleanup(originalErr error) (RestorePreparationCleanupResult, error) {
	return p.cleanup(originalErr, restorePreparationCleanupOperations())
}

func (p *RestorePreparation) cleanup(originalErr error, ops restorePreparationCleanupOps) (RestorePreparationCleanupResult, error) {
	if p == nil {
		return RestorePreparationCleanupResult{OriginalErr: originalErr}, os.ErrClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	copyResult := func() RestorePreparationCleanupResult {
		r := p.result
		r.Children = append([]RestorePreparationChildCleanup(nil), r.Children...)
		return r
	}
	if p.finished {
		return copyResult(), errors.Join(ErrRestorePreparationCleanupAttempted, p.err)
	}
	p.finished = true
	p.result.OriginalErr = originalErr
	if p.work == nil {
		p.err = os.ErrClosed
		return copyResult(), p.err
	}
	r := &p.result
	r.Parent.Path = p.work.Path()
	checkParent := func() bool {
		if !r.DeletionStopped {
			if err := ops.verifyParent(p.work); err != nil {
				r.DeletionStopped = true
				r.ParentErr = errors.Join(r.ParentErr, ErrRestorePreparationParentUnconfirmed, err)
			}
		}
		return !r.DeletionStopped
	}
	allAbsent := true
	var cleanupErrors []error
	for _, child := range p.children {
		c := child.failed
		if child.file != nil {
			f := child.file
			c.Path = f.Path()
			if checkParent() {
				c.Discard, c.Err = ops.discardChild(f)
				if errors.Is(c.Err, ErrRestorePreparationParentUnconfirmed) {
					r.DeletionStopped = true
					r.ParentErr = errors.Join(r.ParentErr, c.Err)
				}
			} else {
				c.Err = ErrRestorePreparationParentUnconfirmed
			}
			c.Err = errors.Join(c.Err, f.Close())
			c.Release = f.ReleaseResult()
		}
		allAbsent = allAbsent && c.Discard.Absent && c.Release.Confirmed()
		r.Children = append(r.Children, c)
		if c.Err != nil {
			cleanupErrors = append(cleanupErrors, c.Err)
		}
	}
	if checkParent() && allAbsent {
		r.Parent, r.ParentErr = ops.discardParent(p.work)
	} else {
		r.ParentErr = errors.Join(r.ParentErr, ErrRestorePreparationCleanup)
	}
	// DiscardEmpty may already have attempted Close; Close caches that outcome.
	r.ParentErr = errors.Join(r.ParentErr, p.work.Close())
	r.ParentRelease = p.work.ReleaseResult()
	p.err = errors.Join(append(cleanupErrors, r.ParentErr)...)
	if p.err != nil {
		p.err = errors.Join(ErrRestorePreparationCleanup, p.err)
	}
	return copyResult(), p.err
}
