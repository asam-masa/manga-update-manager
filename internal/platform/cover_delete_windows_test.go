package platform

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDeleteVerifiedCoverAndAbsence(t *testing.T) {
	dir := t.TempDir()
	info, err := WriteNewCover(context.Background(), dir, testNewCoverID, generatedCover(t))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // Second call safely handles deletion before DB cleanup.
		if err := DeleteVerifiedCover(context.Background(), dir, testNewCoverID, info); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "covers", testNewCoverID+".png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := confirmCoverAbsent(filepath.Join(dir, "missing-parent", "image.png")); err == nil {
		t.Fatal("path-not-found accepted")
	}
}

func TestDeleteVerifiedCoverRefusals(t *testing.T) {
	for _, mode := range []string{"changed", "replacement", "hard-link", "symlink", "directory", "held", "read-only", "identity", "size", "cancel", "invalid-id", "ancestor-link"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			data := generatedCover(t)
			info, err := WriteNewCover(context.Background(), dir, testNewCoverID, data)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(dir, "covers", testNewCoverID+".png")
			preserved := filepath.Join(dir, "original.png")
			ctx := context.Background()
			id := testNewCoverID
			switch mode {
			case "changed":
				data[0] ^= 1
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "replacement", "symlink", "directory":
				if err := os.Rename(name, preserved); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "replacement":
					err = os.WriteFile(name, data, 0600)
				case "symlink":
					err = os.Symlink(preserved, name)
				case "directory":
					err = os.Mkdir(name, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "hard-link":
				if err := os.Link(name, preserved); err != nil {
					t.Fatal(err)
				}
			case "held":
				file, err := openCoverFile(name, windows.GENERIC_READ, windows.OPEN_EXISTING)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
			case "read-only":
				if err := os.Chmod(name, 0400); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(name, 0600)
			case "identity":
				info.FileID[0] ^= 1
			case "size":
				info.ByteSize++
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "invalid-id":
				id = "../original"
			case "ancestor-link":
				link := filepath.Join(t.TempDir(), "linked")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				dir = link
			}
			if err := DeleteVerifiedCover(ctx, dir, id, info); err == nil {
				t.Fatal("unsafe deletion accepted")
			}
			if _, err := os.Lstat(name); err != nil {
				t.Fatal("target removed", err)
			}
			if mode != "held" && mode != "directory" {
				after, err := os.ReadFile(name)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatal("target changed", err)
				}
			}
			if mode == "replacement" || mode == "symlink" || mode == "directory" || mode == "hard-link" {
				if _, err := os.Stat(preserved); err != nil {
					t.Fatal("original removed", err)
				}
			}
		})
	}
}
