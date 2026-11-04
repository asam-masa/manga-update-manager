package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ErrBackupPublishAttempted = errors.New("backup publication already attempted")
var ErrBackupPublishUnknown = errors.New("backup publication result unknown")
var ErrBackupPublishIdentity = errors.New("backup publication identity or name mismatch")

type BackupPublicationState uint8

const (
	BackupPublishNotAttempted BackupPublicationState = iota
	BackupPublishCollision
	BackupPublishConfirmed
	BackupPublishUnknown
)

// Paths are observations/candidates, never authority to reopen or remove them.
// Confirmed records the rename observation, not Sync/Close or durability.
type BackupPublication struct {
	State         BackupPublicationState
	TemporaryPath string
	FinalPath     string
}

// PublishNoReplace requires successful archive preparation and exclusive caller
// ownership. It retains file/parents for result handling; it never closes,
// deletes or retries. After one native attempt, Write and further attempts fail.
func (t *BackupTemp) PublishNoReplace(ctx context.Context) (BackupPublication, error) {
	return t.publishNoReplace(ctx, backupPublicationOps{backupHandleName, identifyBackupTemp, renameBackupNoReplace})
}

type backupPublicationOps struct {
	name     func(*os.File) (string, error)
	identity func(*os.File) (BackupTempIdentity, error)
	rename   func(*os.File, string) error
}

func (t *BackupTemp) publishNoReplace(ctx context.Context, ops backupPublicationOps) (BackupPublication, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.publishAttempted {
		return t.publication, errors.Join(ErrBackupPublishAttempted, t.publishErr)
	}
	r := BackupPublication{TemporaryPath: t.path}
	if t.sourceName != "" && t.finalBase != "" {
		r.FinalPath = filepath.Join(filepath.Dir(t.sourceName), t.finalBase)
	}
	if t.discardAttempted {
		return r, ErrBackupDiscardAttempted
	}
	if t.closed || t.file == nil {
		return r, os.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	id, err := ops.identity(t.file)
	if err != nil {
		return r, err
	}
	name, err := ops.name(t.file)
	if err != nil {
		return r, err
	}
	if id != t.identity || !strings.EqualFold(name, t.sourceName) {
		return r, ErrBackupPublishIdentity
	}
	if _, err := ParseBackupDestination(r.FinalPath); err != nil {
		return r, err
	}
	if strings.EqualFold(r.FinalPath, t.sourceName) {
		return r, ErrInvalidBackupDestination
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	t.publishAttempted = true
	r.State = BackupPublishUnknown
	err = ops.rename(t.file, r.FinalPath)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			r.State = BackupPublishCollision
		} else {
			err = errors.Join(ErrBackupPublishUnknown, err)
		}
	} else {
		id, idErr := ops.identity(t.file)
		name, nameErr := ops.name(t.file)
		if idErr != nil || nameErr != nil || id != t.identity || !strings.EqualFold(name, r.FinalPath) {
			err = errors.Join(ErrBackupPublishUnknown, ErrBackupPublishIdentity, idErr, nameErr)
		} else {
			r.State = BackupPublishConfirmed
		}
	}
	t.publication, t.publishErr = r, err
	return r, err // A late cancellation cannot undo an attempted publication.
}

// Use the normalized DOS spelling of the held file, not a guessed conversion of
// the caller's path. Unsupported namespaces/failed lookup are rejected.
func backupHandleName(file *os.File) (string, error) {
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n == 0 || n >= uint32(len(buf)) {
		return "", ErrInvalidBackupDestination
	}
	name := strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
	if _, err := ParseBackupDestination(name); err != nil {
		return "", err
	}
	return name, nil
}

func renameBackupNoReplace(file *os.File, target string) error {
	name, err := windows.UTF16FromString(target)
	if err != nil {
		return err
	}
	type renameInfo struct {
		Flags  uint32
		Root   windows.Handle
		Length uint32
		Name   [1]uint16
	}
	var header renameInfo
	offset := unsafe.Offsetof(header.Name)
	buf := make([]byte, int(offset)+2*len(name))
	info := (*renameInfo)(unsafe.Pointer(&buf[0]))
	info.Length = uint32(2 * (len(name) - 1)) // bytes, excluding trailing NUL
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[offset])), len(name)), name)
	// Zero flags/Root: ReplaceIfExists=FALSE with an absolute target.
	return windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileRenameInfo, &buf[0], uint32(len(buf)))
}
