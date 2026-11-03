package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"reflect"
	"testing"
)

type zipPart struct {
	name   string
	data   []byte
	mode   fs.FileMode
	method uint16
}

func archiveFixture(t *testing.T, method uint16) ([]byte, Manifest) {
	t.Helper()
	m := fixture()
	db, im := []byte("synthetic database"), []byte("synthetic image")
	m.Database.Size, m.Database.SHA256 = int64(len(db)), fmt.Sprintf("%x", sha256.Sum256(db))
	i := image(1, int64(len(im)))
	i.SHA256 = fmt.Sprintf("%x", sha256.Sum256(im))
	m.Images = []Image{i}
	return zipBytes(t, []zipPart{{"manifest.json", []byte(encoded(t, m)), 0600, method}, {"database.sqlite", db, 0600, method}, {i.Path, im, 0600, method}}), m
}

func zipBytes(t *testing.T, parts []zipPart) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, p := range parts {
		h := &zip.FileHeader{Name: p.name, Method: p.method}
		h.SetMode(p.mode)
		out, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write(p.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func rejectArchive(t *testing.T, data []byte, cause error) {
	t.Helper()
	m, err := ValidateArchive(bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, ErrArchive) || !reflect.DeepEqual(m, Manifest{}) || (cause != nil && !errors.Is(err, cause)) {
		t.Fatalf("partial result or error mismatch: %+v %v", m, err)
	}
}

func TestArchiveValid(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		data, want := archiveFixture(t, method)
		got, err := ValidateArchive(bytes.NewReader(data), int64(len(data)))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, err)
		}
	}
	m := fixture()
	db := []byte("db")
	m.Database.Size = 2
	m.Database.SHA256 = fmt.Sprintf("%x", sha256.Sum256(db))
	m.OmittedImages = []OmittedImage{{image(2, 1).ID, image(2, 1).Path, "missing"}}
	data := zipBytes(t, []zipPart{{"manifest.json", []byte(encoded(t, m)), 0600, zip.Store}, {"database.sqlite", db, 0600, zip.Store}})
	if _, err := ValidateArchive(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRejectsStructure(t *testing.T) {
	valid, _ := archiveFixture(t, zip.Store)
	for name, change := range map[string]func([]byte) []byte{
		"truncated": func(b []byte) []byte { return b[:len(b)-1] },
		"trailing":  func(b []byte) []byte { return append(b, 0) },
		"multi disk": func(b []byte) []byte {
			p := bytes.LastIndex(b, []byte("PK\x05\x06"))
			le.PutUint16(b[p+4:], 1)
			return b
		},
		"count": func(b []byte) []byte {
			p := bytes.LastIndex(b, []byte("PK\x05\x06"))
			le.PutUint16(b[p+8:], MaxEntries+1)
			le.PutUint16(b[p+10:], MaxEntries+1)
			return b
		},
		"lying count": func(b []byte) []byte {
			p := bytes.LastIndex(b, []byte("PK\x05\x06"))
			le.PutUint16(b[p+8:], 2)
			le.PutUint16(b[p+10:], 2)
			return b
		},
		"directory size": func(b []byte) []byte {
			p := bytes.LastIndex(b, []byte("PK\x05\x06"))
			le.PutUint32(b[p+12:], uint32(MaxDirectoryBytes+1))
			return b
		},
		"encrypted": func(b []byte) []byte {
			p := bytes.Index(b, []byte("PK\x01\x02"))
			le.PutUint16(b[p+8:], le.Uint16(b[p+8:])|1)
			return b
		},
		"method":      func(b []byte) []byte { p := bytes.Index(b, []byte("PK\x01\x02")); le.PutUint16(b[p+10:], 99); return b },
		"local name":  func(b []byte) []byte { b[30] = 'X'; return b },
		"local flags": func(b []byte) []byte { b[6] ^= 1; return b },
		"directory oversized manifest": func(b []byte) []byte {
			p := bytes.Index(b, []byte("PK\x01\x02"))
			le.PutUint32(b[p+24:], uint32(MaxManifestBytes+1))
			return b
		},
		"directory oversized payload": func(b []byte) []byte {
			p := bytes.Index(b, []byte("PK\x01\x02"))
			p += 46 + int(le.Uint16(b[p+28:])) + int(le.Uint16(b[p+30:])) + int(le.Uint16(b[p+32:]))
			le.PutUint32(b[p+24:], uint32(MaxDatabaseBytes+1))
			return b
		},
	} {
		t.Run(name, func(t *testing.T) { rejectArchive(t, change(bytes.Clone(valid)), nil) })
	}
	for _, name := range []string{"../database.sqlite", "/database.sqlite", `covers\bad.png`, "C:/data", "covers/", "unknown", "covers/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.png"} {
		t.Run(name, func(t *testing.T) {
			rejectArchive(t, zipBytes(t, []zipPart{{"manifest.json", []byte("{}"), 0600, zip.Store}, {name, nil, 0600, zip.Store}}), nil)
		})
	}
	for _, mode := range []fs.FileMode{fs.ModeSymlink | 0600, fs.ModeDir | 0700, fs.ModeNamedPipe | 0600} {
		rejectArchive(t, zipBytes(t, []zipPart{{"manifest.json", []byte("{}"), mode, zip.Store}, {"database.sqlite", []byte("db"), 0600, zip.Store}}), nil)
	}
}

func TestArchiveRejectsMappingAndContents(t *testing.T) {
	data, m := archiveFixture(t, zip.Store)
	// Rebuild archives with internally valid ZIP checksums, but invalid manifests.
	for name, change := range map[string]func(*Manifest){
		"hash":           func(m *Manifest) { m.Database.SHA256 = fmt.Sprintf("%064d", 0) },
		"size":           func(m *Manifest) { m.Database.Size++ },
		"missing image":  func(m *Manifest) { m.Images[0] = image(2, m.Images[0].Size) },
		"unlisted image": func(m *Manifest) { m.Images = []Image{} },
		"omitted payload": func(m *Manifest) {
			i := m.Images[0]
			m.Images = []Image{}
			m.OmittedImages = []OmittedImage{{i.ID, i.Path, "missing"}}
		},
		"manifest": func(m *Manifest) { m.FormatVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			copy := m
			copy.Images = append([]Image{}, m.Images...)
			change(&copy)
			rejectArchive(t, zipBytes(t, []zipPart{{"manifest.json", []byte(encoded(t, copy)), 0600, zip.Store}, {"database.sqlite", []byte("synthetic database"), 0600, zip.Store}, {m.Images[0].Path, []byte("synthetic image"), 0600, zip.Store}}), nil)
		})
	}
	parts := []zipPart{{"manifest.json", []byte(encoded(t, m)), 0600, zip.Store}, {"database.sqlite", []byte("synthetic database"), 0600, zip.Store}}
	rejectArchive(t, zipBytes(t, parts), nil)
	parts = append(parts, parts[0])
	rejectArchive(t, zipBytes(t, parts), nil)
	corrupt := bytes.Clone(data)
	p := bytes.Index(corrupt, []byte("synthetic database"))
	corrupt[p] ^= 1
	rejectArchive(t, corrupt, zip.ErrChecksum)
	corrupt = bytes.Clone(data)
	p = bytes.Index(corrupt, []byte("synthetic image"))
	corrupt[p] ^= 1
	rejectArchive(t, corrupt, zip.ErrChecksum)
	corrupt = bytes.Clone(data)
	p = bytes.Index(corrupt, []byte("test"))
	corrupt[p] = 'T'
	rejectArchive(t, corrupt, zip.ErrChecksum)
	bad := zipBytes(t, []zipPart{{"manifest.json", []byte("{}"), 0600, zip.Store}, {"database.sqlite", []byte("db"), 0600, zip.Store}})
	rejectArchive(t, bad, ErrManifest)
}

type failingAt struct{ err error }

func (r failingAt) ReadAt([]byte, int64) (int, error) { return 0, r.err }
func TestArchiveReadAndSizeErrors(t *testing.T) {
	cause := errors.New("synthetic read failure")
	for _, size := range []int64{-1, 0, MaxArchiveBytes + 1, 100, MaxArchiveBytes} {
		m, err := ValidateArchive(failingAt{cause}, size)
		if !errors.Is(err, ErrArchive) || !reflect.DeepEqual(m, Manifest{}) {
			t.Fatal(m, err)
		}
		if size >= 22 && size <= MaxArchiveBytes && !errors.Is(err, cause) {
			t.Fatal(err)
		}
	}
	data, _ := archiveFixture(t, zip.Deflate)
	// Supplying a larger size must not hide a truncated physical input.
	_, err := ValidateArchive(bytes.NewReader(data[:len(data)-10]), int64(len(data)))
	if !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestArchiveZIP64Directory(t *testing.T) {
	data, want := archiveFixture(t, zip.Store)
	p := bytes.LastIndex(data, []byte("PK\x05\x06"))
	wide := make([]byte, 56)
	le.PutUint32(wide, 0x06064b50)
	le.PutUint64(wide[4:], 44)
	le.PutUint64(wide[24:], 3)
	le.PutUint64(wide[32:], 3)
	le.PutUint64(wide[40:], uint64(le.Uint32(data[p+12:])))
	le.PutUint64(wide[48:], uint64(le.Uint32(data[p+16:])))
	loc := make([]byte, 20)
	le.PutUint32(loc, 0x07064b50)
	le.PutUint64(loc[8:], uint64(p))
	le.PutUint32(loc[16:], 1)
	end := bytes.Clone(data[p:])
	le.PutUint16(end[8:], 0xffff)
	le.PutUint16(end[10:], 0xffff)
	b := append(bytes.Clone(data[:p]), wide...)
	b = append(b, loc...)
	b = append(b, end...)
	got, err := ValidateArchive(bytes.NewReader(b), int64(len(b)))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	for _, n := range []uint64{MaxEntries + 1, ^uint64(0)} {
		bad := bytes.Clone(b)
		le.PutUint64(bad[p+24:], n)
		le.PutUint64(bad[p+32:], n)
		rejectArchive(t, bad, nil)
	}
}

func TestArchiveRejectsActualExpansionBeyondDeclaration(t *testing.T) {
	m := fixture()
	m.Database.Size = 1
	m.Database.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("x")))
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	r, err := w.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Write([]byte(encoded(t, m))); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), 1<<20)
	r, err = w.CreateRaw(&zip.FileHeader{Name: "database.sqlite", Method: zip.Store, UncompressedSize64: 1, CompressedSize64: uint64(len(payload)), CRC32: crc32.ChecksumIEEE(payload)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	rejectArchive(t, b.Bytes(), zip.ErrFormat)
}

type observedAt struct {
	*bytes.Reader
	maxRead int
}

func (r *observedAt) ReadAt(p []byte, off int64) (int, error) {
	r.maxRead = max(r.maxRead, len(p))
	return r.Reader.ReadAt(p, off)
}
func TestArchiveStreamsPayload(t *testing.T) {
	db := bytes.Repeat([]byte("x"), 2<<20)
	m := fixture()
	m.Database.Size = int64(len(db))
	m.Database.SHA256 = fmt.Sprintf("%x", sha256.Sum256(db))
	data := zipBytes(t, []zipPart{{"manifest.json", []byte(encoded(t, m)), 0600, zip.Store}, {"database.sqlite", db, 0600, zip.Store}})
	r := &observedAt{Reader: bytes.NewReader(data)}
	if _, err := ValidateArchive(r, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if r.maxRead > 65*1024 {
		t.Fatalf("unbounded read: %d", r.maxRead)
	}
}

func TestArchiveRejectsZeroCRCWithoutDescriptor(t *testing.T) {
	valid, _ := archiveFixture(t, zip.Store)
	for index := 0; index < 3; index++ {
		b := bytes.Clone(valid)
		p := bytes.Index(b, []byte("PK\x01\x02"))
		for range index {
			p += 46 + int(le.Uint16(b[p+28:])) + int(le.Uint16(b[p+30:])) + int(le.Uint16(b[p+32:]))
		}
		local := int(le.Uint32(b[p+42:]))
		le.PutUint16(b[p+8:], le.Uint16(b[p+8:])&^8)
		le.PutUint16(b[local+6:], le.Uint16(b[local+6:])&^8)
		le.PutUint32(b[p+16:], 0)
		le.PutUint32(b[local+14:], 0)
		t.Run(fmt.Sprint(index), func(t *testing.T) { rejectArchive(t, b, zip.ErrChecksum) })
	}
}

func TestArchiveZIP32DirectoryLength65535(t *testing.T) {
	b, want := archiveFixture(t, zip.Store)
	end := bytes.LastIndex(b, []byte("PK\x05\x06"))
	p := bytes.Index(b, []byte("PK\x01\x02"))
	padding := 65535 - int(le.Uint32(b[end+12:]))
	step := 46 + int(le.Uint16(b[p+28:])) + int(le.Uint16(b[p+30:]))
	le.PutUint16(b[p+32:], uint16(padding))
	le.PutUint32(b[end+12:], 65535)
	expanded := append(bytes.Clone(b[:p+step]), bytes.Repeat([]byte("x"), padding)...)
	expanded = append(expanded, b[p+step:]...)
	got, err := ValidateArchive(bytes.NewReader(expanded), int64(len(expanded)))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
}
