//go:build windows

// Package restoresource probes sharing on synthetic t.TempDir files only.
// Payloads are not application backups. This is not a production input API.
package restoresource

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

const allShares = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
const payload = "synthetic ZIP-like payload, not restore data"

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func open(name string, access, share uint32) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, access, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root, err := windows.UTF16PtrFromString(filepath.VolumeName(dir) + `\`)
	check(t, err)
	var fs [32]uint16
	check(t, windows.GetVolumeInformation(root, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))))
	if windows.UTF16ToString(fs[:]) != "NTFS" {
		t.Skip("probe limited to local NTFS")
	}
	v := windows.RtlGetVersion()
	t.Logf("Windows %d.%d.%d; NTFS", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	name := filepath.Join(dir, "source.zip")
	check(t, os.WriteFile(name, []byte(payload), 0600))
	return name
}

func closeFile(t *testing.T, f *os.File) {
	t.Helper()
	err := f.Close()
	if !errors.Is(err, os.ErrClosed) {
		check(t, err)
	}
}

func denied(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("expected sharing/access refusal, got %v", err)
	}
	t.Logf("refused: %v", err)
}

func sameBytes(t *testing.T, f *os.File) {
	t.Helper()
	got := make([]byte, len(payload))
	_, err := f.ReadAt(got, 0)
	check(t, err)
	if string(got) != payload {
		t.Fatalf("changed bytes: %q", got)
	}
	got, err = io.ReadAll(io.NewSectionReader(f, 0, int64(len(payload))))
	check(t, err)
	if string(got) != payload {
		t.Fatal("section reader differs")
	}
}

func TestHeldSourceRefusesChangesAndAlias(t *testing.T) {
	name := fixture(t)
	alias := name + ".alias"
	check(t, os.Link(name, alias))
	f, err := open(name, windows.GENERIC_READ, windows.FILE_SHARE_READ)
	check(t, err)
	t.Cleanup(func() { closeFile(t, f) })
	sameBytes(t, f)
	for _, path := range []string{name, alias} {
		writer, err := open(path, windows.GENERIC_WRITE, allShares)
		if writer != nil {
			closeFile(t, writer)
		}
		denied(t, err)
	}
	denied(t, os.Rename(name, name+".moved"))
	denied(t, os.Remove(name))
	// Sharing pins the opened link, not necessarily every hard-link name.
	if err = os.Rename(alias, alias+".moved"); err == nil {
		alias += ".moved"
		t.Log("LIMIT: another hard-link name can be renamed while the source is held")
	} else {
		denied(t, err)
	}
	aliasRemoved := false
	if err = os.Remove(alias); err == nil {
		aliasRemoved = true
		t.Log("LIMIT: another hard-link name can be removed while the source is held")
	} else {
		denied(t, err)
	}
	sameBytes(t, f)
	check(t, f.Close())
	writer, err := open(name, windows.GENERIC_WRITE, allShares)
	check(t, err)
	_, err = writer.WriteAt([]byte("released"), 0)
	check(t, errors.Join(err, writer.Close()))
	check(t, os.Rename(name, name+".moved"))
	check(t, os.Remove(name+".moved"))
	if !aliasRemoved {
		check(t, os.Remove(alias))
	}
}

func TestSourceWithPriorHandles(t *testing.T) {
	for _, access := range []uint32{windows.GENERIC_READ, windows.GENERIC_WRITE, windows.DELETE} {
		t.Run(map[uint32]string{windows.GENERIC_READ: "reader", windows.GENERIC_WRITE: "writer", windows.DELETE: "delete"}[access], func(t *testing.T) {
			name := fixture(t)
			prior, err := open(name, access, allShares)
			check(t, err)
			t.Cleanup(func() { closeFile(t, prior) })
			f, err := open(name, windows.GENERIC_READ, windows.FILE_SHARE_READ)
			if f != nil {
				defer closeFile(t, f)
			}
			if access == windows.GENERIC_READ {
				check(t, err)
				sameBytes(t, f)
			} else {
				denied(t, err)
			}
		})
	}
}

func TestSourceParentRenameObservation(t *testing.T) {
	name := fixture(t)
	parent := filepath.Join(filepath.Dir(name), "parent")
	check(t, os.Mkdir(parent, 0700))
	newName := filepath.Join(parent, "source.zip")
	check(t, os.Rename(name, newName))
	f, err := open(newName, windows.GENERIC_READ, windows.FILE_SHARE_READ)
	check(t, err)
	defer closeFile(t, f)
	err = os.Rename(parent, parent+".moved")
	if err != nil {
		denied(t, err)
	} else {
		t.Log("parent rename allowed; original path no longer locates the input")
	}
	sameBytes(t, f) // Never reopen the old path, whether rename succeeded or not.
}

func TestWritableMappingObservation(t *testing.T) {
	for _, share := range []uint32{0, windows.FILE_SHARE_READ} {
		t.Run(map[uint32]string{0: "exclusive", windows.FILE_SHARE_READ: "read-shared"}[share], func(t *testing.T) {
			name := fixture(t)
			prior, err := open(name, windows.GENERIC_READ|windows.GENERIC_WRITE, allShares)
			check(t, err)
			t.Cleanup(func() { closeFile(t, prior) })
			mapping, err := windows.CreateFileMapping(windows.Handle(prior.Fd()), nil, windows.PAGE_READWRITE, 0, 0, nil)
			check(t, err)
			defer func() { check(t, windows.CloseHandle(mapping)) }()
			addr, err := windows.MapViewOfFile(mapping, windows.FILE_MAP_WRITE, 0, 0, 0)
			check(t, err)
			defer func() { check(t, windows.UnmapViewOfFile(addr)) }()
			// Confirm the view is writable before testing the reader acquisition.
			writeMappedByte(t, addr, 'X')
			var before [1]byte
			_, err = prior.ReadAt(before[:], 0)
			check(t, err)
			if before[0] != 'X' {
				t.Fatal("writable view not confirmed")
			}
			writeMappedByte(t, addr, payload[0])
			check(t, prior.Close()) // Keep only the writable mapping/view alive.
			f, err := open(name, windows.GENERIC_READ, share)
			if err != nil {
				denied(t, err)
				t.Log("reader acquisition refused with a prior writable mapping")
				return
			}
			defer closeFile(t, f)
			writeMappedByte(t, addr, 'X')
			var got [1]byte
			_, err = f.ReadAt(got[:], 0)
			check(t, err)
			if got[0] != 'X' {
				t.Fatalf("mapping/read coherence unconfirmed: %q", got)
			}
			t.Log("LIMIT: read handle acquired, but prior writable mapping changed its bytes")
		})
	}
}

// Write only one byte into this process's live, writable synthetic mapping.
func writeMappedByte(t *testing.T, addr uintptr, value byte) {
	t.Helper()
	var written uintptr
	check(t, windows.WriteProcessMemory(windows.CurrentProcess(), addr, &value, 1, &written))
	if written != 1 {
		t.Fatal("short mapped write", written)
	}
	check(t, windows.FlushViewOfFile(addr, 1))
}

// A sequencing model only: no product error/result contract is defined here.
func acquire(ctx context.Context, name string, acquired func(), close func(*os.File) error) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := open(name, windows.GENERIC_READ, windows.FILE_SHARE_READ)
	if err != nil {
		return nil, err
	}
	acquired()
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, close(f))
	}
	return f, nil
}

func TestSourceCancellationAndReleaseModel(t *testing.T) {
	for _, phase := range []string{"before", "after", "close-fault", "missing"} {
		t.Run(phase, func(t *testing.T) {
			name := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "before" {
				cancel()
			}
			if phase == "missing" {
				name += ".missing"
			}
			fault := errors.New("modeled Close uncertainty")
			opens, closes := 0, 0
			f, err := acquire(ctx, name, func() { opens++; cancel() }, func(f *os.File) error {
				closes++
				err := f.Close()
				if phase == "close-fault" {
					return errors.Join(err, fault)
				}
				return err
			})
			if f != nil {
				closeFile(t, f)
				t.Fatal("returned a handle after cancellation")
			}
			if phase == "missing" {
				if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			want := 0
			if phase == "after" || phase == "close-fault" {
				want = 1
			}
			if opens != want || closes != want || errors.Is(err, fault) != (phase == "close-fault") {
				t.Fatal(err, opens, closes)
			}
			if phase != "missing" {
				writer, err := open(name, windows.GENERIC_WRITE, allShares)
				check(t, err)
				closeFile(t, writer)
			}
		})
	}
}
