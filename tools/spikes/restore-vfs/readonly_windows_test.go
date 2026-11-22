// Package restorevfs is an isolated experiment, not a production restore path.
package restorevfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"golang.org/x/sys/windows"
	_ "modernc.org/sqlite"
	"modernc.org/sqlite/vfs"
)

const logicalDB = "prepared.sqlite"

// Each Open returns its own seek position. Closing this view never closes the
// underlying preparation file. Failed reads are latched because SQLite returns
// its own error codes, not the original Go cause; callers must consult both.
type borrowedFS struct {
	ctx                  context.Context
	reader               io.ReaderAt
	size                 int64
	mu                   sync.Mutex
	active, opens, reads int
	fault, observed      error
	stopAfterFault       bool
}

func (b *borrowedFS) Open(name string) (fs.File, error) {
	if name != logicalDB {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	b.mu.Lock()
	b.active++
	b.opens++
	b.mu.Unlock()
	return &borrowedFile{SectionReader: io.NewSectionReader(b, 0, b.size), owner: b}, nil
}
func (b *borrowedFS) ReadAt(p []byte, off int64) (int, error) {
	b.mu.Lock()
	b.reads++
	fault := b.fault
	prior := b.observed
	b.mu.Unlock()
	if b.stopAfterFault && prior != nil {
		return 0, prior
	}
	if err := b.ctx.Err(); err != nil {
		b.record(err)
		return 0, err
	}
	n, err := b.reader.ReadAt(p, off)
	if fault != nil || b.ctx.Err() != nil {
		err = errors.Join(err, fault, b.ctx.Err())
	}
	if err != nil && err != io.EOF {
		b.record(err)
	}
	return n, err
}
func (b *borrowedFS) record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.observed == nil {
		b.observed = err
	}
}
func (b *borrowedFS) failure() error { b.mu.Lock(); defer b.mu.Unlock(); return b.observed }
func (b *borrowedFS) counts() (int, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active, b.opens, b.reads
}

type borrowedFile struct {
	*io.SectionReader
	owner *borrowedFS
	once  sync.Once
}

func (f *borrowedFile) Close() error {
	f.once.Do(func() { f.owner.mu.Lock(); f.owner.active--; f.owner.mu.Unlock() })
	return nil
}
func (f *borrowedFile) Stat() (fs.FileInfo, error) { return borrowedInfo{size: f.owner.size}, nil }

type borrowedInfo struct{ size int64 }

func (f borrowedInfo) Name() string       { return logicalDB }
func (f borrowedInfo) Size() int64        { return f.size }
func (f borrowedInfo) Mode() fs.FileMode  { return 0444 }
func (f borrowedInfo) ModTime() time.Time { return time.Time{} }
func (f borrowedInfo) IsDir() bool        { return false }
func (f borrowedInfo) Sys() any           { return nil }

func preparedDB(t *testing.T, brokenFK bool) (*application.PreparedRestore, application.PreparedRestoreFile) {
	t.Helper()
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var pathBuf [32768]uint16
	n, nameErr := windows.GetFinalPathNameByHandle(windows.Handle(dir.Fd()), &pathBuf[0], uint32(len(pathBuf)), 0)
	if err := errors.Join(nameErr, dir.Close()); err != nil || n == 0 || n >= uint32(len(pathBuf)) {
		t.Fatal(n, err)
	}
	root := strings.TrimPrefix(windows.UTF16ToString(pathBuf[:n]), `\\?\`)
	initial := filepath.Join(root, "fixture.sqlite")
	db, err := sql.Open("sqlite", initial)
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := db.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO sample(value) VALUES ('synthetic'); CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(parent_id INTEGER REFERENCES parent(id)); INSERT INTO parent VALUES (1);")
	if execErr == nil {
		parentID := 1
		if brokenFK {
			parentID = 2
		}
		_, execErr = db.Exec("INSERT INTO child VALUES (?)", parentID)
	}
	if err := errors.Join(execErr, db.Close()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(initial)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	m := backup.Manifest{FormatVersion: 1, CreatedAt: "2026-11-22T00:00:00Z", AppVersion: "spike", Migrations: []string{"001_create_works.sql"}, Database: backup.File{Path: "database.sqlite", Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}, Images: []backup.Image{}, OmittedImages: []backup.OmittedImage{}}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := backup.WriteArchive(context.Background(), &archive, manifest, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.zip")
	if err := os.WriteFile(source, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := application.PrepareRestore(context.Background(), source, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Cleanup(nil); err != nil {
			t.Error(err)
		}
	})
	return p, p.Files()[0]
}

func connect(t *testing.T, b *borrowedFS, mode string) (*sql.DB, *vfs.FS) {
	t.Helper()
	name, registration, err := vfs.New(b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registration.Close(); err != nil {
			t.Error(err)
		}
	})
	dsn := "file:" + logicalDB + "?" + url.Values{"vfs": {name}, "mode": {mode}}.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db, registration
}

func TestReadOnlyVFSOnRetainedPreparation(t *testing.T) {
	_, file := preparedDB(t, false)
	for _, mode := range []string{"ro", "rw"} {
		t.Run(mode, func(t *testing.T) {
			b := &borrowedFS{ctx: context.Background(), reader: file.Reader, size: file.Metadata.Size}
			db, registration := connect(t, b, mode)
			var got string
			if err := db.QueryRow("SELECT value FROM sample WHERE id=1").Scan(&got); err != nil || got != "synthetic" {
				t.Fatal(got, err)
			}
			if err := db.QueryRow("PRAGMA integrity_check").Scan(&got); err != nil || got != "ok" {
				t.Fatal(got, err)
			}
			rows, err := db.Query("PRAGMA foreign_key_check")
			if err != nil {
				t.Fatal(err)
			}
			if rows.Next() {
				t.Fatal("foreign key failure")
			}
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{"INSERT INTO sample(value) VALUES ('changed')", "UPDATE sample SET value='changed'", "DELETE FROM sample", "CREATE TABLE forbidden(x)", "PRAGMA journal_mode=OFF; INSERT INTO sample(value) VALUES ('changed')"} {
				if _, err := db.Exec(statement); err == nil {
					t.Fatal("write accepted", statement)
				} else {
					t.Logf("write refused: %v", err)
				}
			}
			if err := db.QueryRow("SELECT value FROM sample WHERE id=1").Scan(&got); err != nil || got != "synthetic" {
				t.Fatal(got, err)
			}
			if err := registration.Close(); !errors.Is(err, vfs.ErrInUse) {
				t.Fatal("premature VFS close", err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if active, opens, reads := b.counts(); active != 0 || opens == 0 || reads == 0 {
				t.Fatal(active, opens, reads)
			}
			if err := registration.Close(); err != nil {
				t.Fatal(err)
			}
			if err := b.failure(); err != nil {
				t.Fatal("unexpected read fault", err)
			}
			if _, err := file.Reader.ReadAt(make([]byte, 16), 0); err != nil {
				t.Fatal("underlying owner closed", err)
			}
			if _, err := b.Open("../fixture.sqlite"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
	// Compare all bytes after SQLite/VFS cleanup, with preparation still retained.
	data, err := io.ReadAll(io.NewSectionReader(file.Reader, 0, file.Metadata.Size+1))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	if int64(len(data)) != file.Metadata.Size || hex.EncodeToString(h[:]) != file.Metadata.SHA256 {
		t.Fatal("prepared content changed")
	}
}

func TestVFSCancellationAndReadFault(t *testing.T) {
	_, file := preparedDB(t, false)
	t.Run("query-cancel", func(t *testing.T) {
		b := &borrowedFS{ctx: context.Background(), reader: file.Reader, size: file.Metadata.Size}
		db, registration := connect(t, b, "ro")
		var n int
		if err := db.QueryRow("SELECT count(*) FROM sample").Scan(&n); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := db.QueryRowContext(ctx, "WITH RECURSIVE seq(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x<100000000) SELECT sum(x) FROM seq").Scan(&n)
		if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal(err, ctx.Err())
		}
		t.Logf("cancel result=%v; context=%v", err, ctx.Err())
		if err := db.QueryRow("SELECT count(*) FROM sample").Scan(&n); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if err := errors.Join(db.Close(), registration.Close()); err != nil {
			t.Fatal(err)
		}
	})
	for _, mode := range []string{"read-fault-record-only", "read-fault-stop", "cancelled-reader"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fault := errors.New("synthetic held-reader failure")
			b := &borrowedFS{ctx: ctx, reader: file.Reader, size: file.Metadata.Size, fault: fault}
			b.stopAfterFault = mode != "read-fault-record-only"
			if mode == "cancelled-reader" {
				b.fault = nil
				cancel()
				fault = context.Canceled
			}
			db, registration := connect(t, b, "ro")
			var n int
			sqlErr := db.QueryRow("SELECT count(*) FROM sample").Scan(&n)
			// The original Go cause may be replaced or ignored at the VFS boundary.
			// A production consumer must join its latched I/O cause and context too.
			combined := errors.Join(sqlErr, b.failure(), ctx.Err())
			if !errors.Is(combined, fault) {
				t.Fatal("cause lost", sqlErr, b.failure())
			}
			t.Logf("SQL result=%v; latched=%v; combined=%v", sqlErr, b.failure(), combined)
			if err := errors.Join(db.Close(), registration.Close()); err != nil {
				t.Fatal(err)
			}
			if active, _, _ := b.counts(); active != 0 {
				t.Fatal("view leak", active)
			}
			if _, err := file.Reader.ReadAt(make([]byte, 1), 0); err != nil {
				t.Fatal("owner lost", err)
			}
		})
	}
}

func TestVFSForeignKeyViolation(t *testing.T) {
	_, file := preparedDB(t, true)
	b := &borrowedFS{ctx: context.Background(), reader: file.Reader, size: file.Metadata.Size, stopAfterFault: true}
	db, registration := connect(t, b, "ro")
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("known foreign key violation not reported")
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(db.Close(), registration.Close(), b.failure()); err != nil {
		t.Fatal(err)
	}
}
