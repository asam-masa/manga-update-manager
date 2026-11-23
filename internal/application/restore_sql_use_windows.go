package application

import "errors"

var (
	ErrPreparedRestoreSQLInUse              = errors.New("prepared restore is in SQL use")
	ErrPreparedRestoreSQLReleaseUnconfirmed = errors.New("prepared restore SQL release is unconfirmed")
	ErrPreparedRestoreCleanupStarted        = errors.New("prepared restore cleanup has started")
	ErrPreparedRestoreSQLUseEnded           = errors.New("prepared restore SQL use has ended")
)

// PreparedRestoreSQLUse is a single SQL/VFS borrowing reservation. Do not
// copy. The future SQL session must retain this token until all of its SQL,
// VFS, and view releases have been observed.
type PreparedRestoreSQLUse struct {
	owner *PreparedRestore
	ended bool
}

// BeginSQLUse reserves the prepared files before any SQL or VFS resource is
// acquired. At most one reservation is supported by the initial restore path.
func (p *PreparedRestore) BeginSQLUse() (*PreparedRestoreSQLUse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cleanupStarted {
		return nil, ErrPreparedRestoreCleanupStarted
	}
	if p.sqlReleaseErr != nil {
		return nil, errors.Join(ErrPreparedRestoreSQLReleaseUnconfirmed, p.sqlReleaseErr)
	}
	if p.sqlUse != nil {
		return nil, ErrPreparedRestoreSQLInUse
	}
	use := &PreparedRestoreSQLUse{owner: p}
	p.sqlUse = use
	return use, nil
}

// End records whether every resource depending on this reservation was
// released. An unconfirmed result is sticky for this PreparedRestore and keeps
// its native files and parent retained. A repeated or stale End cannot release
// another reservation.
func (u *PreparedRestoreSQLUse) End(confirmed bool, cause error) error {
	if u == nil || u.owner == nil {
		return ErrPreparedRestoreSQLUseEnded
	}
	p := u.owner
	p.mu.Lock()
	defer p.mu.Unlock()
	if u.ended || p.sqlUse != u {
		return ErrPreparedRestoreSQLUseEnded
	}
	u.ended = true
	p.sqlUse = nil
	if !confirmed {
		p.sqlReleaseErr = errors.Join(ErrPreparedRestoreSQLReleaseUnconfirmed, cause)
		return p.sqlReleaseErr
	}
	return nil
}
