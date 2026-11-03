package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type trackedSource struct {
	io.Reader
	closed    bool
	closeErr  error
	readBytes int
}

func (r *trackedSource) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.readBytes += n
	return n, err
}
func (r *trackedSource) Close() error { r.closed = true; return r.closeErr }

func writerFixture(t *testing.T) ([]byte, Manifest, map[string][]byte) {
	t.Helper()
	_, m := archiveFixture(t, zip.Store)
	m.OmittedImages = []OmittedImage{{image(2, 1).ID, image(2, 1).Path, "missing"}}
	return []byte(encoded(t, m)), m, map[string][]byte{"database.sqlite": []byte("synthetic database"), m.Images[0].Path: []byte("synthetic image")}
}

func TestWriteArchiveRoundTrip(t *testing.T) {
	data, want, sources := writerFixture(t)
	var out bytes.Buffer
	var opened []string
	var readers []*trackedSource
	err := WriteArchive(context.Background(), &out, data, func(name string) (io.ReadCloser, error) {
		opened = append(opened, name)
		r := &trackedSource{Reader: bytes.NewReader(sources[name])}
		readers = append(readers, r)
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ValidateArchive(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil || got.Database != want.Database || len(got.Images) != 1 || len(got.OmittedImages) != 1 {
		t.Fatal(got, err)
	}
	if len(opened) != 2 || opened[0] != "database.sqlite" || opened[1] != want.Images[0].Path {
		t.Fatal(opened)
	}
	for _, r := range readers {
		if !r.closed {
			t.Fatal("source not closed")
		}
	}
}

func TestWriteArchiveInvalidManifestDoesNotStart(t *testing.T) {
	data, _, _ := writerFixture(t)
	for _, input := range [][]byte{nil, []byte("{}"), []byte(strings.Replace(string(data), "database.sqlite", "../private.sqlite", 1)), bytes.Repeat([]byte(" "), int(MaxManifestBytes+1))} {
		var out bytes.Buffer
		opened := false
		err := WriteArchive(context.Background(), &out, input, func(string) (io.ReadCloser, error) { opened = true; return nil, errors.New("unexpected open") })
		if !errors.Is(err, ErrArchiveWrite) || !errors.Is(err, ErrManifest) || out.Len() != 0 || opened {
			t.Fatal(err, out.Len(), opened)
		}
	}
}

func TestWriteArchiveSourceFailures(t *testing.T) {
	data, m, sources := writerFixture(t)
	cause := errors.New("synthetic source error")
	for name, src := range map[string]*trackedSource{
		"short": {Reader: strings.NewReader("short")},
		"long":  {Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 1000))},
		"hash":  {Reader: strings.NewReader(strings.Repeat("x", int(m.Database.Size)))},
		"read":  {Reader: sourceReadFailure{cause}},
		"close": {Reader: bytes.NewReader(sources["database.sqlite"]), closeErr: cause},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			opens := 0
			err := WriteArchive(context.Background(), &out, data, func(string) (io.ReadCloser, error) { opens++; return src, nil })
			if !errors.Is(err, ErrArchiveWrite) || !src.closed || opens != 1 || int64(src.readBytes) > m.Database.Size+1 {
				t.Fatal(err, src, opens)
			}
			if (name == "read" || name == "close") && !errors.Is(err, cause) {
				t.Fatal(err)
			}
		})
	}
	var out bytes.Buffer
	err := WriteArchive(context.Background(), &out, data, func(string) (io.ReadCloser, error) { return nil, cause })
	if !errors.Is(err, ErrArchiveWrite) || !errors.Is(err, cause) {
		t.Fatal(err)
	}
}

type sourceReadFailure struct{ err error }

func (r sourceReadFailure) Read([]byte) (int, error) { return 0, r.err }

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestWriteArchiveOutputFailures(t *testing.T) {
	data, _, sources := writerFixture(t)
	cause := errors.New("synthetic output error")
	// Small payloads stay buffered until zip.Close: output failure must still
	// be returned, even though every source was read and closed successfully.
	for name, dst := range map[string]io.Writer{"failure": writerFunc(func([]byte) (int, error) { return 0, cause }), "short": writerFunc(func(p []byte) (int, error) { return len(p) - 1, nil })} {
		t.Run(name, func(t *testing.T) {
			closed := 0
			err := WriteArchive(context.Background(), dst, data, func(name string) (io.ReadCloser, error) {
				return &closeCountReader{Reader: bytes.NewReader(sources[name]), count: &closed}, nil
			})
			if !errors.Is(err, ErrArchiveWrite) || closed != 2 {
				t.Fatal(err, closed)
			}
			if name == "failure" && !errors.Is(err, cause) {
				t.Fatal(err)
			}
			if name == "short" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
		})
	}
}

type closeCountReader struct {
	io.Reader
	count *int
}

func (r *closeCountReader) Close() error { *r.count++; return nil }

func TestWriteArchiveCancellation(t *testing.T) {
	data, _, sources := writerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	opens := 0
	err := WriteArchive(ctx, &out, data, func(string) (io.ReadCloser, error) { opens++; return nil, errors.New("unexpected") })
	if !errors.Is(err, context.Canceled) || out.Len() != 0 || opens != 0 {
		t.Fatal(err, out.Len(), opens)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	var source *trackedSource
	err = WriteArchive(ctx, &out, data, func(name string) (io.ReadCloser, error) {
		opens++
		source = &trackedSource{Reader: cancelReader{src: bytes.NewReader(sources[name]), cancel: cancel}}
		return source, nil
	})
	if !errors.Is(err, ErrArchiveWrite) || !errors.Is(err, context.Canceled) || opens != 1 || !source.closed {
		t.Fatal(err, opens, source)
	}
}

type cancelReader struct {
	src    io.Reader
	cancel context.CancelFunc
}

func (r cancelReader) Read(p []byte) (int, error) { n, err := r.src.Read(p); r.cancel(); return n, err }

func TestArchiveOutputLimit(t *testing.T) {
	for _, limit := range []int64{0, 4, MaxArchiveBytes} {
		var out bytes.Buffer
		w := &archiveOutput{ctx: context.Background(), dst: &out, remaining: limit}
		if limit == 4 {
			if n, err := w.Write([]byte("1234")); err != nil || n != 4 {
				t.Fatal(n, err)
			}
		}
		if limit == MaxArchiveBytes {
			w.dst = io.Discard
			if n, err := w.Write([]byte("x")); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			continue
		}
		if n, err := w.Write([]byte("x")); err == nil || n != 0 {
			t.Fatal(n, err)
		}
	}
}

func TestWriteArchiveBodyWriteFailure(t *testing.T) {
	_, m, sources := writerFixture(t)
	payload := bytes.Repeat([]byte("x"), 128<<10)
	m.Database.Size = int64(len(payload))
	m.Database.SHA256 = fmt.Sprintf("%x", sha256.Sum256(payload))
	sources["database.sqlite"] = payload
	cause := errors.New("body write failed")
	var source *trackedSource
	opens := 0
	err := WriteArchive(context.Background(), writerFunc(func([]byte) (int, error) { return 0, cause }), []byte(encoded(t, m)), func(name string) (io.ReadCloser, error) {
		opens++
		source = &trackedSource{Reader: bytes.NewReader(sources[name])}
		return source, nil
	})
	if !errors.Is(err, ErrArchiveWrite) || !errors.Is(err, cause) || opens != 1 || !source.closed || source.readBytes >= len(payload) {
		t.Fatal(err, opens, source)
	}
}

func TestWriteArchiveCancellationDuringFinalization(t *testing.T) {
	data, _, sources := writerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := 0
	writes := 0
	dst := writerFunc(func(p []byte) (int, error) { writes++; cancel(); return len(p), nil })
	err := WriteArchive(ctx, dst, data, func(name string) (io.ReadCloser, error) {
		return &closeCountReader{Reader: bytes.NewReader(sources[name]), count: &closed}, nil
	})
	if !errors.Is(err, ErrArchiveWrite) || !errors.Is(err, context.Canceled) || closed != 2 || writes == 0 {
		t.Fatal(err, closed, writes)
	}
}
