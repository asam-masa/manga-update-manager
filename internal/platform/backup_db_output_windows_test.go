package platform

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func outputWork(t *testing.T) *BackupWorkDirectory {
	t.Helper()
	work, err := createBackupWorkDirectory(context.Background(), workDirectoryRoot(t), [16]byte{}, workDirectoryOps(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = work.Close() })
	return work
}

func TestBackupDBOutputSQLiteRoundTrip(t *testing.T) {
	work := outputWork(t)
	out, err := createBackupDBOutput(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	defer out.file.Close()
	source, err := sql.Open("sqlite", filepath.Join(filepath.Dir(work.Path()), "source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Exec("CREATE TABLE sample(title TEXT); INSERT INTO sample VALUES ('synthetic title')"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec("VACUUM INTO ?", out.path); err != nil {
		t.Fatal(err)
	}
	if id, err := identifyBackupTemp(out.file); err != nil || id != out.id {
		t.Fatal("output replaced", id, err)
	}
	copyDB, err := sql.Open("sqlite", out.path)
	if err != nil {
		t.Fatal(err)
	}
	var title, integrity string
	readErr := copyDB.QueryRow("SELECT title FROM sample").Scan(&title)
	checkErr := copyDB.QueryRow("PRAGMA quick_check").Scan(&integrity)
	if err := errors.Join(readErr, checkErr, copyDB.Close()); err != nil || title != "synthetic title" || integrity != "ok" {
		t.Fatal(title, integrity, err)
	}
	owner, _, err := transitionBackupDB(out.file, out.id, out.path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	result, err := finalizeBackupDBWork(owner, work)
	if err != nil || !result.file.absent || !result.directory.Absent {
		t.Fatal(result, err)
	}
}

func TestBackupDBOutputRefusesReuseAndHolds(t *testing.T) {
	work := outputWork(t)
	out, err := createBackupDBOutput(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	defer out.file.Close()
	if _, err := out.file.WriteString("sentinel"); err != nil {
		t.Fatal(err)
	}
	if got, err := createBackupDBOutput(context.Background(), work); got != nil || err == nil {
		t.Fatal(got, err)
	} else {
		var detail *backupDBOutputError
		if !errors.As(err, &detail) || detail.created || detail.path != out.path {
			t.Fatal(err)
		}
	}
	if data, err := os.ReadFile(out.path); err != nil || string(data) != "sentinel" {
		t.Fatal("existing content changed", err)
	}
	if err := os.Rename(out.path, out.path+"-moved"); err == nil {
		t.Fatal("retained output moved")
	}
	if err := work.VerifyIdentity(); err != nil {
		t.Fatal("work ownership lost", err)
	}
}

func TestBackupDBOutputCreationFaults(t *testing.T) {
	fault := errors.New("observe failure")
	for _, mode := range []string{"observe", "name", "nonempty", "cancel", "closed"} {
		t.Run(mode, func(t *testing.T) {
			work := outputWork(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var captured *os.File
			out, err := prepareBackupDBOutput(ctx, work, func(name string) (*os.File, error) {
				f, err := createBackupDBOutputHandle(name)
				captured = f
				return f, err
			}, func(f *os.File) (backupDBObservation, error) {
				if mode == "observe" {
					return backupDBObservation{}, fault
				}
				if mode == "closed" {
					if err := f.Close(); err != nil {
						t.Fatal(err)
					}
					return backupDBObservation{}, fault
				}
				o, err := observeBackupDB(f)
				if mode == "name" {
					o.name += "-other"
				}
				if mode == "nonempty" {
					o.size = 1
				}
				if mode == "cancel" {
					cancel()
				}
				return o, err
			})
			var detail *backupDBOutputError
			if out != nil || !errors.As(err, &detail) || !detail.created || detail.path != filepath.Join(work.Path(), "database.sqlite") {
				t.Fatal(out, err)
			}
			if _, err := captured.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
				t.Fatal("handle not closed", err)
			}
			if _, err := os.Stat(detail.path); err != nil {
				t.Fatal("artifact deleted", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if (mode == "observe" || mode == "closed") && !errors.Is(err, fault) {
				t.Fatal("lost cause", err)
			}
			if mode == "closed" && !errors.Is(err, os.ErrClosed) {
				t.Fatal("lost close failure", err)
			}
			if err := work.VerifyIdentity(); err != nil {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := createBackupDBOutput(ctx, nil); out != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(out, err)
	}
}
