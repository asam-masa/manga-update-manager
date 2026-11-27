package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"golang.org/x/sys/windows"
)

type restoreFixture struct {
	root, source string
	archive      []byte
	manifest     backup.Manifest
	bodies       map[string][]byte
}

func newRestoreFixture(t *testing.T) restoreFixture {
	return newRestoreFixtureWithDB(t, []byte("synthetic database"))
}

func newRestoreFixtureWithDB(t *testing.T, db []byte) restoreFixture {
	return newRestoreFixtureWithMigrations(t, db, []string{"001_create_works.sql"})
}

func newRestoreFixtureWithMigrations(t *testing.T, db []byte, migrations []string) restoreFixture {
	return newRestoreFixtureWithImages(t, db, migrations, []byte("synthetic image"), nil)
}

func newRestoreFixtureWithImages(t *testing.T, db []byte, migrations []string, im []byte, change func(*backup.Manifest)) restoreFixture {
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
	hash := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	id := strings.Repeat("1", 32)
	omitted := strings.Repeat("2", 32)
	m := backup.Manifest{FormatVersion: 1, CreatedAt: "2026-11-22T00:00:00Z", AppVersion: "fixture", Migrations: []string{"001_create_works.sql"}, Database: backup.File{Path: "database.sqlite", Size: int64(len(db)), SHA256: hash(db)}, Images: []backup.Image{{ID: id, Path: "covers/" + id + ".png", Size: int64(len(im)), SHA256: hash(im)}}, OmittedImages: []backup.OmittedImage{{ID: omitted, Path: "covers/" + omitted + ".png", Reason: "missing"}}}
	bodies := map[string][]byte{m.Database.Path: db, m.Images[0].Path: im}
	m.Migrations = migrations
	if change != nil {
		change(&m)
	}
	metadata, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := backup.WriteArchive(context.Background(), &archive, metadata, func(name string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(bodies[name])), nil }); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.zip")
	if err := os.WriteFile(source, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"current.sqlite", "retired.sqlite"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return restoreFixture{root, source, archive.Bytes(), m, bodies}
}
func (f restoreFixture) assertProtected(t *testing.T) {
	t.Helper()
	if got, err := os.ReadFile(f.source); err != nil || !bytes.Equal(got, f.archive) {
		t.Fatal("source changed", err)
	}
	for _, name := range []string{"current.sqlite", "retired.sqlite"} {
		if got, err := os.ReadFile(filepath.Join(f.root, name)); err != nil || string(got) != name {
			t.Fatal("protected file changed", name, err)
		}
	}
}

func TestPrepareRestoreNativeOwnership(t *testing.T) {
	f := newRestoreFixture(t)
	p, err := PrepareRestore(context.Background(), f.source, f.root)
	if err != nil {
		t.Fatal(err)
	}
	files := p.Files()
	if len(files) != 2 || !reflect.DeepEqual(p.Manifest(), f.manifest) {
		t.Fatal(files, p.Manifest())
	}
	for i, file := range files {
		got, err := io.ReadAll(io.NewSectionReader(file.Reader, 0, file.Metadata.Size+1))
		if err != nil || !bytes.Equal(got, f.bodies[file.Metadata.Path]) {
			t.Fatal(file.Metadata, err)
		}
		if _, ok := file.Reader.(io.Closer); ok {
			t.Fatal("borrow exposed Close")
		}
		if i == 0 && file.ImageID != "" || i == 1 && file.ImageID != f.manifest.Images[0].ID {
			t.Fatal(file)
		}
	}
	// Public snapshots cannot change the retained manifest or mapping.
	m := p.Manifest()
	m.Migrations[0] = "other"
	m.Images[0].ID = "other"
	m.OmittedImages[0].Reason = "other"
	files[0].Metadata.Path = "other"
	if !reflect.DeepEqual(p.Manifest(), f.manifest) || p.Files()[0].Metadata.Path != "database.sqlite" {
		t.Fatal("snapshot aliases owner")
	}
	// Source is released before output ownership is returned.
	w, err := os.OpenFile(f.source, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal("source still held", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	sqlUse, err := p.BeginSQLUse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Cleanup(nil); !errors.Is(err, ErrPreparedRestoreSQLInUse) {
		t.Fatal(err)
	}
	got := make([]byte, len(f.bodies[f.manifest.Database.Path]))
	if _, err := p.Files()[0].Reader.ReadAt(got, 0); err != nil || !bytes.Equal(got, f.bodies[f.manifest.Database.Path]) {
		t.Fatal("SQL-use rejection changed retained database", err)
	}
	if err := sqlUse.End(true, nil); err != nil {
		t.Fatal(err)
	}
	cleanup, err := p.Cleanup(nil)
	if err != nil || !cleanup.Parent.Absent || !cleanup.ParentRelease.Confirmed() || len(cleanup.Children) != 2 {
		t.Fatal(cleanup, err)
	}
	for _, c := range cleanup.Children {
		if !c.Discard.Absent || !c.Release.Confirmed() {
			t.Fatal(c)
		}
	}
	if _, err := p.Files()[0].Reader.ReadAt(make([]byte, 1), 0); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	again, err := p.Cleanup(nil)
	if !errors.Is(err, platform.ErrRestorePreparationCleanupAttempted) || !reflect.DeepEqual(again, cleanup) {
		t.Fatal(err)
	}
	f.assertProtected(t)
}

type shortRestoreWriter struct{ io.Writer }

func (w shortRestoreWriter) Write(b []byte) (int, error) { return w.Writer.Write(b[:len(b)-1]) }

type failedRestoreSync struct {
	*platform.RestorePreparationFile
	fault error
}

func (f failedRestoreSync) Sync() error { return f.fault }

type failedRestoreRead struct {
	*platform.RestorePreparationFile
	fault error
}

func (f failedRestoreRead) ReadAt(b []byte, off int64) (int, error) {
	n, _ := f.RestorePreparationFile.ReadAt(b, off)
	return n, f.fault
}

func TestPrepareRestoreFailuresAndCancellation(t *testing.T) {
	for _, mode := range []string{"verify-before", "validate", "verify-extract", "create", "partial-create", "late-crc", "short-write", "sync", "readback", "verify-final", "source-close", "source-unconfirmed", "cancel-validate", "cancel-create", "cancel-extract", "cancel-readback", "cancel-close", "cleanup-error", "cleanup-unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			f := newRestoreFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fault := errors.New("pipeline fault")
			cleanupFault := errors.New("cleanup fault")
			ops := restorePreparationOperations()
			base := ops
			creates, checks, closes, cleanups := 0, 0, 0, 0
			ops.verify = func(ctx context.Context, s *platform.RestoreSource) error {
				checks++
				if mode == "verify-before" && checks == 1 || mode == "verify-extract" && checks == 2 || mode == "verify-final" && checks == 3 {
					return fault
				}
				return base.verify(ctx, s)
			}
			ops.create = func(ctx context.Context, dir string) (*platform.RestorePreparation, error) {
				creates++
				if mode == "create" {
					return nil, fault
				}
				p, err := base.create(ctx, dir)
				if mode == "cancel-create" {
					cancel()
				}
				if err == nil && mode == "partial-create" {
					return p, fault
				}
				return p, err
			}
			if mode == "validate" {
				ops.validate = func(io.ReaderAt, int64) (backup.Manifest, error) { return backup.Manifest{}, fault }
			}
			if mode == "cancel-validate" {
				ops.validate = func(r io.ReaderAt, size int64) (backup.Manifest, error) {
					m, err := base.validate(r, size)
					cancel()
					return m, err
				}
			}
			ops.extract = func(ctx context.Context, r io.ReaderAt, size int64, m backup.Manifest, open func(backup.File) (io.Writer, error)) error {
				if mode == "late-crc" {
					b := bytes.Clone(f.archive)
					pos := bytes.Index(b, []byte("synthetic image"))
					if pos < 0 {
						t.Fatal("payload missing")
					}
					b[pos] ^= 1
					r = bytes.NewReader(b)
				}
				if mode == "short-write" {
					original := open
					open = func(meta backup.File) (io.Writer, error) {
						w, err := original(meta)
						if err != nil {
							return nil, err
						}
						return shortRestoreWriter{w}, nil
					}
				}
				err := base.extract(ctx, r, size, m, open)
				if mode == "cancel-extract" {
					cancel()
				}
				return err
			}
			ops.readback = func(ctx context.Context, file *platform.RestorePreparationFile, meta backup.File) error {
				digest, _ := hex.DecodeString(meta.SHA256)
				if mode == "sync" {
					return VerifyRestorePreparationFile(ctx, failedRestoreSync{file, fault}, meta.Size, [32]byte(digest))
				}
				if mode == "readback" {
					return VerifyRestorePreparationFile(ctx, failedRestoreRead{file, fault}, meta.Size, [32]byte(digest))
				}
				err := base.readback(ctx, file, meta)
				if mode == "cancel-readback" {
					cancel()
				}
				return err
			}
			ops.close = func(s *platform.RestoreSource) (platform.RestoreSourceReleaseResult, error) {
				closes++
				r, err := base.close(s)
				if mode == "source-close" {
					err = errors.Join(err, fault)
				}
				if mode == "source-unconfirmed" {
					r.FileClosed = false
				}
				if mode == "cancel-close" {
					cancel()
				}
				return r, err
			}
			ops.cleanup = func(p *platform.RestorePreparation, err error) (platform.RestorePreparationCleanupResult, error) {
				cleanups++
				r, e := base.cleanup(p, err)
				if mode == "cleanup-error" {
					e = errors.Join(e, cleanupFault)
				}
				if mode == "cleanup-unconfirmed" {
					r.ParentRelease.HandleClosed = false
				}
				return r, e
			}
			if mode == "cleanup-error" || mode == "cleanup-unconfirmed" {
				ops.readback = func(context.Context, *platform.RestorePreparationFile, backup.File) error { return fault }
			}
			p, err := prepareRestore(ctx, f.source, f.root, ops)
			var detail *RestorePrepareError
			if p != nil || !errors.Is(err, ErrRestorePrepare) || !errors.As(err, &detail) || closes != 1 {
				t.Fatal(p, err, closes)
			}
			if strings.HasPrefix(mode, "cancel-") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancel lost", err)
			}
			if mode == "short-write" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
			if mode == "source-unconfirmed" && !errors.Is(err, platform.ErrRestoreSourceRelease) {
				t.Fatal(err)
			}
			if mode == "cleanup-error" && (!errors.Is(err, fault) || !errors.Is(err, cleanupFault) || !errors.Is(detail.OriginalErr, fault)) {
				t.Fatal(detail)
			}
			if mode == "cleanup-unconfirmed" && !errors.Is(err, platform.ErrRestorePreparationCleanup) {
				t.Fatal(err)
			}
			switch mode {
			case "verify-before", "validate", "verify-extract", "create", "partial-create", "sync", "readback", "verify-final", "source-close":
				if !errors.Is(err, fault) {
					t.Fatal("stage cause lost", err)
				}
			}
			noOutput := mode == "verify-before" || mode == "validate" || mode == "verify-extract" || mode == "create" || mode == "cancel-validate"
			if noOutput {
				if detail.Cleanup != nil || cleanups != 0 {
					t.Fatal(detail)
				}
			} else if detail.Cleanup == nil || cleanups != 1 || !detail.Cleanup.Parent.Absent {
				t.Fatal(detail, cleanups)
			}
			if mode != "source-unconfirmed" && !detail.SourceRelease.Confirmed() {
				t.Fatal(detail)
			}
			if mode == "verify-before" || mode == "validate" || mode == "verify-extract" || mode == "cancel-validate" {
				if creates != 0 {
					t.Fatal("prepared before validation", creates)
				}
			}
			f.assertProtected(t)
			entries, e := os.ReadDir(f.root)
			if e != nil || len(entries) != 3 {
				t.Fatal("temporary output left", entries, e)
			}
		})
	}
}

func TestPrepareRestoreEarlyFailures(t *testing.T) {
	f := newRestoreFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := PrepareRestore(ctx, f.source, f.root)
	var detail *RestorePrepareError
	if p != nil || !errors.Is(err, context.Canceled) || !errors.As(err, &detail) || detail.SourceRelease.CloseAttempted || detail.Cleanup != nil {
		t.Fatal(p, err)
	}
	p, err = PrepareRestore(context.Background(), filepath.Join(f.root, "missing.zip"), f.root)
	if p != nil || !errors.As(err, &detail) || detail.Cleanup != nil || !detail.SourceRelease.Confirmed() {
		t.Fatal(p, err)
	}
	f.assertProtected(t)
}

func TestPrepareRestoreRejectsInvalidWholeArchiveBeforeOutput(t *testing.T) {
	f := newRestoreFixture(t)
	bad := bytes.Clone(f.archive)
	pos := bytes.Index(bad, []byte("synthetic image"))
	if pos < 0 {
		t.Fatal("missing fixture payload")
	}
	bad[pos] ^= 1
	if err := os.WriteFile(f.source, bad, 0600); err != nil {
		t.Fatal(err)
	}
	f.archive = bad
	p, err := PrepareRestore(context.Background(), f.source, f.root)
	var detail *RestorePrepareError
	if p != nil || !errors.Is(err, backup.ErrArchive) || !errors.As(err, &detail) || detail.Cleanup != nil || !detail.SourceRelease.Confirmed() {
		t.Fatal(p, err)
	}
	entries, err := os.ReadDir(f.root)
	if err != nil || len(entries) != 3 {
		t.Fatal(entries, err)
	}
	f.assertProtected(t)
}

func TestPrepareRestoreDatabaseOnly(t *testing.T) {
	f := newRestoreFixture(t)
	f.manifest.Images = []backup.Image{}
	f.manifest.OmittedImages = []backup.OmittedImage{}
	metadata, err := json.Marshal(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	err = backup.WriteArchive(context.Background(), &archive, metadata, func(name string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(f.bodies[name])), nil })
	if err != nil {
		t.Fatal(err)
	}
	f.archive = archive.Bytes()
	if err := os.WriteFile(f.source, f.archive, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := PrepareRestore(context.Background(), f.source, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files()) != 1 || p.Files()[0].ImageID != "" || !reflect.DeepEqual(p.Manifest(), f.manifest) {
		t.Fatal(p.Files())
	}
	if _, err := p.Cleanup(nil); err != nil {
		t.Fatal(err)
	}
	f.assertProtected(t)
}
