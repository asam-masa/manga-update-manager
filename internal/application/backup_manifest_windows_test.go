package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

func manifestMetadata() BackupManifestMetadata {
	return BackupManifestMetadata{CreatedAt: time.Date(2026, 11, 8, 21, 0, 0, 123, time.FixedZone("JST", 9*3600)), AppVersion: "synthetic", Migrations: []string{"001_create_works.sql"}}
}

func TestBuildBackupManifestDigestAndOwnership(t *testing.T) {
	body := bytes.Repeat([]byte("synthetic-copy"), 9000)
	source := bytes.NewReader(body)
	reads := 0
	r := manifestReaderAt(func(p []byte, off int64) (int, error) {
		reads++
		if len(p) > 32<<10 {
			t.Fatal("unbounded read", len(p))
		}
		return source.ReadAt(p, off)
	})
	metadata := manifestMetadata()
	data, err := BuildBackupManifest(context.Background(), r, int64(len(body)), metadata)
	if err != nil || reads < 2 {
		t.Fatal(reads, err)
	}
	m, err := backup.DecodeManifest(bytes.NewReader(data))
	want := sha256.Sum256(body)
	if err != nil || m.Database.Size != int64(len(body)) || m.Database.SHA256 != hex.EncodeToString(want[:]) || m.CreatedAt != "2026-11-08T12:00:00.000000123Z" || m.AppVersion != metadata.AppVersion || m.Images == nil || m.OmittedImages == nil {
		t.Fatal(m, err)
	}
	metadata.Migrations[0] = "changed"
	if _, err := backup.DecodeManifest(bytes.NewReader(data)); err != nil {
		t.Fatal("result aliases caller metadata", err)
	}
	// ReaderAt position and lifetime are borrowed, not consumed or closed.
	b, err := io.ReadAll(source)
	if err != nil || !bytes.Equal(b, body) {
		t.Fatal(err)
	}
	metadata = manifestMetadata()
	id, omitted := strings.Repeat("a", 32), strings.Repeat("b", 32)
	metadata.Covers.Images = []backup.Image{{ID: id, Path: "covers/" + id + ".png", Size: 17, SHA256: strings.Repeat("1", 64)}}
	metadata.Covers.OmittedImages = []backup.OmittedImage{{ID: omitted, Path: "covers/" + omitted + ".png", Reason: "missing"}}
	data, err = BuildBackupManifest(context.Background(), source, int64(len(body)), metadata)
	if err != nil {
		t.Fatal(err)
	}
	m, err = backup.DecodeManifest(bytes.NewReader(data))
	if err != nil || len(m.Images) != 1 || m.Images[0] != metadata.Covers.Images[0] || len(m.OmittedImages) != 1 || m.OmittedImages[0] != metadata.Covers.OmittedImages[0] {
		t.Fatal("image metadata lost", m, err)
	}
}

type manifestReaderAt func([]byte, int64) (int, error)

func (r manifestReaderAt) ReadAt(p []byte, off int64) (int, error) { return r(p, off) }

func TestBuildBackupManifestReadFailureAndCancellation(t *testing.T) {
	cause := errors.New("synthetic read failure")
	for _, mode := range []string{"short", "long", "read-error", "early-cancel", "late-cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "early-cancel" {
				cancel()
			}
			r := manifestReaderAt(func(p []byte, off int64) (int, error) {
				if mode == "early-cancel" {
					t.Fatal("read after cancellation")
				}
				if mode == "read-error" {
					return 0, cause
				}
				if mode == "late-cancel" {
					cancel()
				}
				body := "x"
				if mode == "short" {
					body = ""
				} else if mode == "long" {
					body = "xx"
				}
				return strings.NewReader(body).ReadAt(p, off)
			})
			data, err := BuildBackupManifest(ctx, r, 1, manifestMetadata())
			if data != nil || !errors.Is(err, ErrBackupManifest) {
				t.Fatal(string(data), err)
			}
			if mode == "read-error" && !errors.Is(err, cause) || strings.Contains(mode, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cause lost", err)
			}
		})
	}
}

func TestBuildBackupManifestRejectsMetadataBeforeRead(t *testing.T) {
	for _, mode := range []string{"zero-size", "oversized-db", "nil-reader", "invalid-time", "unknown-migration", "duplicate-image", "too-many-images"} {
		t.Run(mode, func(t *testing.T) {
			metadata, size := manifestMetadata(), int64(1)
			var r io.ReaderAt = manifestReaderAt(func([]byte, int64) (int, error) { t.Error("read invalid input"); return 0, io.EOF })
			switch mode {
			case "zero-size":
				size = 0
			case "oversized-db":
				size = backup.MaxDatabaseBytes + 1
			case "nil-reader":
				r = nil
			case "invalid-time":
				metadata.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "unknown-migration":
				metadata.Migrations = []string{"unknown.sql"}
			case "duplicate-image":
				im := backup.Image{ID: strings.Repeat("a", 32), Path: "covers/" + strings.Repeat("a", 32) + ".png", Size: 1, SHA256: strings.Repeat("0", 64)}
				metadata.Covers.Images = []backup.Image{im, im}
			case "too-many-images":
				metadata.Covers.OmittedImages = make([]backup.OmittedImage, backup.MaxImages+1)
			}
			data, err := BuildBackupManifest(context.Background(), r, size, metadata)
			if data != nil || !errors.Is(err, backup.ErrManifest) {
				t.Fatal(data, err)
			}
		})
	}
}

func TestBuildBackupManifestEncodedAndTotalBoundaries(t *testing.T) {
	ctx := context.Background()
	body := bytes.NewReader([]byte("x"))
	metadata := manifestMetadata()
	data, err := BuildBackupManifest(ctx, body, 1, metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadata.AppVersion += strings.Repeat("a", int(backup.MaxManifestBytes)-len(data))
	data, err = BuildBackupManifest(ctx, body, 1, metadata)
	if err != nil || int64(len(data)) != backup.MaxManifestBytes {
		t.Fatal(len(data), err)
	}
	metadata.AppVersion += "a"
	if data, err := BuildBackupManifest(ctx, body, 1, metadata); data != nil || !errors.Is(err, backup.ErrManifest) {
		t.Fatal(len(data), err)
	}
	metadata = manifestMetadata()
	for i := 0; i < 103; i++ {
		id := fmt.Sprintf("%032x", i)
		metadata.Covers.Images = append(metadata.Covers.Images, backup.Image{ID: id, Path: "covers/" + id + ".png", Size: backup.MaxImageBytes, SHA256: strings.Repeat("0", 64)})
	}
	// Solve for the last metadata-only image size including its JSON digits.
	last := &metadata.Covers.Images[102]
	last.Size = 1
	m := backup.Manifest{FormatVersion: 1, CreatedAt: metadata.CreatedAt.UTC().Format(time.RFC3339Nano), AppVersion: metadata.AppVersion, Migrations: metadata.Migrations, Database: backup.File{Path: "database.sqlite", Size: 1, SHA256: strings.Repeat("0", 64)}, Images: metadata.Covers.Images, OmittedImages: []backup.OmittedImage{}}
	for i := 0; i < 3; i++ {
		encoded, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		last.Size = backup.MaxTotalBytes - 1 - 102*backup.MaxImageBytes - int64(len(encoded))
	}
	if _, err := BuildBackupManifest(ctx, body, 1, metadata); err != nil {
		t.Fatal("exact total rejected", err)
	}
	last.Size++
	if data, err := BuildBackupManifest(ctx, body, 1, metadata); data != nil || !errors.Is(err, backup.ErrManifest) {
		t.Fatal("excess total accepted", len(data), err)
	}
}
