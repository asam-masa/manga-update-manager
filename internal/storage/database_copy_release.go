package storage

import "errors"

// ErrDatabaseCopyRelease means copied-DB read resources were not confirmed
// released. Rows.Err can include implicit Close failure, so ambiguous iteration
// errors are conservative failures too. This is not proof of a leak or permission
// to retry Close/delete. The caller must not resume normal admission on this mark.
var ErrDatabaseCopyRelease = errors.New("database copy read resource release unconfirmed")

func databaseCopyReleaseError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrDatabaseCopyRelease, err)
}
