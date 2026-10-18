package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCoverSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.png")
	if err := os.WriteFile(path, []byte("synthetic source"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := OpenCoverSource(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(source)
	closeErr := source.Close()
	if readErr != nil || closeErr != nil || string(data) != "synthetic source" {
		t.Fatal(string(data), readErr, closeErr)
	}
	if _, err := OpenCoverSource(context.Background(), dir); !errors.Is(err, ErrInvalidCoverFile) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenCoverSource(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
