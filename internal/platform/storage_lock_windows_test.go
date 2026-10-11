package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStorageLockLifecycle(t *testing.T) {
	directory := t.TempDir()
	name := filepath.Join(directory, storageLockName)
	// An existing file is not a stale-lock condition and must not be truncated.
	if err := os.WriteFile(name, []byte("retained sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireStorageLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	for _, alias := range []string{directory, strings.ToUpper(directory), filepath.Join(directory, "unused", "..")} {
		other, err := AcquireStorageLock(alias)
		if other != nil || !errors.Is(err, ErrStorageInUse) {
			t.Fatalf("same directory accepted: %v", err)
		}
	}
	if err := os.Remove(name); err == nil {
		t.Fatal("held lock file removed")
	}
	if err := os.Rename(name, name+".moved"); err == nil {
		t.Fatal("held lock file renamed")
	}
	other, err := AcquireStorageLock(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = other.Close()
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal("repeated close", err)
	}
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "retained sentinel" {
		t.Fatalf("lock file changed: %q, %v", data, err)
	}
	reopened, err := AcquireStorageLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	_ = reopened.Close()
}

func TestStorageLockRejectsUnavailableOrUnsafeFiles(t *testing.T) {
	for _, directory := range []string{"", ".", filepath.Join(t.TempDir(), "missing")} {
		if lock, err := AcquireStorageLock(directory); err == nil || lock != nil {
			t.Fatal("invalid directory accepted")
		}
	}
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, storageLockName), 0700); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireStorageLock(directory); err == nil || lock != nil {
		t.Fatal("directory lock accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"hardlink", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			link := os.Link
			if kind == "symlink" {
				link = os.Symlink
			}
			if err := link(outside, filepath.Join(directory, storageLockName)); err != nil {
				if kind == "symlink" {
					t.Skipf("symlink unavailable: %v", err)
				}
				t.Fatal(err)
			}
			if lock, err := AcquireStorageLock(directory); err == nil || lock != nil {
				t.Fatal("linked lock accepted")
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != "outside sentinel" {
				t.Fatal("outside file changed")
			}
		})
	}
}

func TestStorageLockConcurrentAcquisition(t *testing.T) {
	directory := t.TempDir()
	start := make(chan struct{})
	var wg sync.WaitGroup
	locks := make(chan io.Closer, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			lock, err := AcquireStorageLock(directory)
			if err == nil {
				locks <- lock
			} else if !errors.Is(err, ErrStorageInUse) {
				t.Errorf("unexpected acquisition error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(locks)
	count := 0
	for lock := range locks {
		count++
		_ = lock.Close()
	}
	if count != 1 {
		t.Fatalf("acquired %d locks", count)
	}
}

func TestStorageLockDirectoryAlias(t *testing.T) {
	directory := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	lock, err := AcquireStorageLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	other, err := AcquireStorageLock(alias)
	if other != nil || !errors.Is(err, ErrStorageInUse) {
		t.Fatalf("directory alias bypassed lock: %v", err)
	}
}

// This helper runs only in an explicitly launched test child with temporary data.
func TestStorageLockChild(t *testing.T) {
	directory := os.Getenv("MANGA_LOCK_TEST_DIRECTORY")
	if directory == "" {
		return
	}
	lock, err := AcquireStorageLock(directory)
	if errors.Is(err, ErrStorageInUse) {
		fmt.Println("BUSY")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("READY")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func startLockChild(t *testing.T, directory string) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStorageLockChild$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MANGA_LOCK_TEST_DIRECTORY="+directory)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, stdin, bufio.NewReader(stdout)
}

func TestStorageLockAcrossProcesses(t *testing.T) {
	for _, end := range []string{"normal", "killed"} {
		t.Run(end, func(t *testing.T) {
			directory := t.TempDir()
			cmd, stdin, stdout := startLockChild(t, directory)
			if line, _ := stdout.ReadString('\n'); strings.TrimSpace(line) != "READY" {
				t.Fatalf("child not ready: %q", line)
			}
			second, secondIn, secondOut := startLockChild(t, directory)
			_ = secondIn.Close()
			if line, _ := secondOut.ReadString('\n'); strings.TrimSpace(line) != "BUSY" {
				t.Fatalf("second child: %q", line)
			}
			_, _ = io.Copy(io.Discard, secondOut)
			if err := second.Wait(); err != nil {
				t.Fatal(err)
			}
			other, otherIn, otherOut := startLockChild(t, t.TempDir())
			if line, _ := otherOut.ReadString('\n'); strings.TrimSpace(line) != "READY" {
				t.Fatal("different storage refused")
			}
			_ = otherIn.Close()
			_, _ = io.Copy(io.Discard, otherOut)
			if err := other.Wait(); err != nil {
				t.Fatal(err)
			}
			if end == "killed" {
				_ = cmd.Process.Kill()
			} else {
				_ = stdin.Close()
			}
			_, _ = io.Copy(io.Discard, stdout)
			err := cmd.Wait()
			if (err != nil) != (end == "killed") {
				t.Fatalf("child exit: %v", err)
			}
			lock, err := AcquireStorageLock(directory)
			if err != nil {
				t.Fatalf("lock leaked after %s exit: %v", end, err)
			}
			_ = lock.Close()
		})
	}
}
