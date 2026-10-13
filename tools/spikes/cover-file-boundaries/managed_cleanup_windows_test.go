package coverfileboundaries

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Single-threaded probe with a fixture-owned path. This does not authorize
// production paths, protect ancestors, or implement missing-file recovery.
func cleanupManagedProbe(db *sql.DB, name string) error {
	expected, err := loadManagedCandidate(db)
	if err != nil {
		return err
	}
	h, err := openProbe(name, windows.GENERIC_READ|windows.DELETE, windows.OPEN_EXISTING)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), name)
	defer f.Close()
	ok, err := matches(f, expected)
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrInvalid
	}
	deleteFile := byte(1)
	if err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &deleteFile, 1); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// In this fixture, the directory is never moved and no other operation runs.
	// Production must establish those conditions before trusting absence.
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return os.ErrExist
	}
	result, err := db.Exec(`DELETE FROM managed_covers WHERE id=? AND state='ready'
 AND NOT EXISTS (SELECT 1 FROM work_refs WHERE cover_path=managed_covers.relative_path)`, managedID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func readyManagedFixture(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	db, dbPath, name := managedFixture(t)
	f := probeFile(t, name, windows.GENERIC_READ)
	if err := confirmManaged(db, f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return db, dbPath, name
}

func requireManagedCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT count(*) FROM managed_covers WHERE id=?", managedID).Scan(&got); err != nil || got != want {
		t.Fatalf("record count: %d, want %d, error %v", got, want, err)
	}
}

func TestManagedCleanupDeletesMatchedUnreferencedFile(t *testing.T) {
	db, dbPath, name := readyManagedFixture(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openSnapshotDB(t, dbPath)
	if err := cleanupManagedProbe(db, name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file remains: %v", err)
	}
	requireManagedCount(t, db, 0)
}

func TestManagedCleanupRefusalsPreserveFileAndRecord(t *testing.T) {
	for _, kind := range []string{"reserved", "missing record", "shared reference", "closed DB", "changed content", "different file", "hard link", "symlink", "held file", "read-only"} {
		t.Run(kind, func(t *testing.T) {
			db, dbPath, name := readyManagedFixture(t)
			want := "synthetic payload"
			wantCount := 1
			var expectedError error
			var held *os.File
			switch kind {
			case "reserved":
				_, err := db.Exec("UPDATE managed_covers SET state='reserved',volume=NULL,file_id=NULL,size=NULL,hash=NULL")
				if err != nil {
					t.Fatal(err)
				}
				expectedError = sql.ErrNoRows
			case "missing record":
				if _, err := db.Exec("DELETE FROM managed_covers"); err != nil {
					t.Fatal(err)
				}
				wantCount = 0
				expectedError = sql.ErrNoRows
			case "shared reference":
				if _, err := db.Exec("INSERT INTO work_refs VALUES (1,?),(2,?)", "covers/"+managedID+".png", "covers/"+managedID+".png"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("DELETE FROM work_refs WHERE work_id=1"); err != nil {
					t.Fatal(err)
				}
				expectedError = sql.ErrNoRows
			case "closed DB":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			case "changed content":
				want = "synthetiX payload" // Same length, same identity, different hash.
				if err := os.WriteFile(name, []byte(want), 0600); err != nil {
					t.Fatal(err)
				}
				expectedError = os.ErrInvalid
			case "different file", "symlink":
				other := filepath.Join(filepath.Dir(name), "preserved-original.png")
				if err := os.Rename(name, other); err != nil {
					t.Fatal(err)
				}
				if kind == "different file" {
					if err := os.WriteFile(name, []byte(want), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(other, name); err != nil {
					t.Fatal(err)
				}
				expectedError = os.ErrInvalid
			case "hard link":
				if err := os.Link(name, filepath.Join(filepath.Dir(name), "preserved-link.png")); err != nil {
					t.Fatal(err)
				}
				expectedError = os.ErrInvalid
			case "held file":
				held = probeFile(t, name, windows.GENERIC_READ)
				expectedError = windows.ERROR_SHARING_VIOLATION
			case "read-only":
				p, err := windows.UTF16PtrFromString(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_READONLY); err != nil {
					t.Fatal(err)
				}
				defer windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_NORMAL)
				expectedError = windows.ERROR_ACCESS_DENIED
			}
			err := cleanupManagedProbe(db, name)
			if err == nil || (expectedError != nil && !errors.Is(err, expectedError)) {
				t.Fatalf("unexpected cleanup result: %v, want %v", err, expectedError)
			}
			if kind == "closed DB" {
				db = openSnapshotDB(t, dbPath)
			}
			if held != nil {
				if err := held.Close(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := os.ReadFile(name)
			if err != nil || string(got) != want {
				t.Fatalf("file changed: %q %v", got, err)
			}
			requireManagedCount(t, db, wantCount)
		})
	}
}

func TestManagedCleanupDBFailureAfterDeletionKeepsRecord(t *testing.T) {
	db, dbPath, name := readyManagedFixture(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_cleanup BEFORE DELETE ON managed_covers
 BEGIN SELECT RAISE(ABORT,'synthetic record cleanup failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := cleanupManagedProbe(db, name); err == nil || !strings.Contains(err.Error(), "synthetic record cleanup failure") {
		t.Fatalf("expected record cleanup failure: %v", err)
	}
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file deletion not completed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openSnapshotDB(t, dbPath)
	requireManagedCount(t, db, 1)
	// This probe deliberately leaves missing-file recovery to a later boundary.
	if err := cleanupManagedProbe(db, name); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("missing file treated as successful cleanup: %v", err)
	}
	requireManagedCount(t, db, 1)
}
