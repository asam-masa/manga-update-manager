package restoreswitch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type fixtureCoverIdentity struct {
	volume, file string
}

var errFixtureCoverMismatch = errors.New("expected cover row did not match")

func TestRestoreSwitchObservesCommittedCoverRecords(t *testing.T) {
	ctx := context.Background()
	old := fixtureCoverIdentity{strings.Repeat("a", 16), strings.Repeat("b", 32)}
	placed := []fixtureCoverIdentity{
		{strings.Repeat("c", 16), strings.Repeat("d", 32)},
		{strings.Repeat("c", 16), strings.Repeat("e", 32)},
	}
	digest := sha256.Sum256([]byte("synthetic image"))
	coverIDs := []string{strings.Repeat("1", 32), strings.Repeat("2", 32), strings.Repeat("3", 32)}
	for _, commit := range []bool{true, false} {
		t.Run(fmt.Sprint("commit=", commit), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "working.sqlite")
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
			t.Cleanup(func() { _ = db.Close() })
			for i, id := range coverIDs {
				if _, err := db.ExecContext(ctx, `INSERT INTO managed_covers(id,state,volume_serial,file_id,byte_size,sha256)
VALUES (?,'ready',?,?,15,?)`, id, old.volume, old.file, digest[:]); err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, `INSERT INTO works(id,url,title,thumbnail_path,created_at,updated_at)
VALUES (?,?, 'Synthetic work', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
					i+1, fmt.Sprintf("https://example.test/%d", i+1), "covers/"+id+".png"); err != nil {
					t.Fatal(err)
				}
			}

			expected := []fixtureCoverIdentity{old, old}
			if !commit {
				// First write succeeds. The second row's precondition then
				// fails, and the enclosing transaction must undo the first.
				expected[1] = placed[1]
			}
			updateErr := updateRestoreFixtureCovers(ctx, db, coverIDs[:2], expected, placed, digest)
			if commit && updateErr != nil || !commit && !errors.Is(updateErr, errFixtureCoverMismatch) {
				t.Fatal(updateErr)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			checkRestoreFixtureCovers(t, ctx, path, coverIDs, old, placed, commit, digest)
		})
	}
}

// Model only. The caller supplies synthetic observed identities and owns the DB.
func updateRestoreFixtureCovers(ctx context.Context, db *sql.DB, ids []string, expected, placed []fixtureCoverIdentity, digest [sha256.Size]byte) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, rollbackErr)
		}
	}()
	for i, id := range ids {
		result, err := tx.ExecContext(ctx, `UPDATE managed_covers SET volume_serial=?,file_id=?
WHERE id=? AND state='ready' AND volume_serial=? AND file_id=? AND byte_size=15 AND sha256=?`,
			placed[i].volume, placed[i].file, id, expected[i].volume, expected[i].file, digest[:])
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errFixtureCoverMismatch
		}
	}
	return tx.Commit()
}

func checkRestoreFixtureCovers(t *testing.T, ctx context.Context, path string, ids []string, old fixtureCoverIdentity, placed []fixtureCoverIdentity, committed bool, digest [sha256.Size]byte) {
	t.Helper()
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	for i, id := range ids {
		var identity fixtureCoverIdentity
		var path, state, reference string
		var size int64
		var hash []byte
		err := db.QueryRowContext(ctx, `SELECT c.relative_path,c.state,c.volume_serial,c.file_id,c.byte_size,c.sha256,w.thumbnail_path
FROM managed_covers AS c JOIN works AS w ON w.id=? WHERE c.id=?`, i+1, id).
			Scan(&path, &state, &identity.volume, &identity.file, &size, &hash, &reference)
		want := old
		if committed && i < len(placed) {
			want = placed[i]
		}
		if err != nil || identity != want || path != "covers/"+id+".png" || reference != path ||
			state != "ready" || size != 15 || !bytes.Equal(hash, digest[:]) {
			t.Fatal("unexpected reopened cover record", i, identity, path, reference, state, size, err)
		}
	}
}
