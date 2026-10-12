package coverfileboundaries

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/windows"
	_ "modernc.org/sqlite"
)

// Snapshot representation probe only; not the production managed_covers schema.
const snapshotSchema = `CREATE TABLE snapshots (
 id INTEGER PRIMARY KEY,
 volume TEXT NOT NULL CHECK(length(volume)=16 AND instr(volume,char(0))=0 AND volume NOT GLOB '*[^0-9a-f]*'),
 file_id TEXT NOT NULL CHECK(length(file_id)=32 AND instr(file_id,char(0))=0 AND file_id NOT GLOB '*[^0-9a-f]*'),
 size INTEGER NOT NULL CHECK(size>0),
 hash BLOB NOT NULL CHECK(length(hash)=32)
) STRICT;`

func openSnapshotDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func saveSnapshot(db *sql.DB, value fingerprint) error {
	_, err := db.Exec("INSERT INTO snapshots VALUES (1,?,?,?,?)",
		fmt.Sprintf("%016x", value.Identity.Volume), hex.EncodeToString(value.Identity.ID[:]), value.Size, value.Hash[:])
	return err
}

func loadSnapshot(db *sql.DB) (fingerprint, error) {
	var value fingerprint
	var volume, id string
	var hash []byte
	err := db.QueryRow("SELECT volume,file_id,size,hash FROM snapshots WHERE id=1").Scan(&volume, &id, &value.Size, &hash)
	if err != nil {
		return fingerprint{}, err
	}
	value.Identity.Volume, err = strconv.ParseUint(volume, 16, 64)
	if err != nil {
		return fingerprint{}, err
	}
	decoded, err := hex.DecodeString(id)
	if err != nil {
		return fingerprint{}, err
	}
	if len(volume) != 16 || len(decoded) != 16 || len(hash) != 32 || value.Size <= 0 {
		return fingerprint{}, os.ErrInvalid
	}
	copy(value.Identity.ID[:], decoded)
	copy(value.Hash[:], hash)
	return value, nil
}

func matchesSnapshot(db *sql.DB, file *os.File) (bool, error) {
	expected, err := loadSnapshot(db)
	if err != nil {
		return false, err
	}
	return matches(file, expected)
}

func TestSQLiteSnapshotRoundTripAndFileComparison(t *testing.T) {
	dir := t.TempDir()
	name, other := filepath.Join(dir, "original.png"), filepath.Join(dir, "other.png")
	for _, path := range []string{name, other} {
		if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := probeFile(t, name, windows.GENERIC_READ)
	expected, err := capture(f)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "synthetic.sqlite")
	db := openSnapshotDB(t, dbPath)
	if _, err := db.Exec(snapshotSchema); err != nil {
		t.Fatal(err)
	}
	if err := saveSnapshot(db, expected); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openSnapshotDB(t, dbPath)
	got, err := loadSnapshot(db)
	if err != nil || got != expected {
		t.Fatalf("round trip mismatch: %v", err)
	}
	if ok, err := matchesSnapshot(db, f); err != nil || !ok {
		t.Fatalf("same file: %v %v", ok, err)
	}
	if ok, err := matchesSnapshot(db, probeFile(t, other, windows.GENERIC_READ)); err != nil || ok {
		t.Fatalf("other file: %v %v", ok, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("after!"), 0600); err != nil {
		t.Fatal(err)
	}
	f = probeFile(t, name, windows.GENERIC_READ)
	if ok, err := matchesSnapshot(db, f); err != nil || ok {
		t.Fatalf("changed content: %v %v", ok, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if ok, err := matchesSnapshot(db, f); err == nil || ok {
		t.Fatalf("closed DB accepted: %v %v", ok, err)
	}
	f.Close()
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "after!" {
		t.Fatalf("file changed: %q %v", data, err)
	}
}

func TestSQLiteSnapshotPreservesHighBitsAndRejectsMissingRecord(t *testing.T) {
	db := openSnapshotDB(t, filepath.Join(t.TempDir(), "synthetic.sqlite"))
	if _, err := db.Exec(snapshotSchema); err != nil {
		t.Fatal(err)
	}
	// No record must stop before any attempt to inspect the supplied file.
	if ok, err := matchesSnapshot(db, nil); err != sql.ErrNoRows || ok {
		t.Fatalf("missing record accepted: %v %v", ok, err)
	}
	value := fingerprint{Identity: identity128{Volume: ^uint64(0)}, Size: 123}
	for i := range value.Identity.ID {
		value.Identity.ID[i] = byte(255 - i)
	}
	for i := range value.Hash {
		value.Hash[i] = byte(i)
	}
	if err := saveSnapshot(db, value); err != nil {
		t.Fatal(err)
	}
	got, err := loadSnapshot(db)
	if err != nil || got != value {
		t.Fatalf("high bits truncated: %v", err)
	}
}
