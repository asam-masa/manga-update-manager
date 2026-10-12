package coverfileboundaries

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// FILE_ID_INFO: ULONGLONG followed by FILE_ID_128. Probe only.
type identity128 struct {
	Volume uint64
	ID     [16]byte
}

type fingerprint struct {
	Identity identity128
	Size     int64
	Hash     [32]byte
}

func capture(file *os.File) (fingerprint, error) {
	var result fingerprint
	h := windows.Handle(file.Fd())
	ok, err := ordinaryFile(h)
	if err != nil {
		return fingerprint{}, err
	}
	if !ok {
		return fingerprint{}, os.ErrInvalid
	}
	err = windows.GetFileInformationByHandleEx(h, windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&result.Identity)), uint32(unsafe.Sizeof(result.Identity)))
	if err != nil {
		return fingerprint{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return fingerprint{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fingerprint{}, err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, file)
	if err != nil {
		return fingerprint{}, err
	}
	if n != info.Size() {
		return fingerprint{}, io.ErrUnexpectedEOF
	}
	result.Size = n
	copy(result.Hash[:], hash.Sum(nil))
	return result, nil
}

func matches(file *os.File, expected fingerprint) (bool, error) {
	actual, err := capture(file)
	return err == nil && actual == expected, err
}

func probeFile(t *testing.T, name string, access uint32) *os.File {
	t.Helper()
	h, err := openProbe(name, access, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(h), name)
	t.Cleanup(func() { f.Close() })
	return f
}

func TestIdentity128AndContentMatchAfterReopen(t *testing.T) {
	if unsafe.Sizeof(identity128{}) != 24 || unsafe.Offsetof(identity128{}.ID) != 8 {
		t.Fatal("unexpected FILE_ID_INFO layout")
	}
	name := filepath.Join(t.TempDir(), "synthetic.png")
	payload := []byte("synthetic bytes, not a PNG validation test")
	if err := os.WriteFile(name, payload, 0600); err != nil {
		t.Fatal(err)
	}
	f := probeFile(t, name, windows.GENERIC_READ)
	expected, err := capture(f)
	if err != nil {
		t.Fatal(err)
	}
	if expected.Size != int64(len(payload)) || expected.Hash != sha256.Sum256(payload) {
		t.Fatal("unexpected size or hash")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f = probeFile(t, name, windows.GENERIC_READ)
	ok, err := matches(f, expected)
	if err != nil || !ok {
		t.Fatalf("reopen: %v %v", ok, err)
	}
	expected.Size++
	if ok, err := matches(f, expected); err != nil || ok {
		t.Fatalf("wrong recorded size accepted: %v %v", ok, err)
	}
}

func TestSameContentDifferentFileAndChangedContentRejected(t *testing.T) {
	dir := t.TempDir()
	name, other := filepath.Join(dir, "original.png"), filepath.Join(dir, "other.png")
	for _, path := range []string{name, other} {
		if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := probeFile(t, name, windows.GENERIC_READ)
	expected, err := capture(f)
	if err != nil {
		t.Fatal(err)
	}
	g := probeFile(t, other, windows.GENERIC_READ)
	actual, err := capture(g)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Hash != expected.Hash || actual.Size != expected.Size || actual.Identity == expected.Identity {
		t.Fatal("different-file fixture does not isolate identity")
	}
	if ok, err := matches(g, expected); err != nil || ok {
		t.Fatalf("different file accepted: %v %v", ok, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("after!"), 0600); err != nil {
		t.Fatal(err)
	}
	f = probeFile(t, name, windows.GENERIC_READ)
	actual, err = capture(f)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Identity != expected.Identity || actual.Size != expected.Size || actual.Hash == expected.Hash {
		t.Fatal("changed-content fixture does not isolate hash")
	}
	if ok, err := matches(f, expected); err != nil || ok {
		t.Fatalf("changed content accepted: %v %v", ok, err)
	}
}

func TestCaptureFailureNeverMatchesOrRemovesFile(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{false: "write-only read failure", true: "closed handle"}[closed], func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "preserved.png")
			if err := os.WriteFile(name, []byte("preserved"), 0600); err != nil {
				t.Fatal(err)
			}
			f := probeFile(t, name, windows.GENERIC_WRITE)
			if closed {
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if ok, err := matches(f, fingerprint{}); err == nil || ok {
				t.Fatalf("unconfirmed file accepted: %v %v", ok, err)
			}
			f.Close()
			data, err := os.ReadFile(name)
			if err != nil || string(data) != "preserved" {
				t.Fatalf("file changed: %q %v", data, err)
			}
		})
	}
}
