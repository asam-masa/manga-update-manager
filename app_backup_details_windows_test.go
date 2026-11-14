package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func backupDetailsFixture(images, works int) backupSaveRunResult {
	r := backupSaveRunResult{Omissions: backupOmissionObservation{Known: true, WorkNamesState: backupWorkNamesAvailable}}
	for i := range images {
		id := fmt.Sprintf("%032x", i+1)
		r.Omissions.Images = append(r.Omissions.Images, backup.OmittedImage{ID: id, Path: "covers/" + id + ".png", Reason: "missing"})
	}
	for i := range works {
		r.Omissions.Works = append(r.Omissions.Works, storage.CopyCoverWork{CoverID: r.Omissions.Images[0].ID, WorkID: int64(i + 1), Title: fmt.Sprintf("保存時%d", i+1)})
	}
	return r
}

func retainedBackupDetails(t *testing.T, result backupSaveRunResult) (*backupRunState, application.BackupExecutionID) {
	t.Helper()
	s := &backupRunState{}
	run, err := s.start(context.Background(), "synthetic.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		return result, result.FinishError
	})
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, run.done)
	return s, run.id
}

func TestBackupDetailPageBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 49, 50, 51, 100, 101} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, id := retainedBackupDetails(t, backupDetailsFixture(count, count))
			seenImages, seenWorks := 0, 0
			pages := max(1, (count+49)/50)
			for page := 1; page <= pages; page++ {
				got, err := s.imagePage(id, page)
				if err != nil || got.ID != id || got.Page != page || got.Total != count || got.Pages != pages || got.PageSize != 50 || got.Items == nil || len(got.Items) > 50 {
					t.Fatal(got, err)
				}
				for _, image := range got.Items {
					seenImages++
					if image.ID != fmt.Sprintf("%032x", seenImages) {
						t.Fatal("image missing/duplicated", got)
					}
				}
				if count == 0 {
					continue
				}
				works, err := s.workPage(id, fmt.Sprintf("%032x", 1), page)
				if err != nil || works.ID != id || works.Total != count || works.Pages != pages || works.PageSize != 50 || len(works.Items) > 50 {
					t.Fatal(works, err)
				}
				for _, work := range works.Items {
					seenWorks++
					if work.WorkID != int64(seenWorks) {
						t.Fatal("shared-image work missing/duplicated", works)
					}
				}
			}
			if seenImages != count || seenWorks != count {
				t.Fatal(seenImages, seenWorks, count)
			}
			for _, page := range []int{-1, 0, pages + 1, int(^uint(0) >> 1)} {
				if _, err := s.imagePage(id, page); !errors.Is(err, errBackupDetailRequest) {
					t.Fatal(page, err)
				}
			}
		})
	}
}

func TestBackupDetailAvailability(t *testing.T) {
	for _, state := range []backupWorkNamesState{backupWorkNamesUnobserved, backupWorkNamesUnavailable, backupWorkNamesAvailable} {
		r := backupDetailsFixture(1, 0)
		r.Omissions.WorkNamesState = state
		r.Omissions.DetailError = storage.ErrCopyCoverWorkLimit
		r.Save.Publication.State = platform.BackupPublishUnknown
		s, id := retainedBackupDetails(t, r)
		view := s.overview()
		if view.result == nil || *view.result.OmittedImageCount != 1 || view.result.WorkNamesState != state || view.result.Save.Publication.State != platform.BackupPublishUnknown || !errors.Is(view.result.DetailError, storage.ErrCopyCoverWorkLimit) {
			t.Fatal(view)
		}
		page, err := s.workPage(id, r.Omissions.Images[0].ID, 1)
		want := errBackupDetailUnobserved
		if state == backupWorkNamesUnavailable {
			want = errBackupDetailUnavailable
		}
		if state == backupWorkNamesAvailable {
			if err != nil || page.Items == nil || page.Total != 0 || *view.result.WorkCount != 0 {
				t.Fatal(page, err, view)
			}
		} else if !errors.Is(err, want) || view.result.WorkCount != nil {
			t.Fatal(page, err, view)
		}
		if _, err := s.imagePage(id, 1); err != nil {
			t.Fatal("work-name failure erased image reasons", err)
		}
	}
	s, id := retainedBackupDetails(t, backupSaveRunResult{})
	if got := s.overview(); got.result.OmittedImageCount != nil || got.result.WorkCount != nil {
		t.Fatal("unobserved count became zero", got)
	}
	if _, err := s.imagePage(id, 1); !errors.Is(err, errBackupDetailUnobserved) {
		t.Fatal(err)
	}
}

func TestBackupDetailsLargeAndOwned(t *testing.T) {
	r := backupDetailsFixture(10_000, 50_000)
	r.Warnings = []CoverWarningDTO{{Message: "保持警告"}}
	r.Save.Cleanup = &platform.BackupDiscardResult{}
	r.FinishError = errors.New("synthetic finish error")
	s, id := retainedBackupDetails(t, r)
	view := s.overview()
	if view.execution.ID != id || *view.result.OmittedImageCount != 10_000 || *view.result.WorkCount != 50_000 || !errors.Is(view.result.Err, r.FinishError) || !errors.Is(view.result.FinishError, r.FinishError) {
		t.Fatal(view)
	}
	images, err := s.imagePage(id, 200)
	if err != nil || len(images.Items) != 50 || images.Items[49].ID != fmt.Sprintf("%032x", 10_000) {
		t.Fatal(images, err)
	}
	works, err := s.workPage(id, r.Omissions.Images[0].ID, 1000)
	if err != nil || len(works.Items) != 50 || works.Total != 50_000 || works.Items[49].WorkID != 50_000 {
		t.Fatal(works, err)
	}
	images.Items[0].Reason, works.Items[0].Title = "changed", "changed"
	view.result.Warnings[0].Message, view.result.Save.Cleanup.Absent, *view.result.WorkCount = "changed", true, -1
	again, _ := s.workPage(id, r.Omissions.Images[0].ID, 1000)
	imageAgain, _ := s.imagePage(id, 200)
	if again.Items[0].Title == "changed" || imageAgain.Items[0].Reason != "missing" || s.overview().result.Warnings[0].Message != "保持警告" || s.overview().result.Save.Cleanup.Absent || *s.overview().result.WorkCount != 50_000 {
		t.Fatal("observable view aliased retained result")
	}
	for _, imageID := range []string{"", "bad", "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"} {
		if _, err := s.workPage(id, imageID, 1); !errors.Is(err, errBackupDetailRequest) {
			t.Fatal(imageID, err)
		}
	}
	if _, err := s.workPage(id, fmt.Sprintf("%032x", 10_001), 1); !errors.Is(err, errBackupDetailImage) {
		t.Fatal(err)
	}
	if _, err := s.imagePage(0, 1); !errors.Is(err, errBackupDetailRequest) {
		t.Fatal(err)
	}
}

func TestBackupDetailLifetimeAndRace(t *testing.T) {
	var s backupRunState
	release, finish := holdBackupRun(t)
	run, err := s.start(context.Background(), "one.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		<-release
		return backupDetailsFixture(1, 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.overview(); got.result != nil || !got.execution.CanCancel {
		t.Fatal(got)
	}
	if _, err := s.imagePage(run.id, 1); !errors.Is(err, errBackupDetailPending) {
		t.Fatal(err)
	}
	finish()
	waitBackupRun(t, run.done)
	before := s.snapshot()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				page, err := s.imagePage(run.id, 1)
				if err == nil && (page.ID != run.id || page.Total != 1) || err != nil && !errors.Is(err, errBackupDetailStale) {
					t.Error(page, err)
				}
				got := s.overview()
				if (got.execution.State == application.BackupExecutionFinished) != (got.result != nil) {
					t.Error("state/result mismatch", got)
				}
			}
		})
	}
	if !reflect.DeepEqual(before, s.snapshot()) {
		t.Fatal("observation altered retained result")
	}
	next, err := s.start(context.Background(), "two.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		return backupDetailsFixture(2, 2), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, next.done)
	wg.Wait()
	if _, err := s.imagePage(run.id, 1); !errors.Is(err, errBackupDetailStale) {
		t.Fatal("old ID replaced by current", err)
	}
	s.close()
	if _, err := s.imagePage(next.id, 1); !errors.Is(err, errBackupDetailStale) || s.overview().execution.ID != 0 {
		t.Fatal("shutdown did not expire detail", err)
	}
}
