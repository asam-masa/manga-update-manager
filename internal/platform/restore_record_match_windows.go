package platform

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errRestoreRecordMismatch = errors.New("restore record directory entry and open result do not match")

// matchRestoreRecordChild borrows a verified parent for one reconciliation.
// The caller must reserve exclusive use of the parent's enumeration cursor.
// On success the caller owns the returned handle, or gets a confirmed absence
// for this pass. The caller still has to recheck the full record observation.
func matchRestoreRecordChild(ctx context.Context, parent windows.Handle, name string) (windows.Handle, bool, error) {
	var entry restoreRecordDirectoryEntry
	matches := 0
	err := walkRestoreRecordDirectory(ctx, parent, func(candidate restoreRecordDirectoryEntry) {
		if strings.EqualFold(candidate.Name, name) {
			entry = candidate
			matches++
		}
	})
	if err != nil {
		return 0, false, err
	}
	if matches > 1 || matches == 1 && !matchesRestoreRecordName(entry, name) {
		return 0, false, fmt.Errorf("%s: %w", name, errRestoreRecordMismatch)
	}
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	child, missing, err := openRestoreRecordChild(parent, name)
	if err != nil {
		return 0, false, err
	}
	if missing {
		if matches != 0 {
			return 0, false, fmt.Errorf("%s: %w", name, errRestoreRecordMismatch)
		}
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		return 0, true, nil
	}
	if matches == 0 {
		err = fmt.Errorf("%s: %w", name, errRestoreRecordMismatch)
	} else {
		err = matchOpenRestoreRecordChild(parent, child, entry)
	}
	if err = errors.Join(err, ctx.Err()); err != nil {
		return 0, false, errors.Join(err, windows.CloseHandle(child))
	}
	return child, false, nil
}

func matchesRestoreRecordName(entry restoreRecordDirectoryEntry, name string) bool {
	directory := entry.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	return entry.Name == name && directory == (name == "restore-active") &&
		entry.Attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0
}

func matchOpenRestoreRecordChild(parent, child windows.Handle, entry restoreRecordDirectoryEntry) error {
	parentID, err := restoreRecordHandleIdentity(parent)
	if err != nil {
		return err
	}
	childID, err := restoreRecordHandleIdentity(child)
	if err != nil {
		return err
	}
	path, err := backupHandlePath(child)
	if err != nil {
		return err
	}
	if filepath.Base(path) != entry.Name || childID.VolumeSerial != parentID.VolumeSerial || childID.FileID != entry.FileID {
		return fmt.Errorf("%s: %w", entry.Name, errRestoreRecordMismatch)
	}
	return nil
}

func restoreRecordHandleIdentity(handle windows.Handle) (BackupWorkDirectoryIdentity, error) {
	var id BackupWorkDirectoryIdentity
	err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id)))
	return id, err
}
