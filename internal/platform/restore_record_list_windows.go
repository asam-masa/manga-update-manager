package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type restoreRecordDirectoryEntry struct {
	Name       string
	Attributes uint32
	FileID     [16]byte
}

// walkRestoreRecordDirectory borrows a verified directory handle and uses its
// enumeration cursor exclusively for the duration of the walk. Entries are
// provisional until the walk returns nil. The caller must discard them on error.
func walkRestoreRecordDirectory(ctx context.Context, directory windows.Handle, visit func(restoreRecordDirectoryEntry)) error {
	return scanRestoreRecordDirectory(ctx, visit, func(class uint32, buffer []byte) error {
		return windows.GetFileInformationByHandleEx(directory, class, &buffer[0], uint32(len(buffer)))
	})
}

func scanRestoreRecordDirectory(ctx context.Context, visit func(restoreRecordDirectoryEntry), query func(uint32, []byte) error) error {
	const bufferSize = 16 * 1024
	// FILE_ID_EXTD_DIR_INFO buffers and successive entries are 8-byte aligned.
	var storage [bufferSize / 8]uint64
	buffer := unsafe.Slice((*byte)(unsafe.Pointer(&storage[0])), bufferSize)
	class := uint32(windows.FileIdExtdDirectoryRestartInfo)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		clear(buffer)
		err := query(class, buffer)
		if err == windows.ERROR_NO_MORE_FILES {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("enumerate restore record directory: %w", err)
		}
		if err := visitRestoreRecordDirectoryPage(ctx, buffer, visit); err != nil {
			return err
		}
		class = windows.FileIdExtdDirectoryInfo
	}
}

func visitRestoreRecordDirectoryPage(ctx context.Context, page []byte, visit func(restoreRecordDirectoryEntry)) error {
	// FILE_ID_EXTD_DIR_INFO offsets through the variable-length UTF-16 name.
	const headerSize = 88
	const attributesOffset = 56
	const nameLengthOffset = 60
	const fileIDOffset = 72
	invalid := errors.New("invalid restore record directory entry")
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(page) < headerSize {
			return invalid
		}
		next := binary.LittleEndian.Uint32(page)
		nameBytes := binary.LittleEndian.Uint32(page[nameLengthOffset:])
		if nameBytes == 0 || nameBytes%2 != 0 || nameBytes > uint32(len(page)-headerSize) {
			return invalid
		}
		if next != 0 && (next%8 != 0 || next < headerSize+nameBytes || next > uint32(len(page)-headerSize)) {
			return invalid
		}
		nameUnits := make([]uint16, nameBytes/2)
		for i := range nameUnits {
			nameUnits[i] = binary.LittleEndian.Uint16(page[headerSize+i*2:])
			if nameUnits[i] == 0 {
				return invalid
			}
		}
		name := windows.UTF16ToString(nameUnits)
		if name != "." && name != ".." {
			entry := restoreRecordDirectoryEntry{Name: name, Attributes: binary.LittleEndian.Uint32(page[attributesOffset:])}
			copy(entry.FileID[:], page[fileIDOffset:headerSize])
			visit(entry)
		}
		if next == 0 {
			return nil
		}
		page = page[next:]
	}
}
