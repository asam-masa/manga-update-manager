package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/platform"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestAppBackupOmissionOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "detail", "limit", "unexpected", "sql-release", "cancel", "cancel-cause", "deadline", "stop", "db-stop", "post-sync", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			a, root := backupArchiveApp(t)
			works, err := a.store.ListWorks(context.Background())
			if err != nil || len(works) != 1 {
				t.Fatal(works, err)
			}
			// Only the synthetic fixture's managed image is removed.
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(works[0].ThumbnailPath))); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("synthetic detail or postprocessing failure")
			var copyPath string
			createWork := func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
				w, err := platform.CreateBackupDatabaseWork(ctx, root)
				if err == nil {
					copyPath = w.Path()
				}
				return w, err
			}
			loaded, outputCreated := false, false
			load := func(ctx context.Context, path string, ids []string) ([]storage.CopyCoverWork, error) {
				assertBackupImagesPaused(t, a)
				if path == "" || path != copyPath || len(ids) != 1 {
					t.Fatal("details not read from the owned copy", path, ids)
				}
				got, err := storage.DatabaseCopyCoverWorks(ctx, path, ids)
				if err != nil || len(got) != 1 || got[0].WorkID != works[0].ID || got[0].Title != works[0].Title {
					t.Fatal(got, err)
				}
				loaded = true
				switch mode {
				case "detail":
					err = errors.Join(storage.ErrDatabaseCopyCoverWorks, cause)
				case "limit":
					err = errors.Join(storage.ErrDatabaseCopyCoverWorks, storage.ErrCopyCoverWorkLimit)
				case "unexpected":
					err = cause
				case "sql-release":
					err = errors.Join(storage.ErrDatabaseCopyCoverWorks, storage.ErrDatabaseCopyRelease, cause)
				case "cancel":
					cancel()
				case "cancel-cause":
					err = errors.Join(storage.ErrDatabaseCopyCoverWorks, context.Canceled)
				case "deadline":
					err = errors.Join(storage.ErrDatabaseCopyCoverWorks, context.DeadlineExceeded)
				case "stop":
					a.coverOperations.Stop()
				case "db-stop":
					err = a.store.Close()
				}
				return got, err // Faults may return partial data: never retain it.
			}
			createOutput := func(ctx context.Context, target string) (backupSaveFile, error) {
				outputCreated = true
				f, err := platform.CreateBackupTemp(ctx, target)
				if err != nil {
					return nil, err
				}
				cleaned := true
				return &appSaveProbe{BackupTemp: f, a: a, t: t, mode: mode, cause: cause, cancel: cancel, dbCleaned: &cleaned}, nil
			}
			r, err := a.runBackupSaveObserved(ctx, filepath.Join(root, "result.zip"), time.Now(), "synthetic", createWork, createOutput, load, nil)
			canSave := mode == "success" || mode == "detail" || mode == "limit" || mode == "post-sync" || mode == "unknown"
			if !loaded || outputCreated != canSave || !r.Omissions.Known || len(r.Omissions.Images) != 1 || r.Omissions.Images[0].Reason != "missing" {
				t.Fatal("omission observation lost", r.Omissions, outputCreated, err)
			}
			available := mode == "success" || mode == "post-sync" || mode == "unknown"
			if available {
				if r.Omissions.WorkNamesState != backupWorkNamesAvailable || len(r.Omissions.Works) != 1 || r.Omissions.DetailError != nil {
					t.Fatal(r.Omissions)
				}
			} else if r.Omissions.WorkNamesState != backupWorkNamesUnavailable || r.Omissions.Works != nil || r.Omissions.DetailError == nil {
				t.Fatal("partial or unknown details became available", r.Omissions)
			}
			if mode == "success" || mode == "detail" || mode == "limit" {
				if err != nil || r.Save.Publication.State != platform.BackupPublishConfirmed {
					t.Fatal(r.Save, err)
				}
			} else if err == nil || r.Manifest != nil || r.Covers.OmittedImages != nil {
				t.Fatal("preparation artifacts survived failure", r, err)
			}
			if mode == "post-sync" && (r.Save.Publication.State != platform.BackupPublishConfirmed || !errors.Is(err, cause)) {
				t.Fatal("confirmed warning lost", r.Save, err)
			}
			if mode == "unknown" && r.Save.Publication.State != platform.BackupPublishUnknown {
				t.Fatal("unknown publication rewritten", r.Save, err)
			}
			if mode == "sql-release" && (!errors.Is(err, storage.ErrDatabaseCopyRelease) || !errors.Is(r.FinishError, errBackupReleaseUnverified)) {
				t.Fatal("SQL release mark lost", err, r.FinishError)
			}
			if (mode == "detail" && !errors.Is(r.Omissions.DetailError, cause)) || (mode == "limit" && !errors.Is(r.Omissions.DetailError, storage.ErrCopyCoverWorkLimit)) {
				t.Fatal("optional detail warning lost", r.Omissions)
			}
		})
	}
}

func TestAppBackupOmissionUnknownAndEmpty(t *testing.T) {
	a, root := backupArchiveApp(t)
	var before backupOmissionObservation
	if before.Known || before.WorkNamesState != backupWorkNamesUnobserved {
		t.Fatal(before)
	}
	r, err := a.saveBackupArchive(context.Background(), filepath.Join(root, "empty.zip"), time.Now(), "synthetic")
	if err != nil || !r.Omissions.Known || r.Omissions.Images == nil || len(r.Omissions.Images) != 0 || r.Omissions.WorkNamesState != backupWorkNamesAvailable || len(r.Omissions.Works) != 0 {
		t.Fatal("known empty and unobserved confused", r.Omissions, err)
	}
}

func TestAppBackupOmissionsSurviveLaterMetadataFailure(t *testing.T) {
	a, _ := backupArchiveApp(t)
	var observation backupOmissionObservation
	r, err := a.useBackupArchiveObserved(context.Background(), time.Now(), "synthetic",
		func(ctx context.Context, root string) (application.BackupDatabaseWork, error) {
			return platform.CreateBackupDatabaseWork(ctx, root)
		}, func(context.Context, string) ([]storage.ManagedCover, error) {
			return nil, storage.ErrDatabaseCopyRelease
		}, func(context.Context, []byte, io.ReaderAt, string, []storage.ManagedCover) error {
			t.Fatal("archive used after SQL release failure")
			return nil
		}, nil, func(ctx context.Context, path string, manifest []byte) error {
			var err error
			observation, err = a.observeBackupOmissions(ctx, path, manifest, storage.DatabaseCopyCoverWorks)
			return err
		}, nil)
	if !errors.Is(err, storage.ErrDatabaseCopyRelease) || !errors.Is(r.FinishError, errBackupReleaseUnverified) || r.Manifest != nil || r.Covers.Images != nil || !observation.Known || observation.WorkNamesState != backupWorkNamesAvailable {
		t.Fatal("confirmed observation lost with prepared artifacts", r, observation, err)
	}
}

func TestBackupRunRetainsUnavailableDetails(t *testing.T) {
	var s backupRunState
	cause := errors.Join(storage.ErrDatabaseCopyCoverWorks, storage.ErrDatabaseCopyRelease)
	run, err := s.start(context.Background(), "synthetic.zip", func(context.Context, application.BackupExecutionID) (backupSaveRunResult, error) {
		return backupSaveRunResult{Omissions: backupOmissionObservation{Known: true, WorkNamesState: backupWorkNamesUnavailable, DetailError: cause}}, cause
	})
	if err != nil {
		t.Fatal(err)
	}
	waitBackupRun(t, run.done)
	got := s.snapshot().result
	if got == nil || !errors.Is(got.err, storage.ErrDatabaseCopyRelease) || !got.save.Omissions.Known || got.save.Omissions.WorkNamesState != backupWorkNamesUnavailable || got.save.Omissions.Works != nil || !errors.Is(got.save.Omissions.DetailError, cause) {
		t.Fatal("unavailable details or release cause lost", got)
	}
}
