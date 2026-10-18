package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// DeleteVerifiedCover requires a ready, unused DB record and the caller's
// reference exclusion. It verifies and deletes using the same exclusive handle.
// Nil means the fixed path was confirmed absent, not proof of DB cleanup.
func DeleteVerifiedCover(ctx context.Context, directory, id string, expected CoverFileInfo) (err error) {
	if len(id) != 32 || expected.ByteSize <= 0 {
		return ErrInvalidCoverFile
	}
	for _, b := range []byte(id) {
		if !(b >= '0' && b <= '9') && !(b >= 'a' && b <= 'f') {
			return ErrInvalidCoverFile
		}
	}
	guard, err := PrepareCoverDirectory(ctx, directory)
	if err != nil {
		return err
	}
	var file *os.File
	defer func() {
		if file != nil {
			err = errors.Join(err, closeCoverResource(file))
		}
		err = errors.Join(err, guard.Close())
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	name := filepath.Join(directory, "covers", id+".png")
	file, err = openCoverFile(name, windows.GENERIC_READ|windows.DELETE, windows.OPEN_EXISTING)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return errors.Join(confirmCoverAbsent(name), ctx.Err())
	}
	if err != nil {
		return err
	}
	actual, err := captureCoverFile(file, expected.ByteSize)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrInvalidCoverFile
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deleteFile := byte(1)
	if err := windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfo, &deleteFile, 1); err != nil {
		return err
	}
	err = closeCoverResource(file)
	file = nil // Never retry Close on a possibly reused handle.
	if err != nil {
		return err
	}
	return errors.Join(confirmCoverAbsent(name), ctx.Err())
}

// Only the exact fixed file's absence qualifies. Path-not-found, access denied,
// delete-pending and all other errors are not successful cleanup.
func confirmCoverAbsent(name string) error {
	_, err := os.Lstat(name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrInvalidCoverFile
}
