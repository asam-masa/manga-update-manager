package restoreswitch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/storage"
)

// DB evidence is checked only after the plan, completion, and physical
// layout agree. This models classification under an exclusive fixture lock.
func decideFixtureRecoveryWithDB(root string, check func() error) (string, []recoveryMove, error) {
	outcome, moves, err := decideRecoveryWithIdentity(root, observeNativeFixture)
	if err != nil || outcome != "keep new" {
		return outcome, moves, err
	}
	if err := check(); err != nil {
		return "", nil, errors.Join(errRecoveryStop, err)
	}
	return outcome, moves, nil
}

func TestFixtureRecoveryChecksCompletedDatabaseRecords(t *testing.T) {
	const imageBody = "synthetic image"
	ctx := context.Background()
	coverID := strings.Repeat("1", 32)
	old := fixtureCoverIdentity{strings.Repeat("a", 16), strings.Repeat("b", 32)}
	digest := sha256.Sum256([]byte(imageBody))
	for _, mode := range []string{"updated", "stale", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			root := canonicalRecoveryTestDirectory(t, recoveryFixture(t, 0))
			write(t, root, stagedImage, imageBody)
			path := filepath.Join(root, stagedDB)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			store, err := storage.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", interruptedDBDSN(path, "rw"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.ExecContext(ctx, `INSERT INTO managed_covers(id,state,volume_serial,file_id,byte_size,sha256)
VALUES (?,'ready',?,?,?,?)`, coverID, old.volume, old.file, len(imageBody), digest[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO works(url,title,thumbnail_path,created_at,updated_at)
VALUES ('https://example.test/1','Synthetic work',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, "covers/"+coverID+".png"); err != nil {
				t.Fatal(err)
			}
			plan := nativeRecoveryFixturePlan(t, root)
			raw, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			write(t, root, planName, string(raw))
			move(t, root, liveDB, retiredDB)
			move(t, root, liveImage, retiredImage)
			move(t, root, stagedImage, liveImage)
			imageToken, found, err := observeNativeFixture(root, liveImage)
			if err != nil || !found || imageToken != plan.NewImage {
				t.Fatal(found, err)
			}
			volume := binary.LittleEndian.Uint64(imageToken[:8])
			want := fixtureCoverIdentity{fmt.Sprintf("%016x", volume), hex.EncodeToString(imageToken[8:24])}
			if mode != "stale" {
				if err := updateRestoreFixtureCovers(ctx, db, []string{coverID}, []fixtureCoverIdentity{old}, []fixtureCoverIdentity{want}, digest); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			move(t, root, stagedDB, liveDB)
			if mode == "unreadable" {
				// Truncate in place so the physical identity still agrees.
				write(t, root, liveDB, "not a SQLite database")
			}
			done, err := json.Marshal(recoveryCompletion{1, plan.ID, sha256.Sum256(raw)})
			if err != nil {
				t.Fatal(err)
			}
			write(t, root, completeName, string(done))
			if outcome, moves, err := decideRecoveryWithIdentity(root, observeNativeFixture); outcome != "keep new" || len(moves) != 0 || err != nil {
				t.Fatal("expected consistent record and physical evidence for every fixture", outcome, moves, err)
			}
			before, err := os.ReadFile(filepath.Join(root, liveDB))
			if err != nil {
				t.Fatal(err)
			}
			checkCalls := 0
			outcome, moves, err := decideFixtureRecoveryWithDB(root, func() error {
				checkCalls++
				records, err := storage.DatabaseCopyCovers(ctx, filepath.Join(root, liveDB), 1)
				if err != nil {
					return err
				}
				if len(records) != 1 || records[0].ID != coverID || records[0].Info == nil {
					return errFixtureCoverMismatch
				}
				info := records[0].Info
				if info.VolumeSerial != volume || hex.EncodeToString(info.FileID[:]) != want.file || info.ByteSize != int64(len(imageBody)) || info.SHA256 != digest {
					return errFixtureCoverMismatch
				}
				return nil
			})
			if checkCalls != 1 || len(moves) != 0 {
				t.Fatal(checkCalls, moves)
			}
			switch mode {
			case "updated":
				if outcome != "keep new" || err != nil {
					t.Fatal(outcome, err)
				}
			case "stale":
				if outcome != "" || !errors.Is(err, errRecoveryStop) || !errors.Is(err, errFixtureCoverMismatch) {
					t.Fatal(outcome, err)
				}
			case "unreadable":
				if outcome != "" || !errors.Is(err, errRecoveryStop) || !errors.Is(err, storage.ErrDatabaseCopyCovers) {
					t.Fatal(outcome, err)
				}
			}
			content(t, root, liveImage, imageBody)
			content(t, root, liveDB, string(before))
			content(t, root, retiredDB, "old-db")
			content(t, root, retiredImage, "old-image")
			content(t, root, completeName, string(done))
		})
	}
}
