//go:build windows

// Package restorepreparation explores handle lifetimes in t.TempDir only.
// Sentinel bytes are not SQLite/PNG data; this is not a restore implementation.
package restorepreparation

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"golang.org/x/sys/windows"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func finalName(f *os.File) (string, error) {
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n == 0 || n >= uint32(len(buf)) {
		return "", errors.New("name length")
	}
	return strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`), nil
}
func directory(t *testing.T) *platform.BackupWorkDirectory {
	t.Helper()
	if err := windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateDirectory2W").Find(); err != nil {
		t.Skipf("creation capability unverified: %v", err)
	}
	f, err := os.Open(t.TempDir())
	check(t, err)
	root, err := finalName(f)
	check(t, errors.Join(err, f.Close()))
	for _, leaf := range []string{"current-sentinel", "retired-sentinel"} {
		path := filepath.Join(root, leaf)
		check(t, os.WriteFile(path, []byte(leaf), 0600))
		t.Cleanup(func() {
			got, err := os.ReadFile(path)
			check(t, err)
			if string(got) != leaf {
				t.Error("protected sentinel changed")
			}
		})
	}
	d, err := platform.CreateBackupWorkDirectory(context.Background(), root)
	check(t, err)
	t.Cleanup(func() { check(t, d.Close()) })
	check(t, d.VerifyIdentity())
	return d
}

func TestExistingChildGuardCompatibility(t *testing.T) {
	d := directory(t)
	f, err := platform.CreateBackupTemp(context.Background(), filepath.Join(d.Path(), "payload.zip"))
	if err == nil {
		check(t, f.Close())
		t.Fatal("sharing assumptions changed; re-evaluate candidate")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("unexpected refusal: %v", err)
	}
	check(t, d.VerifyIdentity())
	r, err := d.DiscardEmpty()
	check(t, err)
	if !r.Absent || !d.ReleaseResult().Confirmed() {
		t.Fatal("directory release unconfirmed")
	}
	t.Log("existing child parent-guard reopens DELETE-held work directory: sharing violation; no fallback adopted")
}

// Candidate only: keep the parent's existing owner instead of reacquiring it.
// The test constructs every leaf itself; archive entry names are never joined.
type payload struct {
	f    *os.File
	path string
	id   platform.BackupTempIdentity
}

func fileID(f *os.File) (platform.BackupTempIdentity, error) {
	var id platform.BackupTempIdentity
	err := windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id)))
	return id, err
}
func newPayload(t *testing.T, d *platform.BackupWorkDirectory, leaf string) *payload {
	t.Helper()
	check(t, d.VerifyIdentity())
	if filepath.Base(leaf) != leaf {
		t.Fatal("fixture leaf only")
	}
	path := filepath.Join(d.Path(), leaf)
	p, err := windows.UTF16PtrFromString(path)
	check(t, err)
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE, 0, nil, windows.CREATE_NEW, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	check(t, err)
	f := os.NewFile(uintptr(h), path)
	t.Cleanup(func() {
		if f.Fd() != ^uintptr(0) {
			check(t, f.Close())
		}
	})
	id, err := fileID(f)
	check(t, err)
	return &payload{f, path, id}
}
func (p *payload) discard(d *platform.BackupWorkDirectory) error {
	if err := d.VerifyIdentity(); err != nil {
		return err
	}
	id, err := fileID(p.f)
	if err != nil {
		return err
	}
	name, err := finalName(p.f)
	if err != nil {
		return err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(p.f.Fd()), &info); err != nil {
		return err
	}
	if id != p.id || !strings.EqualFold(name, p.path) || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return errors.New("ownership unconfirmed")
	}
	var remove byte = 1
	if err = windows.SetFileInformationByHandle(windows.Handle(p.f.Fd()), windows.FileDispositionInfo, &remove, 1); err != nil {
		return err
	}
	if err = p.f.Close(); err != nil {
		return err
	}
	_, err = os.Lstat(p.path)
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("absence unconfirmed")
	}
	return nil
}

func archive(t *testing.T) ([]byte, backup.Manifest) {
	t.Helper()
	data := map[string][]byte{"database.sqlite": []byte("synthetic database"), "covers/" + strings.Repeat("1", 32) + ".png": []byte("synthetic image")}
	im := "covers/" + strings.Repeat("1", 32) + ".png"
	m := backup.Manifest{FormatVersion: 1, CreatedAt: "2026-11-20T00:00:00Z", AppVersion: "spike", Migrations: []string{"001_create_works.sql"},
		Database: backup.File{Path: "database.sqlite", Size: int64(len(data["database.sqlite"])), SHA256: fmt.Sprintf("%x", sha256.Sum256(data["database.sqlite"]))},
		Images:   []backup.Image{{ID: strings.Repeat("1", 32), Path: im, Size: int64(len(data[im])), SHA256: fmt.Sprintf("%x", sha256.Sum256(data[im]))}}, OmittedImages: []backup.OmittedImage{}}
	meta, err := json.Marshal(m)
	check(t, err)
	var b bytes.Buffer
	check(t, backup.WriteArchive(context.Background(), &b, meta, func(name string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data[name])), nil }))
	return b.Bytes(), m
}

func TestHeldInputAndPayloadLifecycle(t *testing.T) {
	d := directory(t)
	data, m := archive(t)
	input := filepath.Join(filepath.Dir(d.Path()), "input.zip")
	check(t, os.WriteFile(input, data, 0600))
	p, err := windows.UTF16PtrFromString(input)
	check(t, err)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	check(t, err)
	f := os.NewFile(uintptr(h), input)
	defer func() { check(t, f.Close()) }()
	_, err = backup.ValidateArchive(f, int64(len(data)))
	check(t, err)
	if err = os.WriteFile(input, []byte("changed"), 0600); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("input write not denied: %v", err)
	}
	if err = os.Rename(input, input+"-moved"); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("input rename not denied: %v", err)
	}
	z, err := zip.NewReader(f, int64(len(data)))
	check(t, err)
	var outputs []*payload
	for _, entry := range z.File {
		if entry.Name == "manifest.json" {
			continue
		}
		out := newPayload(t, d, fmt.Sprintf("payload-%d", len(outputs)))
		r, err := entry.Open()
		check(t, err)
		n, err := io.Copy(out.f, io.LimitReader(r, 65))
		check(t, errors.Join(err, r.Close()))
		if n > 64 {
			t.Fatal("synthetic payload limit exceeded")
		}
		check(t, out.f.Sync())
		buf := make([]byte, n)
		_, err = out.f.ReadAt(buf, 0)
		check(t, err)
		want := m.Database.SHA256
		if entry.Name != m.Database.Path {
			want = m.Images[0].SHA256
		}
		if fmt.Sprintf("%x", sha256.Sum256(buf)) != want {
			t.Fatal("payload changed")
		}
		outputs = append(outputs, out)
	}
	if len(outputs) != 2 {
		t.Fatal("payload count")
	}
	if err := os.Rename(d.Path(), d.Path()+"-moved"); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("directory move: %v", err)
	}
	for _, out := range outputs {
		check(t, out.discard(d))
	}
	r, err := d.DiscardEmpty()
	check(t, err)
	if !r.Absent || !d.ReleaseResult().Confirmed() {
		t.Fatal("release unconfirmed")
	}
	got, err := os.ReadFile(input)
	check(t, err)
	if !bytes.Equal(got, data) {
		t.Fatal("input changed")
	}
	t.Log("input held through validation/extraction; two payload hashes; same-handle owned cleanup and parent release: verified")
}

func TestPartialPreparationAndPreservation(t *testing.T) {
	for _, mode := range []string{"cancel", "write-failure", "identity-mismatch", "unexpected-file", "released-handle"} {
		t.Run(mode, func(t *testing.T) {
			d := directory(t)
			out := newPayload(t, d, "partial")
			_, err := out.f.Write([]byte("partial sentinel"))
			check(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "cancel":
				cancel()
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatal("fixture cancellation")
				}
			case "write-failure": // Model: the producer failed after the prefix above; no OS fault is injected.
			case "identity-mismatch":
				out.id.FileID[0] ^= 1
			case "unexpected-file":
				check(t, os.WriteFile(filepath.Join(d.Path(), "unowned"), []byte("keep"), 0600))
			case "released-handle":
				check(t, out.f.Close())
			}
			err = out.discard(d)
			if mode == "identity-mismatch" || mode == "released-handle" {
				if err == nil {
					t.Fatal("unconfirmed ownership deleted")
				}
				if mode != "released-handle" {
					check(t, out.f.Close())
				}
				got, e := os.ReadFile(out.path)
				check(t, e)
				if string(got) != "partial sentinel" {
					t.Fatal("unconfirmed payload changed")
				}
				check(t, d.Close())
				next, e := platform.CreateBackupWorkDirectory(context.Background(), filepath.Dir(d.Path()))
				check(t, e)
				t.Cleanup(func() { check(t, next.Close()) })
				if next.Path() == d.Path() {
					t.Fatal("residual reused")
				}
				r, e := next.DiscardEmpty()
				check(t, e)
				if !r.Absent {
					t.Fatal("new empty directory remained")
				}
				return
			}
			check(t, err)
			r, err := d.DiscardEmpty()
			if mode == "unexpected-file" {
				if !errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) || r.MarkedForDeletion {
					t.Fatalf("unexpected child: %+v %v", r, err)
				}
				got, e := os.ReadFile(filepath.Join(d.Path(), "unowned"))
				check(t, e)
				if string(got) != "keep" {
					t.Fatal("unowned changed")
				}
				check(t, d.Close())
				if !d.ReleaseResult().Confirmed() {
					t.Fatal("release unconfirmed")
				}
			} else {
				check(t, err)
				if !r.Absent {
					t.Fatal("directory remained")
				}
			}
		})
	}
}
