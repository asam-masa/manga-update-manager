package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUnusedCoverAndConditionalForget(t *testing.T) {
	ctx := context.Background()
	s, _ := managedRecordStore(t)
	work := coverReferenceWork(t, s)
	// Keep unrelated legacy paths out of this test's canonical reference set.
	if _, _, err := s.RemoveWorkCover(ctx, work.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UnusedManagedCover(ctx, testCoverID); !errors.Is(err, ErrManagedCoverNotFound) {
		t.Fatal(err)
	}
	if err := s.ReserveManagedCover(ctx, testCoverID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UnusedManagedCover(ctx, testCoverID); !errors.Is(err, ErrInvalidManagedCover) {
		t.Fatal(err)
	}
	if err := s.ConfirmManagedCover(ctx, testCoverID, testManagedInfo()); err != nil {
		t.Fatal(err)
	}
	cover, unused, err := s.UnusedManagedCover(ctx, testCoverID)
	if err != nil || !unused || cover.Info == nil {
		t.Fatal(cover, unused, err)
	}
	if _, _, err := s.SetWorkCover(ctx, work.ID, testCoverID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, unused, err := s.UnusedManagedCover(ctx, testCoverID); err != nil || unused {
		t.Fatal(unused, err)
	}
	if err := s.ForgetUnusedManagedCover(ctx, cover); !errors.Is(err, ErrManagedCoverChanged) {
		t.Fatal("referenced record removed", err)
	}
	if _, _, err := s.RemoveWorkCover(ctx, work.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	modified := cover
	info := *cover.Info
	info.SHA256[0] ^= 1
	modified.Info = &info
	if err := s.ForgetUnusedManagedCover(ctx, modified); !errors.Is(err, ErrManagedCoverChanged) {
		t.Fatal("different record accepted", err)
	}
	if err := s.ForgetUnusedManagedCover(ctx, cover); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ManagedCoverByID(ctx, testCoverID); !errors.Is(err, ErrManagedCoverNotFound) {
		t.Fatal(err)
	}
}

func TestCoverCleanupDBFailureAndStop(t *testing.T) {
	ctx := context.Background()
	s, _ := managedRecordStore(t)
	readyCover(t, s, testCoverID)
	cover, _, err := s.UnusedManagedCover(ctx, testCoverID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_cleanup BEFORE DELETE ON managed_covers BEGIN SELECT RAISE(ABORT,'synthetic cleanup rejection'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetUnusedManagedCover(ctx, cover); err == nil {
		t.Fatal("trigger ignored")
	}
	if _, err := s.ManagedCoverByID(ctx, testCoverID); err != nil {
		t.Fatal("record lost", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.UnusedManagedCover(cancelled, testCoverID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.ForgetUnusedManagedCover(cancelled, cover); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.admission.stop()
	if _, _, err := s.UnusedManagedCover(ctx, testCoverID); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
	if err := s.ForgetUnusedManagedCover(ctx, cover); !errors.Is(err, ErrDBStopped) {
		t.Fatal(err)
	}
}

func TestCoverCleanupReferenceAliases(t *testing.T) {
	for _, mode := range []string{"case", "separator", "trailing-dot", "stream", "short-name", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _ := managedRecordStore(t)
			readyCover(t, s, testCoverID)
			cover, unused, err := s.UnusedManagedCover(ctx, testCoverID)
			if err != nil || !unused {
				t.Fatal(cover, unused, err)
			}
			path := "covers/" + testCoverID + ".png"
			switch mode {
			case "case":
				path = strings.ToUpper(path)
			case "separator":
				path = strings.ReplaceAll(strings.ToUpper(path), "/", `\`)
			case "trailing-dot":
				path += "."
			case "stream":
				path += ":stream"
			case "short-name":
				path = "COVERS/012345~1.PNG"
			case "legacy":
				path = "legacy/unrelated.png"
			}
			work := coverReferenceWork(t, s)
			// Model pre-existing stored spellings without normalizing them.
			if _, err := s.db.Exec("UPDATE works SET thumbnail_path=? WHERE id=?", path, work.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "case" {
				other := work
				other.ID, other.LastAccessedAt = 0, nil
				other.URL += "/legacy"
				other.ThumbnailPath = "legacy/other.png"
				if _, err := s.CreateWork(ctx, other); err != nil {
					t.Fatal(err)
				}
			}
			_, unused, err = s.UnusedManagedCover(ctx, testCoverID)
			if mode == "case" || mode == "separator" {
				if err != nil || unused {
					t.Fatal("shared alias not detected", unused, err)
				}
				if err := s.ForgetUnusedManagedCover(ctx, cover); !errors.Is(err, ErrManagedCoverChanged) {
					t.Fatal(err)
				}
			} else {
				if !errors.Is(err, ErrUncertainCoverReferences) || unused {
					t.Fatal("unknown alias accepted", unused, err)
				}
				if err := s.ForgetUnusedManagedCover(ctx, cover); !errors.Is(err, ErrUncertainCoverReferences) {
					t.Fatal(err)
				}
			}
			if _, err := s.ManagedCoverByID(ctx, testCoverID); err != nil {
				t.Fatal("record removed", err)
			}
		})
	}
}
