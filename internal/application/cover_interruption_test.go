package application

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type interruptionRepository struct {
	CoverRepository
	mode  string
	pause func()
}

func (r interruptionRepository) ReserveManagedCover(ctx context.Context, id string) error {
	err := r.CoverRepository.ReserveManagedCover(ctx, id)
	if err == nil && r.mode == "reserved" {
		r.pause()
	}
	return err
}
func (r interruptionRepository) ConfirmManagedCover(ctx context.Context, id string, info storage.ManagedCoverInfo) error {
	err := r.CoverRepository.ConfirmManagedCover(ctx, id, info)
	if err == nil && r.mode == "confirmed" {
		r.pause()
	}
	return err
}
func (r interruptionRepository) SetWorkCover(ctx context.Context, id int64, cover string, now time.Time) (manga.Work, string, error) {
	work, old, err := r.CoverRepository.SetWorkCover(ctx, id, cover, now)
	if err == nil && r.mode == "referenced" {
		r.pause()
	}
	return work, old, err
}

// Invoked only by this test's parent with an isolated fixture directory.
func TestCoverInterruptionChild(t *testing.T) {
	dir := os.Getenv("MUM_COVER_INTERRUPTION_DIRECTORY")
	if dir == "" {
		return
	}
	mode := os.Getenv("MUM_COVER_INTERRUPTION_MODE")
	switch mode {
	case "reserved", "partial", "confirmed", "referenced", "deleted":
	default:
		t.Fatal("invalid child mode")
	}
	id, err := strconv.ParseInt(os.Getenv("MUM_COVER_INTERRUPTION_WORK"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := platform.AcquireStorageLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(dir, "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	work, err := store.WorkByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	pause := func() {
		fmt.Println("BOUNDARY")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		t.Fatal("child boundary unexpectedly resumed")
	}
	var operations CoverOperations
	a := NewCoverImporter(interruptionRepository{store, mode, pause}, &operations, dir,
		func(context.Context) (string, error) { return filepath.Join(dir, "source.png"), nil },
		func() time.Time { return work.UpdatedAt.Add(time.Hour) })
	if mode == "partial" {
		a.write = func(_ context.Context, dir, id string, data []byte) (platform.CoverFileInfo, error) {
			file, err := os.OpenFile(filepath.Join(dir, "covers", id+".png"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return platform.CoverFileInfo{}, err
			}
			if _, err := file.Write(data[:len(data)/2]); err != nil {
				_ = file.Close()
				return platform.CoverFileInfo{}, err
			}
			if err := file.Close(); err != nil {
				return platform.CoverFileInfo{}, err
			}
			pause()
			return platform.CoverFileInfo{}, errors.New("unreachable")
		}
	}
	if mode == "deleted" {
		deleteFile := a.delete
		a.delete = func(ctx context.Context, dir, id string, info platform.CoverFileInfo) error {
			err := deleteFile(ctx, dir, id, info)
			if err == nil {
				pause()
			}
			return err
		}
	}
	_, err = a.Select(ctx, id)
	t.Fatal("operation did not reach interruption boundary", err)
}

func TestCoverInterruptedProcessRecovery(t *testing.T) {
	for _, mode := range []string{"reserved", "partial", "confirmed", "referenced", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			a, store, work, dir := importFixture(t)
			ctx := context.Background()
			seed, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			_, seedErr := seed.Exec("UPDATE works SET site_name=?,notes=? WHERE id=?", "Synthetic Site", "Preserve interruption notes", work.ID)
			if err := errors.Join(seedErr, seed.Close()); err != nil {
				t.Fatal(err)
			}
			if err := store.RecordLastAccess(ctx, work.ID, work.CreatedAt.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			initial, err := a.Select(ctx, work.ID)
			if err != nil || initial.Work == nil {
				t.Fatal(initial, err)
			}
			before := *initial.Work
			oldName := filepath.Join(dir, filepath.FromSlash(before.ThumbnailPath))
			source, err := os.ReadFile(oldName)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "source.png"), source, 0600); err != nil {
				t.Fatal(err)
			}
			unknown := filepath.Join(dir, "covers", "unknown.png")
			if err := os.WriteFile(unknown, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestCoverInterruptionChild$", "-test.timeout=25s")
			cmd.Env = append(os.Environ(), "MUM_COVER_INTERRUPTION_DIRECTORY="+dir, "MUM_COVER_INTERRUPTION_MODE="+mode, "MUM_COVER_INTERRUPTION_WORK="+strconv.FormatInt(work.ID, 10))
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			var diagnostics bytes.Buffer
			cmd.Stderr = &diagnostics
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = in.Close()
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			})
			if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || strings.TrimSpace(line) != "BOUNDARY" {
				t.Fatal("child failed before boundary", line, err)
			}
			if held, err := platform.AcquireStorageLock(dir); !errors.Is(err, platform.ErrStorageInUse) {
				if held != nil {
					_ = held.Close()
				}
				t.Fatal("child did not retain startup lock", err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil || childCtx.Err() != nil {
				t.Fatal("not the intended process interruption", err, childCtx.Err())
			}
			lock, err := platform.AcquireStorageLock(dir)
			if err != nil {
				t.Fatal("lock not released by child exit", err)
			}
			defer lock.Close()
			reopened, err := storage.Open(ctx, filepath.Join(dir, "fixture.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			db, err := sql.Open("sqlite", filepath.Join(dir, "fixture.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			var check string
			queryErr := db.QueryRow("PRAGMA quick_check").Scan(&check)
			if err := errors.Join(queryErr, db.Close()); err != nil || check != "ok" {
				t.Fatal(check, err)
			}
			records, err := reopened.ListManagedCovers(ctx)
			if err != nil || len(records) != 2 {
				t.Fatal(records, err)
			}
			var fresh storage.ManagedCover
			for _, record := range records {
				if record.RelativePath != before.ThumbnailPath {
					fresh = record
				}
			}
			unverified := mode == "reserved" || mode == "partial"
			wantState := "ready"
			if unverified {
				wantState = "reserved"
			}
			if fresh.ID == "" || fresh.State != wantState || (fresh.Info == nil) != unverified {
				t.Fatal(fresh)
			}
			expected := before
			committed := mode == "referenced" || mode == "deleted"
			if committed {
				expected.ThumbnailPath = fresh.RelativePath
				expected.UpdatedAt = before.UpdatedAt.Add(time.Hour)
			}
			assertWork := func() {
				t.Helper()
				got, err := reopened.WorkByID(ctx, work.ID)
				if err != nil || !reflect.DeepEqual(got, expected) {
					t.Fatal("work not preserved", got, expected, err)
				}
			}
			assertWork()
			freshName := filepath.Join(dir, filepath.FromSlash(fresh.RelativePath))
			assertFile := func(name string, data []byte, exists bool) {
				t.Helper()
				got, err := os.ReadFile(name)
				if !exists {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatal("file not absent", name, err)
					}
					return
				}
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("file not preserved", name, err)
				}
			}
			partial := source
			if mode == "partial" {
				partial = source[:len(source)/2]
			}
			assertFile(freshName, partial, mode != "reserved")
			assertFile(oldName, source, mode != "deleted")
			var operations CoverOperations
			cleaner := NewCoverImporter(reopened, &operations, dir, nil, time.Now)
			ticket, err := operations.BeginCleanup()
			if err != nil {
				t.Fatal(err)
			}
			warnings, err := cleaner.Cleanup(ctx, ticket)
			wantWarnings := []string{}
			if unverified {
				wantWarnings = []string{"cover_cleanup_unverified"}
			}
			if err != nil || !reflect.DeepEqual(warnings, wantWarnings) {
				t.Fatal(warnings, err)
			}
			assertWork()
			assertFile(filepath.Join(dir, "source.png"), source, true)
			assertFile(unknown, []byte("untouched"), true)
			assertFile(oldName, source, !committed)
			assertFile(freshName, partial, mode != "reserved" && mode != "confirmed")
			after, err := reopened.ListManagedCovers(ctx)
			wantCount := 1
			if unverified {
				wantCount = 2
			}
			if err != nil || len(after) != wantCount {
				t.Fatal(after, err)
			}
			for _, record := range after {
				if record.RelativePath != expected.ThumbnailPath && (!unverified || !reflect.DeepEqual(record, fresh)) {
					t.Fatal("wrong record retained", record)
				}
				for _, original := range records {
					if original.ID == record.ID && !reflect.DeepEqual(original, record) {
						t.Fatal("retained record changed", record)
					}
				}
			}
		})
	}
}
