package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

type backupDBOutput struct {
	file *os.File
	id   BackupTempIdentity
	path string
}

// Created is an observation, not deletion permission. False is not proof of absence.
type backupDBOutputError struct {
	path     string
	created  bool
	acquired bool
	closed   bool
	err      error
}

func (e *backupDBOutputError) Error() string {
	return fmt.Sprintf("create backup DB output %q: %v", e.path, e.err)
}
func (e *backupDBOutputError) Unwrap() error { return e.err }

// The caller owns work throughout. Success transfers the data handle; finish
// SQLite and consumers before passing it to transitionBackupDB, or Close it.
func createBackupDBOutput(ctx context.Context, work *BackupWorkDirectory) (*backupDBOutput, error) {
	return prepareBackupDBOutput(ctx, work, createBackupDBOutputHandle, observeBackupDB)
}

func prepareBackupDBOutput(ctx context.Context, work *BackupWorkDirectory, create func(string) (*os.File, error), observe func(*os.File) (backupDBObservation, error)) (output *backupDBOutput, err error) {
	name := ""
	created := false
	var file *os.File
	defer func() {
		if err != nil {
			closed := false
			if file != nil {
				closeErr := file.Close()
				closed = closeErr == nil
				err = errors.Join(err, closeErr)
			}
			err = &backupDBOutputError{path: name, created: created, acquired: file != nil, closed: closed, err: err}
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = work.VerifyIdentity(); err != nil {
		return nil, err
	}
	name = filepath.Join(work.Path(), "database.sqlite")
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	file, err = create(name)
	if err != nil {
		return nil, err
	}
	created = true
	observation, err := observe(file)
	if err != nil {
		return nil, err
	}
	if observation.size != 0 || !strings.EqualFold(observation.name, name) {
		return nil, errBackupDBBaseline
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &backupDBOutput{file: file, id: observation.id, path: name}, nil
}

func createBackupDBOutputHandle(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.CREATE_NEW, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}
