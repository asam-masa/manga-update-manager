package restoreswitch

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func content(t *testing.T, root, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || string(got) != want {
		t.Fatalf("%s: %q %v", name, got, err)
	}
}
func move(t *testing.T, root, from, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); err != nil {
		t.Fatal(err)
	}
}

// Model only. DB and image contents are sentinel bytes, not product data.
func TestPairSwitchCheckpoints(t *testing.T) {
	for _, checkpoint := range []string{"prepared", "old-db-moved", "old-pair-moved", "new-db-installed", "new-pair-installed"} {
		t.Run(checkpoint, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "live/db", "old-db")
			write(t, root, "live/covers/image", "old-image")
			write(t, root, "staged/db", "new-db")
			write(t, root, "staged/covers/image", "new-image")
			if err := os.Mkdir(filepath.Join(root, "retired"), 0700); err != nil {
				t.Fatal(err)
			}
			if checkpoint == "prepared" {
				content(t, root, "live/db", "old-db")
				return
			}
			move(t, root, "live/db", "retired/db")
			if checkpoint == "old-db-moved" {
				move(t, root, "retired/db", "live/db")
				content(t, root, "live/db", "old-db")
				content(t, root, "live/covers/image", "old-image")
				return
			}
			move(t, root, "live/covers", "retired/covers")
			if checkpoint == "old-pair-moved" {
				move(t, root, "retired/db", "live/db")
				move(t, root, "retired/covers", "live/covers")
				content(t, root, "live/db", "old-db")
				content(t, root, "live/covers/image", "old-image")
				return
			}
			move(t, root, "staged/db", "live/db")
			if checkpoint == "new-db-installed" {
				// Simulate failure before installing images, retaining all new output.
				move(t, root, "live/db", "staged/db")
				move(t, root, "retired/db", "live/db")
				move(t, root, "retired/covers", "live/covers")
				content(t, root, "live/db", "old-db")
				content(t, root, "live/covers/image", "old-image")
				content(t, root, "staged/db", "new-db")
				return
			}
			move(t, root, "staged/covers", "live/covers")
			content(t, root, "live/db", "new-db")
			content(t, root, "live/covers/image", "new-image")
			content(t, root, "retired/db", "old-db")
			content(t, root, "retired/covers/image", "old-image")
		})
	}
}

func TestRollbackCollisionPreservesBoth(t *testing.T) {
	root := t.TempDir()
	write(t, root, "retired/covers/image", "old-image")
	write(t, root, "live/covers/unexpected", "unexpected")
	if err := os.Rename(filepath.Join(root, "retired/covers"), filepath.Join(root, "live/covers")); err == nil {
		t.Fatal("replaced occupied directory")
	}
	content(t, root, "retired/covers/image", "old-image")
	content(t, root, "live/covers/unexpected", "unexpected")
	// Real recovery must stop here, not delete the unexpected destination.
}
