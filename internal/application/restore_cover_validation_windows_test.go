package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestPreparedRestoreWithImages(t *testing.T) {
	ctx := context.Background()
	versions, _, err := storage.CurrentRestoreSchema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want error
	}{
		{"shared and omitted", nil},
		{"unlisted reference", errRestoreCoverReference},
		{"unreferenced manifest entry", errRestoreCoverReference},
		{"missing managed record", errRestoreCoverReference},
		{"different DB digest", errRestoreCoverReference},
		{"broken PNG", errRestoreCoverContent},
		{"oversized PNG", errRestoreCoverContent},
		{"cancelled", context.Canceled},
		{"cancelled during image read", context.Canceled},
		{"prepared bytes changed", errRestoreCoverContent},
		{"prepared bytes extended", errRestoreCoverContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			width := 2
			if tc.name == "oversized PNG" {
				width = 601
			}
			if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, 2))); err != nil {
				t.Fatal(err)
			}
			data := buf.Bytes()
			if tc.name == "broken PNG" {
				data = data[:len(data)-5]
			}
			path := filepath.Join(t.TempDir(), "fixture.sqlite")
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
			func() {
				defer func() {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				}()
				for i, digit := range []string{"1", "1", "2"} {
					if tc.name == "unreferenced manifest entry" && digit == "2" {
						continue
					}
					id := strings.Repeat(digit, 32)
					hash := sha256.Sum256(data)
					if tc.name == "different DB digest" {
						hash[0] ^= 1
					}
					if tc.name != "missing managed record" || digit != "1" {
						_, err := db.Exec(`INSERT OR IGNORE INTO managed_covers(id,state,volume_serial,file_id,byte_size,sha256) VALUES (?,'ready',?,?,?,?)`,
							id, strings.Repeat("a", 16), strings.Repeat("b", 32), len(data), hash[:])
						if err != nil {
							t.Fatal(err)
						}
					}
					_, err := db.Exec(`INSERT INTO works(url,title,thumbnail_path,created_at,updated_at) VALUES (?,'Fixture',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
						fmt.Sprintf("https://example.test/%d", i), "covers/"+id+".png")
					if err != nil {
						t.Fatal(err)
					}
				}
			}()
			dbBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f := newRestoreFixtureWithImages(t, dbBytes, versions, data, func(m *backup.Manifest) {
				if tc.name == "unlisted reference" {
					m.OmittedImages = []backup.OmittedImage{}
				}
			})
			p, err := PrepareRestore(ctx, f.source, f.root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := p.Cleanup(nil); err != nil {
					t.Error(err)
				}
				f.assertProtected(t)
			})
			checkCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			if tc.name == "cancelled" {
				cancel()
			}
			for i, file := range p.files {
				if file.ImageID == "" {
					continue
				}
				switch tc.name {
				case "cancelled during image read":
					p.files[i].Reader = cancelRestoreCoverReader{file.Reader, cancel}
				case "prepared bytes changed":
					changed := bytes.Clone(data)
					changed[0] ^= 1
					p.files[i].Reader = bytes.NewReader(changed)
				case "prepared bytes extended":
					p.files[i].Reader = bytes.NewReader(append(bytes.Clone(data), 0))
				}
			}
			r, err := checkPreparedRestoreWithImages(checkCtx, p)
			if !errors.Is(err, tc.want) || r.ReleaseErr != nil {
				t.Fatal(r, err)
			}
		})
	}
}

type cancelRestoreCoverReader struct {
	io.ReaderAt
	cancel context.CancelFunc
}

func (r cancelRestoreCoverReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, offset)
	r.cancel()
	return n, err
}
