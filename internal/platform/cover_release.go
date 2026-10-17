package platform

import (
	"errors"
	"io"
)

// ErrCoverRelease means resource release could not be confirmed. It does not
// prove a leak or authorize retrying Close. Keep raw diagnostics out of the UI.
var ErrCoverRelease = errors.New("cover resource release unconfirmed")

func coverReleaseError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrCoverRelease, err)
}

func closeCoverResource(resource io.Closer) error {
	return coverReleaseError(resource.Close())
}
