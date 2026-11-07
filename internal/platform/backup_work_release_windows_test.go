package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackupWorkReleaseSeparatesAbsenceAndRelease(t *testing.T) {
	for _, stage := range []string{"success", "absence", "child", "parents", "both", "unknown"} {
		t.Run(stage, func(t *testing.T) {
			order := ""
			d, ops := workDiscardModel(&order)
			childFailure, parentFailure, absentFailure := errors.New("child release"), errors.New("parent release"), errors.New("absence check")
			d.closeFile = func() error {
				order += "close;"
				if stage == "child" || stage == "both" {
					return childFailure
				}
				return nil
			}
			if stage != "unknown" {
				d.parent = &BackupDirectoryGuard{handles: []windows.Handle{1, 2}, close: func(windows.Handle) error {
					order += "parent;"
					if stage == "parents" || stage == "both" {
						return parentFailure
					}
					return nil
				}}
			}
			if stage == "absence" {
				ops.absent = func(string) error { return absentFailure }
			}
			if r := d.ReleaseResult(); r.Confirmed() || !r.HandleAcquired || r.HandleClosed || r.CloseAttempted {
				t.Fatalf("premature release: %+v", r)
			}
			_, err := d.discardEmpty(ops)
			r := d.ReleaseResult()
			if r.Confirmed() != (stage == "success" || stage == "absence") || !r.CloseAttempted || r.HandleClosed != (stage != "child" && stage != "both") {
				t.Fatalf("wrong release observation: %+v / %v", r, err)
			}
			if stage == "absence" && (!errors.Is(err, absentFailure) || errors.Is(err, ErrBackupWorkDirectoryRelease)) {
				t.Fatalf("absence confused with release failure: %v", err)
			}
			if (stage == "child" || stage == "both") && !errors.Is(err, childFailure) {
				t.Fatal(err)
			}
			if (stage == "parents" || stage == "both") && !errors.Is(err, parentFailure) {
				t.Fatal(err)
			}
			before := order
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = d.Close()
					if again := d.ReleaseResult(); again != r {
						t.Errorf("release changed: %+v", again)
					}
				}()
			}
			wg.Wait()
			if order != before {
				t.Fatal("release retried", order)
			}
		})
	}
}

func TestBackupWorkReleaseCreationFailures(t *testing.T) {
	for _, stage := range []string{"early", "parent-failure", "parent-unknown", "create-failure", "child-close-failure"} {
		t.Run(stage, func(t *testing.T) {
			original, releaseFailure := errors.New("original operation"), errors.New("release failure")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "early" {
				cancel()
			}
			parentCalls := 0
			ops := backupWorkDirectoryOps{
				find: func() error { return nil },
				hold: func(context.Context, string) (io.Closer, error) {
					if stage == "parent-failure" {
						return nil, &BackupDirectoryHoldError{Release: BackupDirectoryReleaseResult{Acquired: 2, Released: 1, CloseAttempted: true}, Err: errors.Join(original, releaseFailure)}
					}
					if stage == "parent-unknown" {
						return nil, original
					}
					return &BackupDirectoryGuard{handles: []windows.Handle{1}, close: func(windows.Handle) error { parentCalls++; return nil }}, nil
				},
				create: func(string) (*os.File, error) {
					if stage == "create-failure" {
						return nil, original
					}
					f, err := os.CreateTemp(t.TempDir(), "synthetic")
					if err != nil {
						t.Fatal(err)
					}
					if err := f.Close(); err != nil {
						t.Fatal(err)
					}
					return f, nil // Subsequent Close must fail, not confirm release.
				},
				identify: func(*os.File) (BackupWorkDirectoryIdentity, error) { return BackupWorkDirectoryIdentity{}, original },
			}
			d, err := createBackupWorkDirectory(ctx, `C:\synthetic`, [16]byte{}, ops)
			var detail *BackupWorkDirectoryError
			if d != nil || !errors.As(err, &detail) {
				t.Fatal(d, err)
			}
			want := original
			if stage == "early" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatal("lost original cause", err)
			}
			r := detail.Release
			if r.Confirmed() != (stage == "early" || stage == "create-failure") || !r.CloseAttempted || r.HandleAcquired != (stage == "child-close-failure") {
				t.Fatalf("incorrect failure observation: %+v / %v", r, err)
			}
			if stage == "parent-failure" && (!errors.Is(err, releaseFailure) || r.Parents.Released != 1) {
				t.Fatal(r, err)
			}
			if stage == "child-close-failure" && (!errors.Is(err, os.ErrClosed) || !errors.Is(err, ErrBackupWorkDirectoryRelease) || r.HandleClosed) {
				t.Fatal(r, err)
			}
			if parentCalls != map[bool]int{true: 1, false: 0}[stage == "create-failure" || stage == "child-close-failure"] {
				t.Fatal("parent release skipped/retried", parentCalls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CreateBackupWorkDirectory(ctx, `C:\synthetic`)
	var detail *BackupWorkDirectoryError
	if !errors.As(err, &detail) || !errors.Is(err, context.Canceled) || !detail.Release.Confirmed() || detail.Created {
		t.Fatal("public early refusal lost release result", err)
	}
	if (BackupWorkDirectoryReleaseResult{}).Confirmed() {
		t.Fatal("zero result confirmed")
	}
}

func TestBackupWorkReleaseConcurrentFirstClose(t *testing.T) {
	childCalls, parentCalls := 0, 0
	d := &BackupWorkDirectory{
		closeFile: func() error { childCalls++; return nil },
		parent:    &BackupDirectoryGuard{handles: []windows.Handle{1, 2}, close: func(windows.Handle) error { parentCalls++; return nil }},
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Close(); err != nil {
				t.Error(err)
			}
			if r := d.ReleaseResult(); !r.Confirmed() || !r.HandleClosed || r.Parents.Acquired != 2 || r.Parents.Released != 2 {
				t.Errorf("incomplete release: %+v", r)
			}
		}()
	}
	wg.Wait()
	if childCalls != 1 || parentCalls != 2 {
		t.Fatal("duplicate/skipped close", childCalls, parentCalls)
	}
}
