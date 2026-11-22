package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
)

type extractedBuffer struct {
	bytes.Buffer
	closed   bool
	maxWrite int
}

func (b *extractedBuffer) Close() error { b.closed = true; return nil }
func (b *extractedBuffer) Write(p []byte) (int, error) {
	b.maxWrite = max(b.maxWrite, len(p))
	return b.Buffer.Write(p)
}

func TestExtractArchiveRoundTrip(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(fmt.Sprint(method), func(t *testing.T) {
			data, _ := archiveFixture(t, method)
			m, err := ValidateArchive(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			before := bytes.Clone(data)
			outputs := make(map[string]*extractedBuffer)
			var names []string
			err = ExtractArchive(context.Background(), bytes.NewReader(data), int64(len(data)), m, func(file File) (io.Writer, error) {
				names = append(names, file.Path)
				b := &extractedBuffer{}
				outputs[file.Path] = b
				return b, nil
			})
			if err != nil || !reflect.DeepEqual(names, []string{m.Database.Path, m.Images[0].Path}) {
				t.Fatal(err, names)
			}
			if outputs[m.Database.Path].String() != "synthetic database" || outputs[m.Images[0].Path].String() != "synthetic image" {
				t.Fatal(outputs)
			}
			for _, b := range outputs {
				if b.closed {
					t.Fatal("borrowed writer closed")
				}
			}
			if !bytes.Equal(data, before) {
				t.Fatal("input changed")
			}
		})
	}
	// Omitted images and the manifest never request a destination.
	m := fixture()
	m.Database.Size, m.Database.SHA256 = 2, fmt.Sprintf("%x", sha256.Sum256([]byte("db")))
	im := image(2, 1)
	m.OmittedImages = []OmittedImage{{ID: im.ID, Path: im.Path, Reason: "missing"}}
	data := zipBytes(t, []zipPart{{"manifest.json", []byte(encoded(t, m)), 0600, zip.Store}, {m.Database.Path, []byte("db"), 0600, zip.Store}})
	calls := 0
	if err := ExtractArchive(context.Background(), bytes.NewReader(data), int64(len(data)), m, func(file File) (io.Writer, error) {
		calls++
		if file != m.Database {
			t.Fatal(file)
		}
		return io.Discard, nil
	}); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestExtractArchiveManifestMismatch(t *testing.T) {
	data, m := archiveFixture(t, zip.Store)
	changes := map[string]func(*Manifest){
		"format":     func(m *Manifest) { m.FormatVersion++ },
		"time":       func(m *Manifest) { m.CreatedAt = "2028-02-29T00:00:00Z" },
		"app":        func(m *Manifest) { m.AppVersion += "other" },
		"database":   func(m *Manifest) { m.Database.Size++ },
		"migrations": func(m *Manifest) { m.Migrations = append([]string{}, m.Migrations...); m.Migrations[0] = "other" },
		"images":     func(m *Manifest) { m.Images = nil },
		"omitted": func(m *Manifest) {
			m.OmittedImages = []OmittedImage{{ID: image(2, 1).ID, Path: image(2, 1).Path, Reason: "missing"}}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			want := m
			change(&want)
			calls := 0
			err := ExtractArchive(context.Background(), bytes.NewReader(data), int64(len(data)), want, func(File) (io.Writer, error) { calls++; return io.Discard, nil })
			if !errors.Is(err, ErrArchiveExtract) || !errors.Is(err, ErrManifest) || calls != 0 {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestExtractArchiveOutputFailures(t *testing.T) {
	data, m := archiveFixture(t, zip.Store)
	fault := errors.New("output fault")
	for _, mode := range []string{"open", "open-with-writer", "nil-writer", "write", "short", "cancel-open", "cancel-write", "cancel-with-error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, writes := 0, 0
			out := &extractedBuffer{}
			err := ExtractArchive(ctx, bytes.NewReader(data), int64(len(data)), m, func(File) (io.Writer, error) {
				calls++
				switch mode {
				case "open":
					return nil, fault
				case "open-with-writer":
					return out, fault
				case "nil-writer":
					return nil, nil
				case "cancel-open":
					cancel()
					return out, nil
				}
				return writerFunc(func(p []byte) (int, error) {
					writes++
					switch mode {
					case "write":
						return 0, fault
					case "short":
						return len(p) - 1, nil
					case "cancel-with-error":
						cancel()
						return 1, fault
					default:
						cancel()
						return out.Write(p)
					}
				}), nil
			})
			if !errors.Is(err, ErrArchiveExtract) || calls != 1 || out.closed {
				t.Fatal(err, calls, out.closed)
			}
			if mode == "open" || mode == "open-with-writer" || mode == "write" || mode == "cancel-with-error" {
				if !errors.Is(err, fault) {
					t.Fatal("cause lost", err)
				}
			}
			if mode == "short" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
			if mode == "cancel-open" || mode == "cancel-write" || mode == "cancel-with-error" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancel lost", err)
				}
			}
			if mode == "cancel-open" && (writes != 0 || out.Len() != 0) {
				t.Fatal("wrote after cancelled open")
			}
		})
	}
}

type extractingReaderAt struct {
	src   io.ReaderAt
	after func()
	err   error
}

func (r extractingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.src.ReadAt(p, off)
	if r.after != nil {
		r.after()
	}
	return n, errors.Join(err, r.err)
}

func TestExtractArchiveInputFailures(t *testing.T) {
	data, m := archiveFixture(t, zip.Store)
	fault := errors.New("input fault")
	for _, mode := range []string{"before", "read", "cancel-read", "cancel-read-error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads, opens := 0, 0
			var src io.ReaderAt = extractingReaderAt{src: bytes.NewReader(data), after: func() {
				reads++
				if mode == "cancel-read" || mode == "cancel-read-error" {
					cancel()
				}
			}, err: func() error {
				if mode == "read" || mode == "cancel-read-error" {
					return fault
				}
				return nil
			}()}
			if mode == "before" {
				cancel()
			}
			err := ExtractArchive(ctx, src, int64(len(data)), m, func(File) (io.Writer, error) { opens++; return io.Discard, nil })
			if !errors.Is(err, ErrArchiveExtract) || opens != 0 {
				t.Fatal(err, opens)
			}
			if mode == "before" && reads != 0 {
				t.Fatal(reads)
			}
			if mode != "read" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if (mode == "read" || mode == "cancel-read-error") && !errors.Is(err, fault) {
				t.Fatal(err)
			}
		})
	}
	for _, size := range []int64{-1, 21, MaxArchiveBytes + 1} {
		if err := ExtractArchive(context.Background(), bytes.NewReader(data), size, m, func(File) (io.Writer, error) { t.Fatal("opened on invalid size"); return nil, nil }); !errors.Is(err, ErrArchiveExtract) {
			t.Fatal(err)
		}
	}
}

func TestExtractArchiveLateCorruption(t *testing.T) {
	valid, m := archiveFixture(t, zip.Store)
	// Change only the image bytes, after the DB has been validly extracted.
	bad := bytes.Clone(valid)
	pos := bytes.Index(bad, []byte("synthetic image"))
	if pos < 0 {
		t.Fatal("missing fixture payload")
	}
	bad[pos] ^= 1
	opens := 0
	var output []*extractedBuffer
	err := ExtractArchive(context.Background(), bytes.NewReader(bad), int64(len(bad)), m, func(File) (io.Writer, error) {
		opens++
		b := &extractedBuffer{}
		output = append(output, b)
		return b, nil
	})
	if !errors.Is(err, ErrArchiveExtract) || !errors.Is(err, zip.ErrChecksum) || opens != 2 || output[0].String() != "synthetic database" {
		t.Fatal(err, opens)
	}
	for _, b := range output {
		if b.closed {
			t.Fatal("caller output closed on failure")
		}
	}
	// ZIP-valid payload with a stale manifest hash must also fail.
	bad = zipBytes(t, []zipPart{{"manifest.json", []byte(encoded(t, m)), 0600, zip.Store}, {m.Database.Path, []byte("synthetic database"), 0600, zip.Store}, {m.Images[0].Path, []byte("Synthetic image"), 0600, zip.Store}})
	if err := ExtractArchive(context.Background(), bytes.NewReader(bad), int64(len(bad)), m, func(File) (io.Writer, error) { return io.Discard, nil }); !errors.Is(err, ErrArchiveExtract) {
		t.Fatal(err)
	}
}

func TestExtractArchiveStreamsAndBounds(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 2<<20)
	m := fixture()
	m.Database.Size = int64(len(payload))
	m.Database.SHA256 = fmt.Sprintf("%x", sha256.Sum256(payload))
	data := zipBytes(t, []zipPart{{"manifest.json", []byte(encoded(t, m)), 0600, zip.Store}, {m.Database.Path, payload, 0600, zip.Store}})
	src := &observedAt{Reader: bytes.NewReader(data)}
	out := &extractedBuffer{}
	if err := ExtractArchive(context.Background(), src, int64(len(data)), m, func(File) (io.Writer, error) { return out, nil }); err != nil {
		t.Fatal(err)
	}
	if out.maxWrite > 32<<10 || src.maxRead > 65*1024 || !bytes.Equal(out.Bytes(), payload) {
		t.Fatal("unbounded/mismatched transfer", out.maxWrite, src.maxRead)
	}
	// Exact output boundary is allowed; the next byte cannot reach the writer.
	b := &bytes.Buffer{}
	w := &extractionOutput{ctx: context.Background(), dst: b, remaining: 4}
	if n, err := w.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := w.Write([]byte("5")); n != 0 || !errors.Is(err, ErrArchive) || b.String() != "1234" {
		t.Fatal(n, err, b.String())
	}
}

func TestExtractArchivePreservesReadFaultAtEveryBoundary(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(fmt.Sprint(method), func(t *testing.T) {
			data, m := archiveFixture(t, method)
			reads := 0
			open := func(File) (io.Writer, error) { return io.Discard, nil }
			if err := ExtractArchive(context.Background(), extractingReaderAt{src: bytes.NewReader(data), after: func() { reads++ }}, int64(len(data)), m, open); err != nil {
				t.Fatal(err)
			}
			fault := errors.New("read returned bytes and fault")
			for target := 1; target <= reads; target++ {
				t.Run(fmt.Sprint(target), func(t *testing.T) {
					current := 0
					err := ExtractArchive(context.Background(), boundaryFaultReader{src: bytes.NewReader(data), current: &current, target: target, fault: fault}, int64(len(data)), m, open)
					if !errors.Is(err, fault) || !errors.Is(err, ErrArchiveExtract) {
						t.Fatal("fault swallowed", err)
					}
				})
			}
		})
	}
}

type boundaryFaultReader struct {
	src     io.ReaderAt
	current *int
	target  int
	fault   error
}

func (r boundaryFaultReader) ReadAt(p []byte, off int64) (int, error) {
	(*r.current)++
	n, err := r.src.ReadAt(p, off)
	if *r.current == r.target {
		err = errors.Join(err, r.fault)
	}
	return n, err
}

func TestExtractArchiveRejectsMissingDependencies(t *testing.T) {
	data, m := archiveFixture(t, zip.Store)
	open := func(File) (io.Writer, error) { t.Fatal("unexpected callback"); return nil, nil }
	if err := ExtractArchive(context.Background(), nil, int64(len(data)), m, open); !errors.Is(err, ErrArchiveExtract) {
		t.Fatal(err)
	}
	if err := ExtractArchive(context.Background(), bytes.NewReader(data), int64(len(data)), m, nil); !errors.Is(err, ErrArchiveExtract) {
		t.Fatal(err)
	}
}
