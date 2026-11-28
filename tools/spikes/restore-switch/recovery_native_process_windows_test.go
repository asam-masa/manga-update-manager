package restoreswitch

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func observeNativeFixture(root, name string) (token fixtureIdentity, found bool, err error) {
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return token, false, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return token, false, errors.Join(errRecoveryStop, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return token, false, err
	}
	var id struct {
		VolumeSerial uint64
		FileID       [16]byte
	}
	err = windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()), windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id)))
	if err := errors.Join(err, file.Close()); err != nil {
		return token, false, err
	}
	if id.FileID == [16]byte{} {
		return token, false, errRecoveryStop
	}
	binary.LittleEndian.PutUint64(token[:8], id.VolumeSerial)
	copy(token[8:], id.FileID[:])
	return token, true, nil
}

func nativeRecoveryFixturePlan(t *testing.T, root string) recoveryPlan {
	t.Helper()
	get := func(name string) fixtureIdentity {
		t.Helper()
		token, found, err := observeNativeFixture(root, name)
		if err != nil || !found {
			t.Fatal(name, found, err)
		}
		return token
	}
	newDB := get(stagedDB)
	// These cases stop before management updates. This identity does not
	// establish whether a later SQL transaction committed.
	return recoveryPlan{Version: 1, ID: "synthetic-native-restore",
		OldDB: get(liveDB), OldImage: get(liveImage), NewDB: newDB, UpdatedDB: newDB, NewImage: get(stagedImage)}
}

func runNativeRecoveryFixture(t *testing.T, root string, reject bool) {
	t.Helper()
	for i := 0; i <= 4; i++ {
		outcome, moves, err := decideRecoveryWithIdentity(root, observeNativeFixture)
		if reject {
			if !errors.Is(err, errRecoveryStop) || len(moves) != 0 {
				t.Fatal(outcome, moves, err)
			}
			return
		}
		if outcome != "restore old" || err != nil {
			t.Fatal(outcome, moves, err)
		}
		if len(moves) == 0 {
			return
		}
		move(t, root, moves[0].from, moves[0].to)
	}
	t.Fatal("native fixture recovery did not converge")
}

func TestNativeIdentityAfterSwitchProcessKilled(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		replace    bool
	}{
		{"same files", "recover-native", false},
		{"same bytes different file", "recover-native-rejected", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := canonicalRecoveryTestDirectory(t, recoveryFixture(t, 0))
			write(t, root, recoveryProcessFixture, recoveryProcessFixture)
			if err := os.Mkdir(filepath.Join(root, "records"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			killRecoveryFixtureAtCheckpoint(t, ctx, root, "place-image-native", "new-image-placed")
			if tc.replace {
				move(t, root, liveImage, "retained-original")
				write(t, root, liveImage, "new-image")
			}
			if output, err := recoveryProcessCommand(t, ctx, root, tc.mode).CombinedOutput(); err != nil {
				t.Fatal(string(output), err)
			}
			if tc.replace {
				content(t, root, "retained-original", "new-image")
				content(t, root, liveImage, "new-image")
				content(t, root, retiredDB, "old-db")
				content(t, root, retiredImage, "old-image")
				content(t, root, stagedDB, "new-db")
				if _, err := os.Lstat(filepath.Join(root, liveDB)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("stopped recovery changed live DB", err)
				}
				return
			}
			content(t, root, liveDB, "old-db")
			content(t, root, liveImage, "old-image")
			content(t, root, stagedDB, "new-db")
			content(t, root, stagedImage, "new-image")
		})
	}
}
