package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

func sourceReadFixture(t *testing.T) *RestoreSource {
	t.Helper()
	s, err := OpenRestoreSource(context.Background(), restoreSourceFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRestoreSourceReadNative(t *testing.T) {
	s := sourceReadFixture(t)
	ctx := context.Background()
	if err := s.VerifyIdentity(ctx); err != nil {
		t.Fatal(err)
	}
	reader := s.Reader(ctx)
	if _, err := s.file.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(sourceBytes))
	if n, err := reader.ReadAt(got, 0); n != len(got) || err != nil || string(got) != sourceBytes {
		t.Fatal(n, err)
	}
	if pos, err := s.file.Seek(0, io.SeekCurrent); err != nil || pos != 5 {
		t.Fatal("read changed cursor", pos, err)
	}
	short := make([]byte, 8)
	if n, err := reader.ReadAt(short, int64(len(sourceBytes)-3)); n != 3 || err != io.EOF || string(short[:n]) != sourceBytes[len(sourceBytes)-3:] {
		t.Fatal(n, err)
	}
	if n, err := reader.ReadAt(short, int64(len(sourceBytes))); n != 0 || err != io.EOF {
		t.Fatal(n, err)
	}
	if n, err := reader.ReadAt(nil, 0); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := reader.ReadAt(short, -1); n != 0 || err == nil {
		t.Fatal(n, err)
	}
	if size, err := s.Size(ctx); err != nil || size != int64(len(sourceBytes)) {
		t.Fatal(size, err)
	}
	if err := s.VerifyIdentity(ctx); err != nil || s.ReleaseResult().CloseAttempted {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := reader.ReadAt(got, 0); n != 0 || !errors.Is(err, os.ErrClosed) {
		t.Fatal("borrowed reader survived Close", n, err)
	}
	if got, err := os.ReadFile(s.Path()); err != nil || string(got) != sourceBytes {
		t.Fatal("source changed", err)
	}
}

func TestRestoreSourceReadClosed(t *testing.T) {
	var nilSource *RestoreSource
	closed := sourceReadFixture(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	faulty := sourceReadFixture(t)
	realClose := faulty.closeFile
	t.Cleanup(func() { _ = realClose() })
	fault := errors.New("Close not confirmed")
	faulty.closeFile = func() error { return fault } // Physical handle still live.
	if err := faulty.Close(); !errors.Is(err, fault) {
		t.Fatal(err)
	}
	if _, err := faulty.file.ReadAt(make([]byte, 1), 0); err != nil {
		t.Fatal("fault fixture not live", err)
	}
	for _, s := range []*RestoreSource{nilSource, {}, closed, faulty} {
		if n, err := s.Reader(context.Background()).ReadAt(make([]byte, 1), 0); n != 0 || !errors.Is(err, os.ErrClosed) {
			t.Fatal(n, err)
		}
		if size, err := s.Size(context.Background()); size != 0 || !errors.Is(err, os.ErrClosed) {
			t.Fatal(size, err)
		}
		if err := s.VerifyIdentity(context.Background()); !errors.Is(err, os.ErrClosed) {
			t.Fatal(err)
		}
	}
}

func TestRestoreSourceReadCancellationAndErrors(t *testing.T) {
	for _, mode := range []string{"before", "read-error", "after", "after-eof", "after-error"} {
		t.Run(mode, func(t *testing.T) {
			s := sourceReadFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "before" {
				cancel()
			}
			calls := 0
			fault := errors.New("read fault")
			buf := make([]byte, 4)
			n, err := s.readAt(ctx, buf, 0, func(*os.File, []byte, int64) (int, error) {
				calls++
				copy(buf, "ab")
				if mode != "read-error" {
					cancel()
				}
				if mode == "after-eof" {
					return 2, io.EOF
				}
				if mode == "read-error" || mode == "after-error" {
					return 2, fault
				}
				return 2, nil
			})
			if mode == "before" {
				if n != 0 || calls != 0 || !errors.Is(err, context.Canceled) {
					t.Fatal(n, err, calls)
				}
			} else {
				if n != 2 || calls != 1 || string(buf[:2]) != "ab" {
					t.Fatal(n, err, calls)
				}
				if mode == "read-error" {
					if err != fault {
						t.Fatal("native cause replaced", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if mode == "after-eof" && !errors.Is(err, io.EOF) {
					t.Fatal("EOF lost", err)
				}
				if mode == "after-error" && !errors.Is(err, fault) {
					t.Fatal("read error lost", err)
				}
			}
			if s.ReleaseResult().CloseAttempted {
				t.Fatal("read implicitly released owner")
			}
		})
	}
	s := sourceReadFixture(t)
	if err := s.file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reader(context.Background()).ReadAt(make([]byte, 1), 0); !errors.Is(err, os.ErrClosed) {
		t.Fatal("native error lost", err)
	}
}

func TestRestoreSourceRecheck(t *testing.T) {
	for _, mode := range []string{"same", "case", "id", "volume", "name", "size", "attributes-links", "observe-error", "cancel-before", "cancel-after-error"} {
		t.Run(mode, func(t *testing.T) {
			s := sourceReadFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel-before" {
				cancel()
			}
			calls := 0
			fault := errors.New("inspection fault")
			size, err := s.inspect(ctx, func(f *os.File) (restoreSourceObservation, error) {
				calls++
				o, err := observeRestoreSource(f)
				switch mode {
				case "case":
					o.name = strings.ToUpper(o.name)
				case "id":
					o.id.FileID[0] ^= 1
				case "volume":
					o.id.VolumeSerial++
				case "name":
					o.name += ".other"
				case "size":
					o.size++
				case "attributes-links":
					err = ErrUnsafeRestoreSource
				case "observe-error":
					err = fault
				case "cancel-after-error":
					cancel()
					err = fault
				}
				return o, err
			})
			if mode == "same" || mode == "case" {
				if err != nil || size != s.size {
					t.Fatal(size, err)
				}
			} else if size != 0 || err == nil {
				t.Fatal(size, err)
			}
			if mode == "cancel-before" {
				if calls != 0 || !errors.Is(err, context.Canceled) {
					t.Fatal(calls, err)
				}
			} else if mode != "same" && mode != "case" && !errors.Is(err, ErrUnsafeRestoreSource) {
				t.Fatal(err)
			}
			if mode == "observe-error" || mode == "cancel-after-error" {
				if !errors.Is(err, fault) {
					t.Fatal("inspection cause lost", err)
				}
			}
			if mode == "cancel-after-error" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if s.ReleaseResult().CloseAttempted {
				t.Fatal("inspection implicitly released owner")
			}
		})
	}
}

func TestRestoreSourceNewLinkRecheck(t *testing.T) {
	s := sourceReadFixture(t)
	alias := s.Path() + ".alias"
	if err := os.Link(s.Path(), alias); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Log("link creation refused; dynamic mismatch not exercised")
			return
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		if err := os.Remove(alias); err != nil {
			t.Error(err)
		}
	})
	if err := s.VerifyIdentity(context.Background()); !errors.Is(err, ErrUnsafeRestoreSource) {
		t.Fatal("new link not detected", err)
	}
	if size, err := s.Size(context.Background()); size != 0 || !errors.Is(err, ErrUnsafeRestoreSource) {
		t.Fatal(size, err)
	}
}

func TestRestoreSourceReadCloseOrdering(t *testing.T) {
	for _, mode := range []string{"read", "inspect"} {
		t.Run(mode, func(t *testing.T) {
			s := sourceReadFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			done := make(chan error, 1)
			go func() {
				var err error
				if mode == "read" {
					_, err = s.readAt(context.Background(), make([]byte, 1), 0, func(f *os.File, p []byte, off int64) (int, error) { close(entered); <-release; return f.ReadAt(p, off) })
				} else {
					_, err = s.inspect(context.Background(), func(f *os.File) (restoreSourceObservation, error) {
						close(entered)
						<-release
						return observeRestoreSource(f)
					})
				}
				done <- err
			}()
			<-entered
			requested, closed := make(chan struct{}), make(chan error, 1)
			go func() { close(requested); closed <- s.Close() }()
			<-requested
			select {
			case err := <-closed:
				t.Fatal("Close crossed active operation", err)
			default:
			}
			unblock()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := <-closed; err != nil || !s.ReleaseResult().Confirmed() {
				t.Fatal(err)
			}
		})
	}
	s := sourceReadFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	closeFile := s.closeFile
	s.closeFile = func() error { close(entered); <-release; return closeFile() }
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	<-entered
	read := make(chan error, 1)
	go func() { _, err := s.Reader(context.Background()).ReadAt(make([]byte, 1), 0); read <- err }()
	unblock()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := <-read; !errors.Is(err, os.ErrClosed) {
		t.Fatal("read admitted after Close began", err)
	}
}
