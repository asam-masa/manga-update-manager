package coverfileboundaries

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Synthetic schema only. work_refs models shared references, not production works.
const managedSchema = `CREATE TABLE managed_covers (
 id TEXT PRIMARY KEY CHECK(length(id)=32 AND instr(id,char(0))=0 AND id NOT GLOB '*[^0-9a-f]*'),
 relative_path TEXT GENERATED ALWAYS AS ('covers/' || id || '.png') STORED UNIQUE,
 state TEXT NOT NULL CHECK(state IN ('reserved','ready')),
 volume TEXT, file_id TEXT, size INTEGER, hash BLOB,
 CHECK((state='reserved' AND volume IS NULL AND file_id IS NULL AND size IS NULL AND hash IS NULL)
 OR (state='ready' AND volume IS NOT NULL AND length(volume)=16 AND instr(volume,char(0))=0 AND volume NOT GLOB '*[^0-9a-f]*'
 AND file_id IS NOT NULL AND length(file_id)=32 AND instr(file_id,char(0))=0 AND file_id NOT GLOB '*[^0-9a-f]*'
 AND size IS NOT NULL AND size>0 AND hash IS NOT NULL AND length(hash)=32))
) STRICT;
CREATE TABLE work_refs (work_id INTEGER PRIMARY KEY, cover_path TEXT NOT NULL) STRICT;`

const managedID = "0123456789abcdef0123456789abcdef"

func managedFixture(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "synthetic.sqlite")
	db := openSnapshotDB(t, dbPath)
	if _, err := db.Exec(managedSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", managedID); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "covers", managedID+".png")
	if err := os.Mkdir(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	// Not a PNG decoder or write/flush durability probe.
	if err := os.WriteFile(name, []byte("synthetic payload"), 0600); err != nil {
		t.Fatal(err)
	}
	return db, dbPath, name
}

func confirmManaged(db *sql.DB, file *os.File) error {
	value, err := capture(file)
	if err != nil {
		return err
	}
	result, err := db.Exec(`UPDATE managed_covers SET state='ready',volume=?,file_id=?,size=?,hash=?
 WHERE id=? AND state='reserved'`, fmt.Sprintf("%016x", value.Identity.Volume),
		hex.EncodeToString(value.Identity.ID[:]), value.Size, value.Hash[:], managedID)
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

func loadManagedCandidate(db *sql.DB) (fingerprint, error) {
	return scanFingerprint(db.QueryRow(`SELECT volume,file_id,size,hash FROM managed_covers AS cover
 WHERE id=? AND state='ready'
 AND NOT EXISTS (SELECT 1 FROM work_refs WHERE cover_path=cover.relative_path)`, managedID))
}

func requireNoCandidate(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := loadManagedCandidate(db); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no candidate, got %v", err)
	}
}

func TestManagedCandidateRequiresReadyAndNoReferences(t *testing.T) {
	db, dbPath, name := managedFixture(t)
	requireNoCandidate(t, db)
	f := probeFile(t, name, windows.GENERIC_READ)
	if err := confirmManaged(db, f); err != nil {
		t.Fatal(err)
	}
	if err := confirmManaged(db, f); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ready record changed again: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openSnapshotDB(t, dbPath)
	value, err := loadManagedCandidate(db)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := matches(f, value); err != nil || !ok {
		t.Fatalf("persisted candidate mismatch: %v %v", ok, err)
	}
	for _, work := range []int{1, 2} {
		if _, err := db.Exec("INSERT INTO work_refs VALUES (?,?)", work, "covers/"+managedID+".png"); err != nil {
			t.Fatal(err)
		}
	}
	requireNoCandidate(t, db)
	if _, err := db.Exec("DELETE FROM work_refs WHERE work_id=1"); err != nil {
		t.Fatal(err)
	}
	requireNoCandidate(t, db)
	if _, err := db.Exec("DELETE FROM work_refs WHERE work_id=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManagedCandidate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM managed_covers"); err != nil {
		t.Fatal(err)
	}
	requireNoCandidate(t, db)
}

func TestManagedConfirmationFailurePreservesReservation(t *testing.T) {
	for _, failure := range []string{"closed handle", "DB update aborted"} {
		t.Run(failure, func(t *testing.T) {
			db, _, name := managedFixture(t)
			f := probeFile(t, name, windows.GENERIC_READ)
			if failure == "closed handle" {
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.Exec(`CREATE TRIGGER reject_ready BEFORE UPDATE ON managed_covers
 BEGIN SELECT RAISE(ABORT,'synthetic update failure'); END;`); err != nil {
					t.Fatal(err)
				}
			}
			err := confirmManaged(db, f)
			if failure == "closed handle" && !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
				t.Fatalf("expected invalid handle: %v", err)
			}
			if failure == "DB update aborted" && (err == nil || !strings.Contains(err.Error(), "synthetic update failure")) {
				t.Fatalf("expected injected DB failure: %v", err)
			}
			var unchanged bool
			if err := db.QueryRow(`SELECT state='reserved' AND volume IS NULL AND file_id IS NULL
 AND size IS NULL AND hash IS NULL FROM managed_covers WHERE id=?`, managedID).Scan(&unchanged); err != nil || !unchanged {
				t.Fatalf("reservation changed: %v %v", unchanged, err)
			}
			requireNoCandidate(t, db)
			if failure != "closed handle" {
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(name)
			if err != nil || string(data) != "synthetic payload" {
				t.Fatalf("file changed: %q %v", data, err)
			}
		})
	}
}

func TestManagedIncompleteReadyUpdateRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		volume any
		id     any
	}{
		{"missing volume", nil, strings.Repeat("a", 32)},
		{"missing file ID", strings.Repeat("a", 16), nil},
		{"old volume width", strings.Repeat("a", 8), strings.Repeat("a", 32)},
		{"old file ID width", strings.Repeat("a", 16), strings.Repeat("a", 16)},
		{"NUL volume", strings.Repeat("a", 16) + "\x00tail", strings.Repeat("a", 32)},
		{"NUL file ID", strings.Repeat("a", 16), strings.Repeat("a", 32) + "\x00tail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _, _ := managedFixture(t)
			if _, err := db.Exec(`UPDATE managed_covers SET state='ready',volume=?,file_id=?,size=1,hash=?
 WHERE id=?`, tc.volume, tc.id, make([]byte, 32), managedID); err == nil {
				t.Fatal("incomplete ready record accepted")
			}
			var state string
			if err := db.QueryRow("SELECT state FROM managed_covers WHERE id=?", managedID).Scan(&state); err != nil || state != "reserved" {
				t.Fatalf("reservation not preserved: %q %v", state, err)
			}
			requireNoCandidate(t, db)
		})
	}
}
