package managedcoverschema

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

// Candidate schema only: not embedded in production migrations.
const schema = `CREATE TABLE managed_covers (
 id TEXT PRIMARY KEY CHECK(length(id)=32 AND instr(id,char(0))=0 AND id NOT GLOB '*[^0-9a-f]*'),
 relative_path TEXT GENERATED ALWAYS AS ('covers/' || id || '.png') STORED UNIQUE,
 state TEXT NOT NULL CHECK(state IN ('reserved','ready')),
 volume_serial TEXT,
 file_id TEXT,
 byte_size INTEGER,
 sha256 BLOB,
 CHECK((state='reserved' AND volume_serial IS NULL AND file_id IS NULL AND byte_size IS NULL AND sha256 IS NULL)
 OR (state='ready' AND volume_serial IS NOT NULL AND length(volume_serial)=8 AND instr(volume_serial,char(0))=0 AND volume_serial NOT GLOB '*[^0-9a-f]*'
 AND file_id IS NOT NULL AND length(file_id)=16 AND instr(file_id,char(0))=0 AND file_id NOT GLOB '*[^0-9a-f]*'
 AND byte_size IS NOT NULL AND byte_size>0 AND sha256 IS NOT NULL AND length(sha256)=32))
) STRICT;`

func database(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "synthetic.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

const id = "0123456789abcdef0123456789abcdef"

func TestIDWithEmbeddedNULRejected(t *testing.T) {
	db := database(t)
	if _, err := db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", id+"\x00/foreign"); err == nil {
		t.Fatal("ID with embedded NUL accepted")
	}
}

func TestReservedToReadyAndGeneratedPath(t *testing.T) {
	db := database(t)
	if _, err := db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE managed_covers SET state='ready',volume_serial='01234567',file_id='0123456789abcdef',byte_size=123,sha256=? WHERE id=?", make([]byte, 32), id); err != nil {
		t.Fatal(err)
	}
	var path, state string
	if err := db.QueryRow("SELECT relative_path,state FROM managed_covers WHERE id=?", id).Scan(&path, &state); err != nil {
		t.Fatal(err)
	}
	if path != "covers/"+id+".png" || state != "ready" {
		t.Fatalf("%q %q", path, state)
	}
	if _, err := db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", id); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestInvalidRecordsRejected(t *testing.T) {
	for _, query := range []string{
		"INSERT INTO managed_covers(id,state) VALUES ('../outside','reserved')",
		"INSERT INTO managed_covers(id,state) VALUES (NULL,'reserved')",
		"INSERT INTO managed_covers(id,state) VALUES ('" + id + "','unknown')",
		"INSERT INTO managed_covers(id,state) VALUES ('" + id + "','ready')",
		"INSERT INTO managed_covers(id,state,byte_size) VALUES ('" + id + "','reserved',123)",
	} {
		t.Run(query, func(t *testing.T) {
			db := database(t)
			if _, err := db.Exec(query); err == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}

func TestFailedReadyTransitionPreservesReservation(t *testing.T) {
	db := database(t)
	if _, err := db.Exec("INSERT INTO managed_covers(id,state) VALUES (?,'reserved')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE managed_covers SET state='ready' WHERE id=?", id); err == nil {
		t.Fatal("incomplete transition accepted")
	}
	var state string
	if err := db.QueryRow("SELECT state FROM managed_covers WHERE id=?", id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "reserved" {
		t.Fatal(state)
	}
}

func TestReadyMetadataMustBeCompleteAndWellFormed(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		volume, fileID, size, hash any
	}{
		{"missing volume", nil, "0123456789abcdef", 123, make([]byte, 32)},
		{"invalid volume", "../outside", "0123456789abcdef", 123, make([]byte, 32)},
		{"NUL volume", "01234567\x00outside", "0123456789abcdef", 123, make([]byte, 32)},
		{"missing file ID", "01234567", nil, 123, make([]byte, 32)},
		{"invalid file ID", "01234567", "not-a-file-id", 123, make([]byte, 32)},
		{"NUL file ID", "01234567", "0123456789abcdef\x00outside", 123, make([]byte, 32)},
		{"missing size", "01234567", "0123456789abcdef", nil, make([]byte, 32)},
		{"zero size", "01234567", "0123456789abcdef", 0, make([]byte, 32)},
		{"negative size", "01234567", "0123456789abcdef", -1, make([]byte, 32)},
		{"missing hash", "01234567", "0123456789abcdef", 123, nil},
		{"short hash", "01234567", "0123456789abcdef", 123, make([]byte, 31)},
		{"text hash", "01234567", "0123456789abcdef", 123, "0123456789abcdef0123456789abcdef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := database(t)
			_, err := db.Exec("INSERT INTO managed_covers(id,state,volume_serial,file_id,byte_size,sha256) VALUES (?,'ready',?,?,?,?)", id, tc.volume, tc.fileID, tc.size, tc.hash)
			if err == nil {
				t.Fatal("invalid metadata accepted")
			}
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM managed_covers").Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial insert: %d %v", count, err)
			}
		})
	}
}
