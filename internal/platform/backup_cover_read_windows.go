package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

type BackupCoverReadStatus string

const (
	BackupCoverPresent BackupCoverReadStatus = "present"
	BackupCoverMissing BackupCoverReadStatus = "missing"
	BackupCoverChanged BackupCoverReadStatus = "changed"
)

type BackupCoverReadResult struct {
	Status BackupCoverReadStatus
	Data   []byte
}

// ReadBackupCover observes one ready record while the caller retains reference
// exclusion. maxBytes comes from the caller's backup format policy. It does not
// validate PNG, authorize deletion, or replace verification during ZIP writing.
// Only an absent child of verified held parents is Missing. Changed requires a
// safe bounded read of the same physical file; unsafe/unreadable is an error.
func ReadBackupCover(ctx context.Context, directory, id string, expected CoverFileInfo, maxBytes int64) (BackupCoverReadResult, error) {
	return readBackupCover(ctx, directory, id, expected, maxBytes, closeCoverResource)
}

func readBackupCover(ctx context.Context, directory, id string, expected CoverFileInfo, maxBytes int64, release func(io.Closer) error) (result BackupCoverReadResult, err error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(id) != 32 || maxBytes <= 0 || maxBytes == 1<<63-1 || expected.ByteSize <= 0 || expected.ByteSize > maxBytes {
		return result, ErrInvalidCoverFile
	}
	for _, c := range []byte(id) {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return result, ErrInvalidCoverFile
		}
	}
	guard, err := holdCoverDirectory(ctx, directory, false)
	if err != nil {
		return result, err
	}
	var file *os.File
	defer func() {
		if file != nil {
			err = errors.Join(err, release(file))
		}
		err = errors.Join(err, release(guard), ctx.Err())
		if err != nil {
			result = BackupCoverReadResult{}
		}
	}()
	file, err = openCoverFile(filepath.Join(directory, "covers", id+".png"), windows.GENERIC_READ, windows.OPEN_EXISTING)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return BackupCoverReadResult{Status: BackupCoverMissing}, nil
	}
	if err != nil {
		return result, err
	}
	h := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return result, err
	}
	kind, err := windows.GetFileType(h)
	if err != nil {
		return result, err
	}
	if kind != windows.FILE_TYPE_DISK || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return result, ErrInvalidCoverFile
	}
	var identity coverFileIdentity
	if err := windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&identity)), uint32(unsafe.Sizeof(identity))); err != nil {
		return result, err
	}
	if identity.VolumeSerial != expected.VolumeSerial || identity.FileID != expected.FileID {
		return result, ErrInvalidCoverFile
	}
	stat, err := file.Stat()
	if err != nil {
		return result, err
	}
	if stat.Size() < 0 || stat.Size() > maxBytes {
		return result, ErrInvalidCoverFile
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	data, err := io.ReadAll(io.LimitReader(file, stat.Size()+1))
	if err != nil {
		return result, err
	}
	if int64(len(data)) != stat.Size() {
		return result, io.ErrUnexpectedEOF
	}
	if int64(len(data)) != expected.ByteSize || sha256.Sum256(data) != expected.SHA256 {
		return BackupCoverReadResult{Status: BackupCoverChanged}, nil
	}
	return BackupCoverReadResult{Status: BackupCoverPresent, Data: data}, nil
}
