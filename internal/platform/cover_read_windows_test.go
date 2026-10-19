package platform

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadVerifiedCoverAndRelease(t *testing.T) {
	dir := t.TempDir()
	original := generatedCover(t)
	info, err := WriteNewCover(context.Background(), dir, testNewCoverID, original)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadVerifiedCover(context.Background(), dir, testNewCoverID, info)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("read: %v", err)
	}
	got[0] ^= 1
	again, err := ReadVerifiedCover(context.Background(), dir, testNewCoverID, info)
	if err != nil || !bytes.Equal(again, original) {
		t.Fatalf("returned buffer aliases file: %v", err)
	}
	// Exclusive deletion after return requires the reader's handle to be closed.
	if err := DeleteVerifiedCover(context.Background(), dir, testNewCoverID, info); err != nil {
		t.Fatalf("file handle leaked: %v", err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatalf("directory handles leaked: %v", err)
	}
	// Keep the renamed disposable fixture in testing's cleanup scope.
	if err := os.Rename(dir+"-moved", dir); err != nil {
		t.Fatal(err)
	}
}

func TestReadVerifiedCoverRefusals(t *testing.T) {
	for _, mode := range []string{"changed", "replacement", "hard-link", "symlink", "directory", "held", "identity", "volume", "size", "hash", "cancel", "invalid-id", "upper-id", "ancestor-link", "missing"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			original := generatedCover(t)
			info, err := WriteNewCover(context.Background(), dir, testNewCoverID, original)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(dir, "covers", testNewCoverID+".png")
			preserved := filepath.Join(dir, "original.png")
			ctx := context.Background()
			id := testNewCoverID
			switch mode {
			case "changed":
				original[0] ^= 1
				err = os.WriteFile(name, original, 0600)
			case "replacement", "symlink", "directory", "missing":
				if err := os.Rename(name, preserved); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "replacement":
					err = os.WriteFile(name, original, 0600)
				case "symlink":
					err = os.Symlink(preserved, name)
				case "directory":
					err = os.Mkdir(name, 0700)
				}
			case "hard-link":
				err = os.Link(name, preserved)
			case "held":
				file, openErr := openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer file.Close()
			case "identity":
				info.FileID[0] ^= 1
			case "volume":
				info.VolumeSerial++
			case "size":
				info.ByteSize++
			case "hash":
				info.SHA256[0] ^= 1
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "invalid-id":
				id = "../original"
			case "upper-id":
				id = strings.ToUpper(id)
			case "ancestor-link":
				link := filepath.Join(t.TempDir(), "linked")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				dir = link
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadVerifiedCover(ctx, dir, id, info)
			if err == nil || got != nil {
				t.Fatalf("unsafe read: bytes=%d err=%v", len(got), err)
			}
			if mode != "held" && mode != "directory" && mode != "missing" {
				after, err := os.ReadFile(name)
				if err != nil || !bytes.Equal(after, original) {
					t.Fatal("target changed", err)
				}
			}
			if mode == "replacement" || mode == "symlink" || mode == "directory" || mode == "hard-link" || mode == "missing" {
				after, err := os.ReadFile(preserved)
				if err != nil || !bytes.Equal(after, original) {
					t.Fatal("original changed", err)
				}
			}
		})
	}
}

func TestReadVerifiedCoverCancellationReleasesHandles(t *testing.T) {
	dir := t.TempDir()
	info, err := WriteNewCover(context.Background(), dir, testNewCoverID, generatedCover(t))
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "covers", testNewCoverID+".png")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := coverCancelContext{Context: ctx, check: func() {
		file, err := os.Open(name)
		if err != nil {
			cancel() // The read's exclusive file handle is now held.
		} else {
			file.Close()
		}
	}}
	got, err := ReadVerifiedCover(controlled, dir, testNewCoverID, info)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation after open not observed", err)
	}
	if err := DeleteVerifiedCover(context.Background(), dir, testNewCoverID, info); err != nil {
		t.Fatal("cancelled read retained handle", err)
	}
}

func TestReadVerifiedCoverSizeAndMissingDirectory(t *testing.T) {
	ctx := context.Background()
	for _, size := range []int64{0, -1, MaxCoverReadBytes + 1, 1<<63 - 1} {
		got, err := ReadVerifiedCover(ctx, "invalid", testNewCoverID, CoverFileInfo{ByteSize: size})
		if got != nil || !errors.Is(err, ErrInvalidCoverFile) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
	dir := t.TempDir()
	got, err := ReadVerifiedCover(ctx, dir, testNewCoverID, CoverFileInfo{ByteSize: 1})
	if got != nil || err == nil {
		t.Fatal("missing directory accepted")
	}
	if _, err := os.Lstat(filepath.Join(dir, "covers")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created covers directory", err)
	}
	// This OS boundary verifies bytes/identity, not PNG; exact limit is allowed.
	data := make([]byte, MaxCoverReadBytes)
	info, err := WriteNewCover(ctx, dir, testNewCoverID, data)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ReadVerifiedCover(ctx, dir, testNewCoverID, info)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("exact limit refused", err)
	}
}
