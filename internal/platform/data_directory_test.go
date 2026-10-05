package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareDataDirectory(t *testing.T) {
	root := t.TempDir()
	for range 2 {
		got, err := prepareDataDirectory(root)
		if err != nil || got != filepath.Join(root, "MangaUpdateManager") {
			t.Fatalf("directory = %q, %v", got, err)
		}
		info, err := os.Stat(got)
		if err != nil || !info.IsDir() {
			t.Fatalf("missing directory: %v", err)
		}
	}
	if _, err := prepareDataDirectory("relative"); err == nil {
		t.Fatal("accepted relative root")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDataDirectory(file); err == nil {
		t.Fatal("accepted file as directory")
	}
}

func TestDataDirectoryUsesOSLocation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AppData", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	got, err := DataDirectory()
	if err != nil || got != filepath.Join(root, "MangaUpdateManager") {
		t.Fatalf("OS location = %q, %v", got, err)
	}
}
