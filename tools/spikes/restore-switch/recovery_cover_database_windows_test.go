package restoreswitch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestPlacedCoverIdentitiesRoundTripThroughWorkingDB(t *testing.T) {
	ctx := context.Background()
	root := canonicalRecoveryTestDirectory(t, t.TempDir())
	if err := os.Mkdir(filepath.Join(root, "covers"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "working.sqlite")
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
	ids := []string{strings.Repeat("1", 32), strings.Repeat("2", 32), strings.Repeat("3", 32)}
	old := fixtureCoverIdentity{strings.Repeat("a", 16), strings.Repeat("b", 32)}
	payload := []byte("synthetic image")
	digest := sha256.Sum256(payload)
	for i, id := range ids {
		if _, err := db.ExecContext(ctx, `INSERT INTO managed_covers(id,state,volume_serial,file_id,byte_size,sha256)
VALUES (?,'ready',?,?,?,?)`, id, old.volume, old.file, len(payload), digest[:]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO works(id,url,title,thumbnail_path,created_at,updated_at)
VALUES (?,?,'Synthetic work',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, i+1, fmt.Sprintf("https://example.test/%d", i), "covers/"+id+".png"); err != nil {
			t.Fatal(err)
		}
	}

	var placed []fixtureCoverIdentity
	var observed []platform.BackupTempIdentity
	for _, id := range ids[:2] {
		temp := prepareRecoveryRecord(t, root, filepath.Join("covers", id+".png"), payload)
		publishRecoveryRecord(t, temp)
		identity := temp.Identity()
		observed = append(observed, identity)
		placed = append(placed, fixtureCoverIdentity{fmt.Sprintf("%016x", identity.VolumeSerial), hex.EncodeToString(identity.FileID[:])})
	}
	if observed[0] == observed[1] {
		t.Fatal("two separately created images have the same identity")
	}
	if err := updateRestoreFixtureCovers(ctx, db, ids[:2], []fixtureCoverIdentity{old, old}, placed, digest); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	checkRestoreFixtureCovers(t, ctx, path, ids, old, placed, true, digest)
	records, err := storage.DatabaseCopyCovers(ctx, path, len(ids))
	if err != nil || len(records) != len(ids) {
		t.Fatal(len(records), err)
	}
	for i, record := range records[:2] {
		if record.ID != ids[i] || record.Info == nil {
			t.Fatal("unexpected reread cover record", record.ID)
		}
		info := record.Info
		got, err := platform.ReadVerifiedCover(ctx, root, record.ID, platform.CoverFileInfo{
			VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256,
		})
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatal("placed bytes do not match the reread identity", err)
		}
	}

	// Keep the original present so its ID cannot be recycled. Substitute
	// identical bytes at its name and verify the stored identity rejects it.
	replaced := filepath.Join(root, "covers", ids[0]+".png")
	if err := os.Rename(replaced, filepath.Join(root, "retained-image.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaced, payload, 0600); err != nil {
		t.Fatal(err)
	}
	info := records[0].Info
	got, err := platform.ReadVerifiedCover(ctx, root, ids[0], platform.CoverFileInfo{
		VolumeSerial: info.VolumeSerial, FileID: info.FileID, ByteSize: info.ByteSize, SHA256: info.SHA256,
	})
	if got != nil || !errors.Is(err, platform.ErrInvalidCoverFile) {
		t.Fatal("stored identity accepted a different file with identical contents", err)
	}
}
