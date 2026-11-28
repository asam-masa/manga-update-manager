package platform

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openRestoreRecordChild borrows a verified directory handle. A successful
// result transfers ownership of the child to the caller. A missing name is
// one observation to reconcile with enumeration, not proof of absence.
func openRestoreRecordChild(parent windows.Handle, name string) (child windows.Handle, missing bool, err error) {
	var directory bool
	switch name {
	case "restore-active":
		directory = true
	case "plan.json", "complete.json":
	default:
		return 0, false, fmt.Errorf("invalid restore record name %q", name)
	}
	if parent == 0 || parent == windows.InvalidHandle {
		return 0, false, windows.ERROR_INVALID_HANDLE
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, false, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_DONT_REPARSE,
	}
	var ioStatus windows.IO_STATUS_BLOCK
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, windows.FILE_READ_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		&attributes, &ioStatus, nil, 0, windows.FILE_SHARE_READ, windows.FILE_OPEN,
		windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if err == windows.STATUS_OBJECT_NAME_NOT_FOUND {
		return 0, true, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("open restore record child %q: %w", name, err)
	}
	if err := checkRestoreRecordChild(handle, directory); err != nil {
		return 0, false, errors.Join(fmt.Errorf("check restore record child %q: %w", name, err), windows.CloseHandle(handle))
	}
	return handle, false, nil
}

func checkRestoreRecordChild(handle windows.Handle, directory bool) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	kind, err := windows.GetFileType(handle)
	if err != nil {
		return err
	}
	isDirectory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if kind != windows.FILE_TYPE_DISK || isDirectory != directory ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrUnsafeBackupDirectory
	}
	return nil
}
