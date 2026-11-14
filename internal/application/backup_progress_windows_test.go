package application

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestBackupDatabaseProgressEntryPoints(t *testing.T) {
	for _, mode := range []string{"success", "copy", "validation", "cancel-copy"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			work := &databaseWorkFake{}
			var stages []BackupStage
			report := func(stage BackupStage) {
				stages = append(stages, stage)
				work.calls = append(work.calls, "notify:"+string(stage))
			}
			copier := databaseCopyFunc(func(context.Context, string) error {
				if stages[len(stages)-1] != BackupStageDatabase {
					t.Fatal("copy entered without its notification")
				}
				if mode == "copy" {
					return databaseStageFault
				}
				if mode == "cancel-copy" {
					cancel()
				}
				return nil
			})
			r, err := UseBackupDatabaseWithProgress(ctx, work, copier, func(context.Context, string) error {
				if stages[len(stages)-1] != BackupStageDatabaseCheck {
					t.Fatal("validation entered without its notification")
				}
				if mode == "validation" {
					return databaseStageFault
				}
				return nil
			}, func(context.Context, io.ReaderAt, int64) error { return nil }, report)
			want := []BackupStage{BackupStageDatabase}
			if mode == "validation" || mode == "success" {
				want = append(want, BackupStageDatabaseCheck)
			}
			if mode == "success" {
				want = append(want, BackupStageDatabaseCheck)
			}
			want = append(want, BackupStageCleanup)
			if !reflect.DeepEqual(stages, want) || work.calls[len(work.calls)-2] != "notify:cleanup" || work.calls[len(work.calls)-1] != "cleanup" || (err == nil) != (mode == "success") {
				t.Fatal(stages, want, work.calls, r, err)
			}
			if mode == "cancel-copy" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancel was rewritten", err)
			}
		})
	}
}
