package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/settings"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func TestTagSettingsAPIsRefuseBackupWithoutChanges(t *testing.T) {
	a := testApp(t, t.TempDir())
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/tag", Title: "One"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{"Before"}})); err != nil {
		t.Fatal(err)
	}
	tags, err := a.LoadTags()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := a.LoadTagDeletePreview(tags[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetRegistrationFormVisible(false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWorkListMode("rows"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWorkSort("title_desc"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetShowFutureSchedules(false); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.LoadLibrarySnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op, err := a.operationGate.BeginBackup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer op.End()
	calls := map[string]func() error{
		"tags":        func() error { _, e := a.LoadTags(); return e },
		"work-tags":   func() error { _, e := a.LoadWorkTags(w.ID); return e },
		"usage":       func() error { _, e := a.LoadTagUsage(); return e },
		"preview":     func() error { _, e := a.LoadTagDeletePreview(tags[0].ID); return e },
		"rename":      func() error { _, e := a.RenameTag(tags[0], "Changed"); return e },
		"delete":      func() error { _, e := a.DeleteTag(preview); return e },
		"form-load":   func() error { _, e := a.LoadDisplaySettings(); return e },
		"form-save":   func() error { return a.SetRegistrationFormVisible(true) },
		"mode-load":   func() error { _, e := a.LoadWorkListMode(); return e },
		"mode-save":   func() error { return a.SetWorkListMode("compact") },
		"sort-load":   func() error { _, e := a.LoadWorkSort(); return e },
		"sort-save":   func() error { return a.SetWorkSort("registration_asc") },
		"future-load": func() error { _, e := a.LoadShowFutureSchedules(); return e },
		"future-save": func() error { return a.SetShowFutureSchedules(true) },
	}
	for name, call := range calls {
		if err := call(); errorCode(t, err) != "backup_in_progress" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	after, err := a.store.LoadLibrarySnapshot(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("library changed %v", err)
	}
	form, e1 := a.store.DisplaySettings(ctx)
	mode, e2 := a.store.WorkListMode(ctx)
	order, e3 := a.store.WorkSort(ctx)
	future, e4 := a.store.ShowFutureSchedules(ctx)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || form.RegistrationFormVisible || mode != settings.Rows || order != settings.TitleDesc || future {
		t.Fatalf("settings changed %#v %s %s %v %v %v %v %v", form, mode, order, future, e1, e2, e3, e4)
	}
	op.End()
	if _, err := a.RenameTag(tags[0], "Changed"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetRegistrationFormVisible(true); err != nil {
		t.Fatal(err)
	}
}

type blockingSettingsAdmission struct {
	application.DisplaySettingsRepository
	read, write func(context.Context) error
}

func (r blockingSettingsAdmission) DisplaySettings(ctx context.Context) (settings.Display, error) {
	if r.read != nil {
		if err := r.read(ctx); err != nil {
			return settings.Display{}, err
		}
	}
	return r.DisplaySettingsRepository.DisplaySettings(ctx)
}
func (r blockingSettingsAdmission) SetRegistrationFormVisible(ctx context.Context, v bool) error {
	if r.write != nil {
		if err := r.write(ctx); err != nil {
			return err
		}
	}
	return r.DisplaySettingsRepository.SetRegistrationFormVisible(ctx, v)
}

func TestBackupWaitsForTagAndSettingsOperations(t *testing.T) {
	for _, kind := range []string{"rename", "delete", "settings-read", "settings-write", "settings-write-cancel"} {
		t.Run(kind, func(t *testing.T) {
			cancelWait := kind == "settings-write-cancel"
			if cancelWait {
				kind = "settings-write"
			}
			a := testApp(t, t.TempDir())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a.ctx = ctx
			w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/tag", Title: "One"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{"Before"}})); err != nil {
				t.Fatal(err)
			}
			tags, err := a.LoadTags()
			if err != nil {
				t.Fatal(err)
			}
			preview, err := a.LoadTagDeletePreview(tags[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			block := func(ctx context.Context) error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			a.tagManager = application.NewTagManager(managementChangeRepository{
				rename: func(ctx context.Context, tag manga.Tag, name string) (manga.Tag, error) {
					if err := block(ctx); err != nil {
						return manga.Tag{}, err
					}
					return a.store.RenameTag(ctx, tag, name)
				},
				remove: func(ctx context.Context, p storage.TagDeletePreview) (manga.Tag, error) {
					if err := block(ctx); err != nil {
						return manga.Tag{}, err
					}
					return a.store.DeleteTag(ctx, p)
				},
			})
			r := blockingSettingsAdmission{DisplaySettingsRepository: a.store}
			if kind == "settings-read" {
				r.read = block
			} else {
				r.write = block
			}
			a.display = application.NewDisplaySettings(r)
			done := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "rename":
					_, err = a.RenameTag(tags[0], "Changed")
				case "delete":
					_, err = a.DeleteTag(preview)
				case "settings-read":
					_, err = a.LoadDisplaySettings()
				case "settings-write":
					err = a.SetRegistrationFormVisible(false)
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			type outcome struct {
				op  *application.BackupOperation
				err error
			}
			ready := make(chan outcome, 1)
			backupCtx, stopBackup := context.WithCancel(ctx)
			defer stopBackup()
			go func() { op, err := a.operationGate.BeginBackup(backupCtx); ready <- outcome{op, err} }()
			waitAppGate(t, a, application.ErrOperationsPaused)
			select {
			case <-ready:
				t.Fatal("backup passed unfinished operation")
			default:
			}
			if cancelWait {
				stopBackup()
				select {
				case r := <-ready:
					if r.op != nil || !errors.Is(r.err, context.Canceled) {
						t.Fatalf("cancel %#v", r)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if err := a.SetWorkSort("title_asc"); err != nil {
					t.Fatal("admission not restored", err)
				}
			}
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if cancelWait {
				op, err := a.operationGate.BeginBackup(ctx)
				ready <- outcome{op, err}
			}
			select {
			case r := <-ready:
				if r.err != nil || r.op == nil {
					t.Fatalf("backup %#v", r)
				}
				defer r.op.End()
				if !a.mu.TryLock() {
					t.Fatal("App lock retained")
				}
				a.mu.Unlock()
				end, err := a.workOperations.BeginTagChange(1)
				if err != nil {
					t.Fatal("tag ticket retained", err)
				}
				end()
				if kind == "settings-write" {
					v, err := a.store.DisplaySettings(ctx)
					if err != nil || v.RegistrationFormVisible {
						t.Fatalf("save not finished %#v %v", v, err)
					}
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestTagSettingsEarlyErrorsReleaseNormalTicket(t *testing.T) {
	a := testApp(t, t.TempDir())
	for _, call := range []func() error{
		func() error { _, e := a.RenameTag(TagDTO{}, "bad"); return e },
		func() error { _, e := a.DeleteTag(TagDeletePreviewDTO{}); return e },
		func() error { _, e := a.LoadTagDeletePreview("bad"); return e },
		func() error { return a.SetWorkListMode("bad") },
		func() error { return a.SetWorkSort("bad") },
	} {
		if err := call(); errorCode(t, err) != "invalid_input" {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		op, err := a.operationGate.BeginBackup(ctx)
		cancel()
		if err != nil {
			t.Fatal("normal ticket leaked", err)
		}
		op.End()
	}
}
