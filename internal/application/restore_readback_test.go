package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

type readbackProbe struct {
	data    []byte
	calls   []string
	sizes   int
	maxRead int
	maxEnd  int64
	closed  bool
	onSync  func() error
	onSize  func(int) (int64, error)
	onRead  func([]byte, int64) (int, error)
}

func (p *readbackProbe) Sync() error {
	p.calls = append(p.calls, "sync")
	if p.onSync != nil {
		return p.onSync()
	}
	return nil
}
func (p *readbackProbe) Size() (int64, error) {
	p.calls = append(p.calls, "size")
	p.sizes++
	if p.onSize != nil {
		return p.onSize(p.sizes)
	}
	return int64(len(p.data)), nil
}
func (p *readbackProbe) ReadAt(b []byte, off int64) (int, error) {
	p.calls = append(p.calls, "read")
	p.maxRead = max(p.maxRead, len(b))
	p.maxEnd = max(p.maxEnd, off+int64(len(b)))
	if p.onRead != nil {
		return p.onRead(b, off)
	}
	return bytes.NewReader(p.data).ReadAt(b, off)
}
func (p *readbackProbe) Close() error { p.closed = true; return nil }

func TestRestoreReadbackRoundTripAndBounds(t *testing.T) {
	for _, size := range []int{1, 32 << 10, 2 << 20} {
		data := bytes.Repeat([]byte("x"), size)
		p := &readbackProbe{data: data}
		if err := VerifyRestorePreparationFile(context.Background(), p, int64(size), sha256.Sum256(data)); err != nil {
			t.Fatal(err)
		}
		if p.calls[0] != "sync" || p.calls[1] != "size" || p.calls[len(p.calls)-1] != "size" || p.sizes != 2 || p.closed || p.maxRead > 32<<10 || p.maxEnd > int64(size)+1 || !bytes.Equal(p.data, data) {
			t.Fatal(p.calls, p.maxRead, p.maxEnd)
		}
	}
	// These are request-boundary models, not a 256MiB content readback.
	fault := errors.New("stop after sync")
	for _, size := range []int64{-1, 0, backup.MaxDatabaseBytes, backup.MaxDatabaseBytes + 1} {
		p := &readbackProbe{onSync: func() error { return fault }}
		err := VerifyRestorePreparationFile(context.Background(), p, size, [sha256.Size]byte{})
		if !errors.Is(err, ErrRestoreReadback) {
			t.Fatal(err)
		}
		if size == backup.MaxDatabaseBytes {
			if !errors.Is(err, fault) || !reflect.DeepEqual(p.calls, []string{"sync"}) {
				t.Fatal(err, p.calls)
			}
		} else if len(p.calls) != 0 || !errors.Is(err, backup.ErrManifest) {
			t.Fatal(err, p.calls)
		}
	}
	if err := VerifyRestorePreparationFile(context.Background(), nil, 1, [sha256.Size]byte{}); !errors.Is(err, ErrRestoreReadback) {
		t.Fatal(err)
	}
}

func TestRestoreReadbackFailures(t *testing.T) {
	want := []byte("abc")
	digest := sha256.Sum256(want)
	fault := errors.New("readback fault")
	for _, mode := range []string{"sync", "size-before", "short-size", "extra-size", "short-body", "extra-body", "hash", "read", "bytes-with-error", "zero-progress", "short-nil", "size-after", "growth-after", "shrink-after"} {
		t.Run(mode, func(t *testing.T) {
			p := &readbackProbe{data: bytes.Clone(want)}
			switch mode {
			case "sync":
				p.onSync = func() error { return fault }
			case "size-before":
				p.onSize = func(int) (int64, error) { return 0, fault }
			case "short-size":
				p.data = []byte("ab")
			case "extra-size":
				p.data = []byte("abcd")
			case "short-body":
				p.data = []byte("ab")
				p.onSize = func(int) (int64, error) { return 3, nil }
			case "extra-body":
				p.data = []byte("abcd")
				p.onSize = func(int) (int64, error) { return 3, nil }
			case "hash":
				p.data = []byte("abd")
			case "read":
				p.onRead = func([]byte, int64) (int, error) { return 0, fault }
			case "bytes-with-error":
				p.onRead = func(b []byte, _ int64) (int, error) { return copy(b, want), fault }
			case "zero-progress":
				p.onRead = func([]byte, int64) (int, error) { return 0, nil }
			case "short-nil":
				p.onRead = func(b []byte, _ int64) (int, error) { return copy(b, want), nil }
			default:
				p.onSize = func(n int) (int64, error) {
					if n == 1 {
						return 3, nil
					}
					if mode == "size-after" {
						return 0, fault
					}
					if mode == "growth-after" {
						return 4, nil
					}
					return 2, nil
				}
			}
			err := VerifyRestorePreparationFile(context.Background(), p, 3, digest)
			if !errors.Is(err, ErrRestoreReadback) || p.closed {
				t.Fatal(err, p.calls)
			}
			if mode == "sync" || mode == "size-before" || mode == "read" || mode == "bytes-with-error" || mode == "size-after" {
				if !errors.Is(err, fault) {
					t.Fatal("cause lost", err)
				}
			}
			if mode == "zero-progress" || mode == "short-nil" {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatal(err)
				}
			}
			if mode == "sync" && !reflect.DeepEqual(p.calls, []string{"sync"}) {
				t.Fatal(p.calls)
			}
			if (mode == "size-before" || mode == "short-size" || mode == "extra-size") && !reflect.DeepEqual(p.calls, []string{"sync", "size"}) {
				t.Fatal(p.calls)
			}
			if p.maxEnd > 4 {
				t.Fatal("unbounded body", p.maxEnd)
			}
		})
	}
}

func TestRestoreReadbackCancellationAndCauses(t *testing.T) {
	data := []byte("abc")
	fault := errors.New("cancelled operation fault")
	for _, where := range []string{"before", "sync", "size-before", "read", "size-after"} {
		for _, fail := range []bool{false, true} {
			t.Run(where+map[bool]string{false: "", true: "-error"}[fail], func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p := &readbackProbe{data: data}
				cancelErr := func() error {
					cancel()
					if fail {
						return fault
					}
					return nil
				}
				switch where {
				case "before":
					cancel()
				case "sync":
					p.onSync = cancelErr
				case "size-before":
					p.onSize = func(int) (int64, error) { return 3, cancelErr() }
				case "read":
					p.onRead = func(b []byte, _ int64) (int, error) {
						err := cancelErr()
						if err == nil {
							err = io.EOF
						}
						return copy(b, data), err
					}
				case "size-after":
					p.onSize = func(n int) (int64, error) {
						if n == 2 {
							return 3, cancelErr()
						}
						return 3, nil
					}
				}
				err := VerifyRestorePreparationFile(ctx, p, 3, sha256.Sum256(data))
				if !errors.Is(err, ErrRestoreReadback) || !errors.Is(err, context.Canceled) || p.closed {
					t.Fatal(err, p.calls)
				}
				if fail && where != "before" && !errors.Is(err, fault) {
					t.Fatal("cause lost", err)
				}
				wantCalls := map[string][]string{"before": nil, "sync": {"sync"}, "size-before": {"sync", "size"}, "read": {"sync", "size", "read"}, "size-after": {"sync", "size", "read", "size"}}[where]
				if !reflect.DeepEqual(p.calls, wantCalls) {
					t.Fatal("operation after cancellation", p.calls, wantCalls)
				}
			})
		}
	}
}
