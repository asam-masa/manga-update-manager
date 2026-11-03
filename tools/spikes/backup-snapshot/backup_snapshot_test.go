package backupsnapshot

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"io"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Probe only: no product Store, UI, or user data is accessed.
func TestVacuumIntoSnapshot(t *testing.T) {
	for _, journal := range []string{"delete", "wal"} {
		t.Run(journal, func(t *testing.T) {
			dir := t.TempDir()
			db, err := sql.Open("sqlite", filepath.Join(dir, "source.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			for _, statement := range []string{"PRAGMA journal_mode=" + journal, "CREATE TABLE works(id INTEGER PRIMARY KEY, title TEXT)", "INSERT INTO works VALUES(7,'合成作品')", "PRAGMA user_version=12"} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			out := filepath.Join(dir, "snapshot.sqlite")
			if _, err := db.Exec("VACUUM INTO ?", out); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE works SET title='変更後' WHERE id=7"); err != nil {
				t.Fatal(err)
			}
			copyDB, err := sql.Open("sqlite", out)
			if err != nil {
				t.Fatal(err)
			}
			defer copyDB.Close()
			var title, check string
			var version int
			if err := copyDB.QueryRow("SELECT title FROM works WHERE id=7").Scan(&title); err != nil || title != "合成作品" {
				t.Fatal(title, err)
			}
			if err := copyDB.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
				t.Fatal(check, err)
			}
			if err := copyDB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 12 {
				t.Fatal(version, err)
			}
			if _, err := db.Exec("VACUUM INTO ?", out); err == nil {
				t.Fatal("accepted existing nonempty target")
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec("VACUUM INTO ?", filepath.Join(dir, "in-transaction.sqlite")); err == nil {
				t.Fatal("accepted active transaction")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

func TestZIPCompletionAndReadback(t *testing.T) {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, body := range map[string]string{"manifest.json": "{\"formatVersion\":1}", "covers/synthetic.png": "synthetic image bytes"} {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) != 2 {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		input, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(input)
		closeErr := input.Close()
		if err != nil || closeErr != nil || len(body) == 0 {
			t.Fatal(err, closeErr)
		}
	}
	if _, err := zip.NewReader(bytes.NewReader(data[:len(data)-22]), int64(len(data)-22)); err == nil {
		t.Fatal("accepted truncated archive")
	}
	broken := zip.NewWriter(failingWriter{})
	if _, err := broken.Create("manifest.json"); err != nil {
		t.Fatal(err)
	}
	if err := broken.Close(); err == nil {
		t.Fatal("ignored finalization write failure")
	}
}
