package application

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
)

type restoreSQLFaultReader struct {
	err   error
	calls int
}

func (r *restoreSQLFaultReader) ReadAt(p []byte, _ int64) (int, error) {
	r.calls++
	return copy(p, "abc"), r.err
}

func TestRestoreSQLFSIndependentBoundedViews(t *testing.T) {
	b := &restoreSQLFS{ctx: context.Background(), reader: strings.NewReader("abcSECRET"), size: 3}
	if _, err := b.Open("../" + restoreSQLName); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	first, err := b.Open(restoreSQLName)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Open(restoreSQLName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.(io.Seeker).Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		view fs.File
		want string
	}{{first, "bc"}, {second, "abc"}} {
		got, err := io.ReadAll(tt.view)
		if err != nil || string(got) != tt.want {
			t.Fatal(string(got), err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil || b.openViews != 1 {
		t.Fatal(err, b.openViews)
	}
	if err := second.Close(); err != nil || b.openViews != 0 {
		t.Fatal(err, b.openViews)
	}
}

func TestRestoreSQLFSRetainsJoinedEOF(t *testing.T) {
	fault := errors.New("synthetic read fault")
	reader := &restoreSQLFaultReader{err: errors.Join(io.EOF, fault)}
	b := &restoreSQLFS{ctx: context.Background(), reader: reader, size: 3}
	view, err := b.Open(restoreSQLName)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if n, err := view.Read(make([]byte, 3)); n != 3 || !errors.Is(err, fault) {
		t.Fatal(n, err)
	}
	if n, err := view.Read(make([]byte, 1)); n != 0 || !errors.Is(err, fault) || reader.calls != 1 {
		t.Fatal(n, err, reader.calls)
	}
}
