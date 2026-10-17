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

var ErrInvalidCoverFile = errors.New("invalid or unverified cover file")

// CoverFileInfo is verification data, not permission to delete a file.
// FileID preserves the Windows FILE_ID_128 byte order.
type CoverFileInfo struct {
	VolumeSerial uint64
	FileID       [16]byte
	ByteSize     int64
	SHA256       [32]byte
}

type coverFileIdentity struct {
	VolumeSerial uint64
	FileID       [16]byte
}

// WriteNewCover writes a reserved ID's generated PNG bytes without overwriting.
// The caller must reserve the ID and finish thumbnail.Generate first. This
// boundary does not decode PNG, alter DB state, or delete failed output.
func WriteNewCover(ctx context.Context, directory, id string, data []byte) (result CoverFileInfo, err error) {
	if len(id) != 32 || len(data) == 0 {
		return result, ErrInvalidCoverFile
	}
	for _, b := range []byte(id) {
		if !(b >= '0' && b <= '9') && !(b >= 'a' && b <= 'f') {
			return result, ErrInvalidCoverFile
		}
	}
	guard, err := PrepareCoverDirectory(ctx, directory)
	if err != nil {
		return result, err
	}
	var file *os.File
	defer func() {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
		err = errors.Join(err, guard.Close())
		if err != nil {
			result = CoverFileInfo{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	name := filepath.Join(directory, "covers", id+".png")
	file, err = openCoverFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.CREATE_NEW)
	if err != nil {
		return result, err
	}
	if err := writeAndSyncCover(ctx, file, data); err != nil {
		return result, err
	}
	expected, err := captureCoverFile(file, int64(len(data)))
	if err != nil {
		return result, err
	}
	if expected.SHA256 != sha256.Sum256(data) {
		return result, ErrInvalidCoverFile
	}
	err = file.Close()
	file = nil // Do not retry Close on a possibly reused OS handle.
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	file, err = openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
	if err != nil {
		return result, err
	}
	actual, err := captureCoverFile(file, expected.ByteSize)
	if err != nil {
		return result, err
	}
	if actual != expected {
		return result, ErrInvalidCoverFile
	}
	err = file.Close()
	file = nil
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return actual, nil
}

func openCoverFile(name string, access, disposition uint32) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, access, 0, nil, disposition, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

// This consumer-side seam permits short-write and Sync failure tests without
// filling a real drive. File ownership and Close remain with WriteNewCover.
type coverWriteSyncer interface {
	Write([]byte) (int, error)
	Sync() error
}

func writeAndSyncCover(ctx context.Context, file coverWriteSyncer, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := file.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return ctx.Err()
}

func captureCoverFile(file *os.File, expectedSize int64) (CoverFileInfo, error) {
	h := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return CoverFileInfo{}, err
	}
	kind, err := windows.GetFileType(h)
	if err != nil {
		return CoverFileInfo{}, err
	}
	if kind != windows.FILE_TYPE_DISK || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return CoverFileInfo{}, ErrInvalidCoverFile
	}
	var identity coverFileIdentity
	if err := windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&identity)), uint32(unsafe.Sizeof(identity))); err != nil {
		return CoverFileInfo{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return CoverFileInfo{}, err
	}
	if expectedSize <= 0 || stat.Size() != expectedSize {
		return CoverFileInfo{}, ErrInvalidCoverFile
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return CoverFileInfo{}, err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, expectedSize))
	if err != nil {
		return CoverFileInfo{}, err
	}
	if n != expectedSize {
		return CoverFileInfo{}, io.ErrUnexpectedEOF
	}
	result := CoverFileInfo{VolumeSerial: identity.VolumeSerial, FileID: identity.FileID, ByteSize: n}
	copy(result.SHA256[:], hash.Sum(nil))
	return result, nil
}
