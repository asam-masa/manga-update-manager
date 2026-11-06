package platform

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

const backupDBBaselineMaxBytes int64 = 256 << 20

var errBackupDBBaseline = errors.New("backup DB cleanup baseline not established")

// A baseline is a content observation, not DB validity or deletion permission.
type backupDBBaseline struct {
	size int64
	hash [sha256.Size]byte
}

type backupDBObservation struct {
	id       BackupTempIdentity
	name     string
	size     int64
	modified windows.Filetime
}

// The owner must retain the creation handle and parents, and finish SQLite and
// all consumers first. This helper never closes, deletes or reopens the file.
// No context is accepted: cleanup must also work after operation cancellation.
func captureBackupDBBaseline(file *os.File, id BackupTempIdentity, name string) (backupDBBaseline, error) {
	return readBackupDBBaseline(file, id, name, func() (backupDBObservation, error) {
		return observeBackupDB(file)
	})
}

func readBackupDBBaseline(reader io.ReaderAt, id BackupTempIdentity, name string, observe func() (backupDBObservation, error)) (backupDBBaseline, error) {
	fail := func(err error) (backupDBBaseline, error) {
		return backupDBBaseline{}, errors.Join(errBackupDBBaseline, err)
	}
	before, err := observe()
	if err != nil {
		return fail(err)
	}
	if name == "" || before.id != id || !strings.EqualFold(before.name, name) || before.size < 0 || before.size > backupDBBaselineMaxBytes {
		return fail(nil)
	}
	h := sha256.New()
	n, err := io.Copy(h, io.NewSectionReader(reader, 0, before.size))
	if err != nil {
		return fail(err)
	}
	if n != before.size {
		return fail(io.ErrUnexpectedEOF)
	}
	after, err := observe()
	if err != nil {
		return fail(err)
	}
	if after != before {
		return fail(nil)
	}
	result := backupDBBaseline{size: n}
	copy(result.hash[:], h.Sum(nil))
	return result, nil
}

func observeBackupDB(file *os.File) (backupDBObservation, error) {
	var result backupDBObservation
	id, err := identifyBackupTemp(file) // Disk file, no reparse point, one link.
	if err != nil {
		return result, err
	}
	name, err := backupHandleName(file)
	if err != nil {
		return result, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return result, err
	}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if size > uint64(backupDBBaselineMaxBytes) {
		return result, errBackupDBBaseline
	}
	return backupDBObservation{id: id, name: name, size: int64(size), modified: info.LastWriteTime}, nil
}
