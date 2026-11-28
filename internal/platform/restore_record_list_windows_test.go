package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWalkRestoreRecordDirectory(t *testing.T) {
	path := t.TempDir()
	const itemCount = 240 // More than one native buffer of directory entries.
	for i := range itemCount {
		name := fmt.Sprintf("unrelated-%03d.txt", i)
		if i == 0 {
			name = "plan.json"
		}
		if err := os.WriteFile(filepath.Join(path, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory := openRestoreTestDirectory(t, path)
	plan := requireRestoreTestChild(t, directory, "plan.json")
	var identity BackupWorkDirectoryIdentity
	if err := windows.GetFileInformationByHandleEx(plan, windows.FileIdInfo, (*byte)(unsafe.Pointer(&identity)), uint32(unsafe.Sizeof(identity))); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		entries := make(map[string]restoreRecordDirectoryEntry)
		err := walkRestoreRecordDirectory(context.Background(), directory, func(entry restoreRecordDirectoryEntry) {
			if _, duplicate := entries[entry.Name]; duplicate {
				t.Errorf("duplicate entry %q", entry.Name)
			}
			entries[entry.Name] = entry
		})
		if err != nil {
			t.Fatal(err)
		}
		entry, found := entries["plan.json"]
		if len(entries) != itemCount || !found || entry.FileID != identity.FileID ||
			entry.Attributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
			t.Fatalf("count=%d, plan=%+v, expected ID=%x", len(entries), entry, identity.FileID)
		}
	}
}

func TestScanRestoreRecordDirectoryIncomplete(t *testing.T) {
	failure := errors.New("synthetic enumeration failure")
	for _, followup := range []string{"query failure", "malformed page", "cancel at end"} {
		t.Run(followup, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls, entries int
			err := scanRestoreRecordDirectory(ctx, func(restoreRecordDirectoryEntry) { entries++ }, func(class uint32, buffer []byte) error {
				calls++
				if calls == 1 {
					if class != windows.FileIdExtdDirectoryRestartInfo {
						t.Fatalf("first class %d", class)
					}
					binary.LittleEndian.PutUint32(buffer[60:], 2)
					binary.LittleEndian.PutUint16(buffer[88:], 'x')
					return nil
				}
				if calls != 2 || class != windows.FileIdExtdDirectoryInfo {
					t.Fatalf("continuation %d, class %d", calls, class)
				}
				switch followup {
				case "query failure":
					return failure
				case "malformed page":
					binary.LittleEndian.PutUint32(buffer[60:], 1)
					return nil
				default:
					cancel()
					return windows.ERROR_NO_MORE_FILES
				}
			})
			if err == nil || entries != 1 || calls != 2 ||
				followup == "query failure" && !errors.Is(err, failure) ||
				followup == "cancel at end" && !errors.Is(err, context.Canceled) {
				t.Fatalf("err=%v entries=%d calls=%d", err, entries, calls)
			}
		})
	}
}
