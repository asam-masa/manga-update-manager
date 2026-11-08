package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func copyCoverWork(t *testing.T, s *Store, path string) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO works(url,title,thumbnail_path,created_at,updated_at) VALUES(?,?,?,?,?)`,
		fmt.Sprintf("https://example.com/%d", faultDriverID.Add(1)), "合成作品", path, "2026-01-01T00:00:00.000000000Z", "2026-01-01T00:00:00.000000000Z")
	if err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseCopyCoversSnapshotAndLimits(t *testing.T) {
	s, source := managedRecordStore(t)
	a, b, unused := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	for _, id := range []string{b, a, unused} {
		readyCover(t, s, id)
	}
	for _, id := range []string{b, a, a} {
		copyCoverWork(t, s, "covers/"+id+".png")
	}
	copyCoverWork(t, s, "")
	path := filepath.Join(t.TempDir(), "コピー # 100%.sqlite")
	if err := s.CopyDatabase(bounded(t), path); err != nil {
		t.Fatal(err)
	}
	copyCoverWork(t, s, "covers/"+unused+".png")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := make(map[string][32]byte)
	for _, p := range []string{source, path} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = sha256.Sum256(data)
	}
	got, err := DatabaseCopyCovers(bounded(t), path, 2)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	for i, id := range []string{a, b} {
		if got[i].ID != id || got[i].RelativePath != "covers/"+id+".png" || got[i].State != "ready" || !reflect.DeepEqual(got[i].Info, ptrCopyInfo()) {
			t.Fatal(got[i])
		}
	}
	for _, limit := range []int{0, 1, -1, int(^uint(0) >> 1)} {
		got, err := DatabaseCopyCovers(bounded(t), path, limit)
		if got != nil || !errors.Is(err, ErrDatabaseCopyCovers) {
			t.Fatal(limit, got, err)
		}
	}
	for p, want := range before {
		data, err := os.ReadFile(p)
		if err != nil || sha256.Sum256(data) != want {
			t.Fatal("database changed", p, err)
		}
	}
}

func ptrCopyInfo() *ManagedCoverInfo { v := testManagedInfo(); return &v }

func TestDatabaseCopyCoversEmptyAndInvalidRecords(t *testing.T) {
	for _, mode := range []string{"empty", "missing", "reserved", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			s, path := managedRecordStore(t)
			if mode != "empty" {
				readyCover(t, s, strings.Repeat("a", 32))
				copyCoverWork(t, s, "covers/"+strings.Repeat("a", 32)+".png")
				id := strings.Repeat("b", 32)
				if mode == "reserved" {
					if err := s.ReserveManagedCover(bounded(t), id); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "invalid" {
					readyCover(t, s, id)
					if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
						t.Fatal(err)
					}
					if _, err := s.db.Exec("UPDATE managed_covers SET byte_size=0 WHERE id=?", id); err != nil {
						t.Fatal(err)
					}
				}
				copyCoverWork(t, s, "covers/"+id+".png")
			}
			got, err := DatabaseCopyCovers(bounded(t), path, 10)
			if mode == "empty" {
				if err != nil || got == nil || len(got) != 0 {
					t.Fatal(got, err)
				}
				return
			}
			if got != nil || !errors.Is(err, ErrDatabaseCopyCovers) || errors.Is(err, ErrDatabaseCopyRelease) {
				t.Fatal(got, err)
			}
		})
	}
}

func TestDatabaseCopyCoversPathAndCancellation(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	for _, path := range []string{missing, "relative.sqlite", "file:example.sqlite", "bad\x00path"} {
		got, err := DatabaseCopyCovers(bounded(t), path, 10)
		if got != nil || !errors.Is(err, ErrDatabaseCopyCovers) {
			t.Fatal(got, err)
		}
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bounded(t))
	cancel()
	got, err := DatabaseCopyCovers(ctx, missing, 10)
	if got != nil || !errors.Is(err, context.Canceled) || errors.Is(err, ErrDatabaseCopyRelease) {
		t.Fatal(got, err)
	}
}

func TestDatabaseCopyCoversCleanupFailureDiscardsResults(t *testing.T) {
	s, path := managedRecordStore(t)
	readyCover(t, s, testCoverID)
	copyCoverWork(t, s, "covers/"+testCoverID+".png")
	for _, mode := range []string{"rows", "rollback", "connection", "cancel-close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(bounded(t))
			defer cancel()
			name := fmt.Sprintf("copy-covers-%d", faultDriverID.Add(1))
			cause := syntheticCleanup
			switch mode {
			case "connection":
				sql.Register(name, validationCloseDriver{})
				cause = validationCloseFault
			case "cancel-close":
				sql.Register(name, validationCloseDriver{cancel: cancel})
				cause = context.Canceled
			default:
				sql.Register(name, cleanupDriver{rows: mode == "rows", commit: mode == "rows"})
			}
			db, err := sql.Open(name, path)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			got, err := readDatabaseCopyCovers(ctx, db, 10)
			if got != nil || !errors.Is(err, cause) {
				t.Fatal(got, err)
			}
			if errors.Is(err, ErrDatabaseCopyRelease) != (mode != "cancel-close") {
				t.Fatal("wrong release classification", err)
			}
			if err := db.Ping(); err == nil {
				t.Fatal("connection not closed")
			}
		})
	}
}
