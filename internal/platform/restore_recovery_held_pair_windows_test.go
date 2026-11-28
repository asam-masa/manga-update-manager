package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

type recoveryFixtureMove struct {
	from, to string
	id       BackupTempIdentity
}

var errRecoveryFixtureRelease = errors.New("recovery fixture handle release failed")

// Fixture only: retain both directory guards and all checked handles through
// the move loop. The count reports observed moves, not an atomic outcome.
func moveRecoveryFixturePair(root string, pair [2]recoveryFixtureMove) (completed int, resultErr error) {
	ctx := context.Background()
	lock, err := AcquireStorageLock(root)
	if err != nil {
		return 0, err
	}
	recordRelease := func(err error) {
		if err != nil {
			resultErr = errors.Join(resultErr, errRecoveryFixtureRelease, err)
		}
	}
	defer func() { recordRelease(lock.Close()) }()
	// The two fixture sources share a parent, as do the two destinations.
	for _, path := range []string{pair[0].from, pair[0].to} {
		guard, err := HoldBackupDirectory(ctx, path)
		if err != nil {
			return 0, err
		}
		defer func() { recordRelease(guard.Close()) }()
	}
	var files [2]*os.File
	for i, candidate := range pair {
		path, err := windows.UTF16PtrFromString(candidate.from)
		if err != nil {
			return 0, err
		}
		handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return 0, err
		}
		file := os.NewFile(uintptr(handle), candidate.from)
		if file == nil {
			return 0, errors.Join(errRecoveryFixtureIdentity, windows.CloseHandle(handle))
		}
		files[i] = file
		defer func() { recordRelease(file.Close()) }()
		actual, err := observeRestoreSource(file)
		if err != nil {
			return 0, err
		}
		if actual.id != candidate.id || !strings.EqualFold(actual.name, candidate.from) {
			return 0, errRecoveryFixtureIdentity
		}
	}
	for i, candidate := range pair {
		if err := renameBackupNoReplace(files[i], candidate.to); err != nil {
			return completed, err
		}
		actual, err := observeRestoreSource(files[i])
		if err != nil {
			return completed, err
		}
		if actual.id != candidate.id || !strings.EqualFold(actual.name, candidate.to) {
			return completed, errRecoveryFixtureIdentity
		}
		completed++
	}
	return completed, nil
}

func TestRecoveryFixtureMovesVerifiedPairOnHeldHandles(t *testing.T) {
	for _, mode := range []string{"success", "second identity mismatch", "second destination occupied"} {
		t.Run(mode, func(t *testing.T) {
			root := workDirectoryRoot(t)
			from, to := filepath.Join(root, "retired"), filepath.Join(root, "live")
			for _, dir := range []string{from, to} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			var pair [2]recoveryFixtureMove
			var replacementID, occupiedID BackupTempIdentity
			payloads := [2]string{"synthetic database", "synthetic image"}
			for i, name := range []string{"db.sqlite", "image.png"} {
				pair[i] = recoveryFixtureMove{from: filepath.Join(from, name), to: filepath.Join(to, name)}
				if err := os.WriteFile(pair[i].from, []byte(payloads[i]), 0600); err != nil {
					t.Fatal(err)
				}
				observation, err := observeRestoreFixturePath(pair[i].from)
				if err != nil {
					t.Fatal(err)
				}
				pair[i].id = observation.id
			}
			if mode == "second identity mismatch" {
				if err := os.Rename(pair[1].from, pair[1].from+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pair[1].from, []byte(payloads[1]), 0600); err != nil {
					t.Fatal(err)
				}
				replacement, err := observeRestoreFixturePath(pair[1].from)
				if err != nil || replacement.id == pair[1].id {
					t.Fatal("expected a different fixture identity", err)
				}
				replacementID = replacement.id
			}
			if mode == "second destination occupied" {
				if err := os.WriteFile(pair[1].to, []byte("occupied"), 0600); err != nil {
					t.Fatal(err)
				}
				occupied, err := observeRestoreFixturePath(pair[1].to)
				if err != nil {
					t.Fatal(err)
				}
				occupiedID = occupied.id
			}
			completed, err := moveRecoveryFixturePair(root, pair)
			if errors.Is(err, errRecoveryFixtureRelease) {
				t.Fatal(err)
			}
			want := 2
			switch mode {
			case "second identity mismatch":
				want = 0
				if !errors.Is(err, errRecoveryFixtureIdentity) {
					t.Fatal(err)
				}
				assertRecoveryFixtureBytes(t, pair[1].from+".original", payloads[1])
				assertRecoveryFixtureID(t, pair[1].from+".original", pair[1].id)
			case "second destination occupied":
				want = 1
				if !errors.Is(err, windows.ERROR_FILE_EXISTS) && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
					t.Fatal(err)
				}
				assertRecoveryFixtureBytes(t, pair[1].to, "occupied")
				assertRecoveryFixtureID(t, pair[1].to, occupiedID)
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			if completed != want {
				t.Fatal(completed, want)
			}
			for i, candidate := range pair {
				if i < want {
					if _, err := os.Lstat(candidate.from); !errors.Is(err, os.ErrNotExist) {
						t.Fatal(err)
					}
					assertRecoveryFixtureID(t, candidate.to, candidate.id)
					assertRecoveryFixtureBytes(t, candidate.to, payloads[i])
				} else {
					assertRecoveryFixtureBytes(t, candidate.from, payloads[i])
					wantID := candidate.id
					if mode == "second identity mismatch" && i == 1 {
						wantID = replacementID
					}
					assertRecoveryFixtureID(t, candidate.from, wantID)
					if mode != "second destination occupied" {
						if _, err := os.Lstat(candidate.to); !errors.Is(err, os.ErrNotExist) {
							t.Fatal(err)
						}
					}
				}
			}
			reacquired, err := AcquireStorageLock(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := reacquired.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertRecoveryFixtureID(t *testing.T, path string, want BackupTempIdentity) {
	t.Helper()
	got, err := observeRestoreFixturePath(path)
	if err != nil || got.id != want {
		t.Fatal("unexpected fixture identity", path, got.id, want, err)
	}
}

func assertRecoveryFixtureBytes(t *testing.T, path, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil || string(body) != want {
		t.Fatal("unexpected fixture body", path, err)
	}
}
