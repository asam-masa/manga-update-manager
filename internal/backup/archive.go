package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	hexencoding "encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

const (
	MaxArchiveBytes int64 = MaxTotalBytes + 64<<20
	MaxEntries            = MaxImages + 2
	// Bound directory allocations before archive/zip parses external metadata.
	MaxDirectoryBytes int64 = 64 << 20
)

var ErrArchive = errors.New("invalid backup archive")

// ValidateArchive verifies container structure and streamed bytes, not SQLite
// contents or image validity. It never creates files and returns no partial
// manifest. The caller owns r and must keep its contents stable during the call.
func ValidateArchive(r io.ReaderAt, size int64) (Manifest, error) {
	m, err := validateArchive(r, size)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrArchive, err)
	}
	return m, nil
}

func validateArchive(r io.ReaderAt, size int64) (Manifest, error) {
	if size < 22 || size > MaxArchiveBytes {
		return Manifest{}, ErrArchive
	}
	// Confine every read, including archive/zip's random-access reads.
	source := io.NewSectionReader(r, 0, size)
	if err := checkDirectory(source, size); err != nil {
		return Manifest{}, err
	}
	z, err := zip.NewReader(source, size)
	if err != nil {
		return Manifest{}, err
	}
	files := make(map[string]*zip.File, len(z.File))
	var declared uint64
	for _, f := range z.File {
		limit := entryLimit(f.Name)
		if limit == 0 || !f.Mode().IsRegular() || f.Flags&0x41 != 0 ||
			(f.Method != zip.Store && f.Method != zip.Deflate) || files[f.Name] != nil ||
			f.UncompressedSize64 > uint64(limit) || f.CompressedSize64 > uint64(size) {
			return Manifest{}, ErrArchive
		}
		declared += f.UncompressedSize64 // <=10002 individually bounded values.
		if declared > uint64(MaxTotalBytes) {
			return Manifest{}, ErrArchive
		}
		files[f.Name] = f
	}
	mf := files["manifest.json"]
	if mf == nil || files["database.sqlite"] == nil {
		return Manifest{}, ErrArchive
	}
	reader, err := mf.Open()
	if err != nil {
		return Manifest{}, err
	}
	crc := crc32.NewIEEE()
	m, decodeErr := DecodeManifest(io.TeeReader(reader, crc))
	if err := errors.Join(decodeErr, reader.Close()); err != nil {
		return Manifest{}, err
	}
	// archive/zip can skip its CRC check when the stored value is zero.
	if crc.Sum32() != mf.CRC32 {
		return Manifest{}, zip.ErrChecksum
	}
	if len(files) != len(m.Images)+2 {
		return Manifest{}, ErrArchive
	}
	for _, im := range m.Images {
		if files[im.Path] == nil {
			return Manifest{}, ErrArchive
		}
	}
	// Only expected paths are accessed. Omitted images have no ZIP payload.
	if err := verifyFile(files[m.Database.Path], m.Database.Size, m.Database.SHA256); err != nil {
		return Manifest{}, err
	}
	for _, im := range m.Images {
		if err := verifyFile(files[im.Path], im.Size, im.SHA256); err != nil {
			return Manifest{}, err
		}
	}
	return m, nil
}

func entryLimit(name string) int64 {
	switch name {
	case "manifest.json":
		return MaxManifestBytes
	case "database.sqlite":
		return MaxDatabaseBytes
	}
	if len(name) == len("covers/")+32+len(".png") && name[:7] == "covers/" && name[39:] == ".png" && hex(name[7:39], 32) {
		return MaxImageBytes
	}
	return 0
}

func verifyFile(f *zip.File, size int64, digest string) error {
	if f == nil || f.UncompressedSize64 != uint64(size) {
		return ErrArchive
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	h := sha256.New()
	crc := crc32.NewIEEE()
	n, readErr := io.CopyBuffer(io.MultiWriter(h, crc), io.LimitReader(r, size+1), make([]byte, 32<<10))
	if err := errors.Join(readErr, r.Close()); err != nil {
		return err
	}
	if crc.Sum32() != f.CRC32 {
		return zip.ErrChecksum
	}
	if n != size || hexencoding.EncodeToString(h.Sum(nil)) != digest {
		return ErrArchive
	}
	return nil
}

var le = binary.LittleEndian

// Check the declared AND actual directory count before archive/zip allocates
// its File slice. ZIP64 values are bounded before converting to signed offsets.
func checkDirectory(r io.ReaderAt, size int64) error {
	tail := make([]byte, min(size, 65*1024))
	if _, err := r.ReadAt(tail, size-int64(len(tail))); err != nil {
		return err
	}
	end := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if le.Uint32(tail[i:]) == 0x06054b50 {
			if i+22+int(le.Uint16(tail[i+20:])) != len(tail) {
				return ErrArchive
			}
			end = i
			break
		}
	}
	if end < 0 {
		return ErrArchive
	}
	b := tail[end:]
	endOffset := size - int64(len(tail)) + int64(end)
	count, length, offset := uint64(le.Uint16(b[10:])), uint64(le.Uint32(b[12:])), uint64(le.Uint32(b[16:]))
	if le.Uint32(b[4:]) != 0 || uint64(le.Uint16(b[8:])) != count {
		return ErrArchive
	}
	if count == 0xffff || length == 0xffffffff || length == 0xffff || offset == 0xffffffff {
		var loc [20]byte
		if endOffset < 20 {
			return ErrArchive
		}
		if _, err := r.ReadAt(loc[:], endOffset-20); err != nil {
			return err
		}
		if le.Uint32(loc[:]) != 0x07064b50 {
			// 65535 is a valid ZIP32 directory length. Match archive/zip's
			// optional ZIP64 lookup for this value without requiring a locator.
			if count == 0xffff || length != 0xffff || offset == 0xffffffff {
				return ErrArchive
			}
		} else {
			if le.Uint32(loc[4:]) != 0 || le.Uint32(loc[16:]) != 1 {
				return ErrArchive
			}
			p := le.Uint64(loc[8:])
			if p > uint64(endOffset-20) || uint64(endOffset-20)-p < 56 {
				return ErrArchive
			}
			var wide [56]byte
			if _, err := r.ReadAt(wide[:], int64(p)); err != nil {
				return err
			}
			if le.Uint32(wide[:]) != 0x06064b50 || le.Uint32(wide[16:]) != 0 || le.Uint32(wide[20:]) != 0 || le.Uint64(wide[4:]) != uint64(endOffset-20)-p-12 {
				return ErrArchive
			}
			count, length, offset = le.Uint64(wide[32:]), le.Uint64(wide[40:]), le.Uint64(wide[48:])
			if le.Uint64(wide[24:]) != count {
				return ErrArchive
			}
			endOffset = int64(p)
		}
	}
	if count < 2 || count > MaxEntries || length > uint64(MaxDirectoryBytes) || offset > uint64(endOffset) || length != uint64(endOffset)-offset {
		return ErrArchive
	}
	pos := int64(offset)
	for range count {
		var h [46]byte
		if endOffset-pos < 46 {
			return ErrArchive
		}
		if _, err := r.ReadAt(h[:], pos); err != nil {
			return err
		}
		if le.Uint32(h[:]) != 0x02014b50 || le.Uint16(h[34:]) != 0 {
			return ErrArchive
		}
		n := int64(le.Uint16(h[28:]))
		extra := int64(le.Uint16(h[30:]))
		comment := int64(le.Uint16(h[32:]))
		step := 46 + n + extra + comment
		if n > 43 || step > endOffset-pos {
			return ErrArchive
		}
		name := make([]byte, n)
		if _, err := r.ReadAt(name, pos+46); err != nil {
			return err
		}
		if entryLimit(string(name)) == 0 {
			return ErrArchive
		}
		// For this size-bounded format every local offset fits in ZIP32.
		// Resolve a forced ZIP64 local offset from its bounded extra field.
		local := uint64(le.Uint32(h[42:]))
		if local == 0xffffffff {
			x := make([]byte, extra)
			if _, err := r.ReadAt(x, pos+46+n); err != nil {
				return err
			}
			found := false
			for len(x) >= 4 {
				tag, l := le.Uint16(x), int(le.Uint16(x[2:]))
				x = x[4:]
				if l > len(x) {
					return ErrArchive
				}
				if tag == 1 {
					v := x[:l]
					if le.Uint32(h[24:]) == 0xffffffff {
						if len(v) < 8 {
							return ErrArchive
						}
						v = v[8:]
					}
					if le.Uint32(h[20:]) == 0xffffffff {
						if len(v) < 8 {
							return ErrArchive
						}
						v = v[8:]
					}
					if len(v) < 8 {
						return ErrArchive
					}
					local = le.Uint64(v)
					found = true
					break
				}
				x = x[l:]
			}
			if !found {
				return ErrArchive
			}
		}
		if local > offset || offset-local < 30 {
			return ErrArchive
		}
		var lh [30]byte
		if _, err := r.ReadAt(lh[:], int64(local)); err != nil {
			return err
		}
		if le.Uint32(lh[:]) != 0x04034b50 || !bytes.Equal(lh[6:10], h[8:12]) || int64(le.Uint16(lh[26:])) != n {
			return ErrArchive
		}
		localName := make([]byte, n)
		if _, err := r.ReadAt(localName, int64(local)+30); err != nil {
			return err
		}
		if !bytes.Equal(name, localName) {
			return ErrArchive
		}
		pos += step
	}
	if pos != endOffset {
		return ErrArchive
	}
	return nil
}
