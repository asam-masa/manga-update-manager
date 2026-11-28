package restoreswitch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

const interruptedDBName = "interrupted-working.sqlite"

func TestRestoreObservesDatabaseCommitAcrossProcessExit(t *testing.T) {
	first := strings.Repeat("1", 32)
	second := strings.Repeat("2", 32)
	old := strings.Repeat("a", 32)
	for _, mode := range []string{"before-db-commit", "after-db-commit"} {
		t.Run(mode, func(t *testing.T) {
			root := canonicalRecoveryTestDirectory(t, t.TempDir())
			write(t, root, recoveryProcessFixture, recoveryProcessFixture)
			path := filepath.Join(root, interruptedDBName)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			store, err := storage.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			sum := sha256.Sum256([]byte("image"))
			for _, id := range []string{first, second} {
				if _, err := db.ExecContext(ctx, `INSERT INTO managed_covers(id,state,volume_serial,file_id,byte_size,sha256)
VALUES (?,'ready',?,?,5,?)`, id, strings.Repeat("c", 16), old, sum[:]); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			killRecoveryFixtureAtCheckpoint(t, ctx, root, mode, mode)

			db, err = sql.Open("sqlite", interruptedDBDSN(path, "ro"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			for i, id := range []string{first, second} {
				want := old
				if mode == "after-db-commit" {
					want = strings.Repeat(fmt.Sprint(i+3), 32)
				}
				var got string
				if err := db.QueryRowContext(ctx, "SELECT file_id FROM managed_covers WHERE id=?", id).Scan(&got); err != nil || got != want {
					t.Fatal(id, got, err)
				}
			}
		})
	}
}

func runInterruptedDatabaseFixture(t *testing.T, root, mode string) {
	t.Helper()
	path := filepath.Join(root, interruptedDBName)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("expected synthetic working database", err)
	}
	db, err := sql.Open("sqlite", interruptedDBDSN(path, "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= 2; i++ {
		result, err := tx.Exec("UPDATE managed_covers SET file_id=? WHERE id=? AND file_id=?",
			strings.Repeat(fmt.Sprint(i+2), 32), strings.Repeat(fmt.Sprint(i), 32), strings.Repeat("a", 32))
		if err != nil {
			t.Fatal(err)
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if mode == "before-db-commit" {
			pauseRecoveryFixture(t, mode)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	pauseRecoveryFixture(t, mode)
}

func interruptedDBDSN(path, mode string) string {
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path), RawQuery: "mode=" + mode}).String()
}
