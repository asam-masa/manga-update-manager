package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

func publicTagEdit(w WorkDTO, tags *TagEditInput) EditWorkInput {
	return EditWorkInput{URL: w.URL, Title: w.Title, SiteName: w.SiteName, Notes: w.Notes, Tags: tags}
}

func TestTagAPIWireAndPersistence(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	if got, err := a.LoadTags(); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/tag-api", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.LoadWorkTags(w.ID); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	result, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{" 異世界 ", "連載中", "異世界"}}))
	if err != nil {
		t.Fatal(err)
	}
	w = result.Work
	tags, err := a.LoadWorkTags(w.ID)
	if err != nil || len(tags) != 2 || tags[0].ID != "1" || tags[0].Name != "異世界" {
		t.Fatal(tags, err)
	}
	all, err := a.LoadTags()
	if err != nil || !reflect.DeepEqual(all, tags) {
		t.Fatal(all, err)
	}
	result, err = a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{Existing: []TagDTO{tags[1], tags[0]}, NewNames: []string{"異世界"}}))
	if err != nil || result.Work.UpdatedAt != w.UpdatedAt {
		t.Fatal(result, err)
	}
	// JSON omission and null preserve; explicit {} is a clear operation.
	for _, suffix := range []string{"", `,"tags":null`} {
		wire, _ := json.Marshal(publicTagEdit(w, nil))
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(wire, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, "tags")
		base, _ := json.Marshal(fields)
		payload := strings.TrimSuffix(string(base), "}") + suffix + "}"
		var input EditWorkInput
		if err := json.Unmarshal([]byte(payload), &input); err != nil {
			t.Fatal(err)
		}
		if _, err := a.EditWork(w.ID, input); err != nil {
			t.Fatal(err)
		}
		if got, err := a.LoadWorkTags(w.ID); err != nil || !reflect.DeepEqual(got, tags) {
			t.Fatal(got, err)
		}
	}
	a.shutdown(context.Background())
	a = testApp(t, dir)
	if got, err := a.LoadWorkTags(w.ID); err != nil || !reflect.DeepEqual(got, tags) {
		t.Fatal(got, err)
	}
	input := publicTagEdit(w, nil)
	clearWire, _ := json.Marshal(input)
	var clearFields map[string]json.RawMessage
	if err := json.Unmarshal(clearWire, &clearFields); err != nil {
		t.Fatal(err)
	}
	clearFields["tags"] = json.RawMessage(`{}`)
	clearWire, _ = json.Marshal(clearFields)
	if err := json.Unmarshal(clearWire, &input); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(w.ID, input); err != nil {
		t.Fatal(err)
	}
	if got, err := a.LoadWorkTags(w.ID); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if got, err := a.LoadTags(); err != nil || !reflect.DeepEqual(got, tags) {
		t.Fatal(got, err)
	}
}

func TestTagAPIInputValidationAndIDPrecision(t *testing.T) {
	a := NewApp()
	a.ctx = context.Background()
	called := false
	now := time.Now().UTC()
	stored := manga.Work{ID: 1, URL: "https://example.com/precision", Title: "作品", CreatedAt: now, UpdatedAt: now}
	var captured manga.EditWorkInput
	a.editor = application.NewWorkEditor(appEditRepositoryFunc(func(_ context.Context, _ int64, input manga.EditWorkInput, _ time.Time) (manga.Work, error) {
		called = true
		captured = input
		return stored, nil
	}), time.Now)
	for _, id := range []string{"", "0", "-1", "+1", "01", " 1", "1e3", "9223372036854775808"} {
		called = false
		_, err := a.EditWork(1, publicTagEdit(workDTO(stored), &TagEditInput{Existing: []TagDTO{{ID: id, Name: "タグ"}}}))
		if errorCode(t, err) != "invalid_input" || called {
			t.Fatal(id, called, err)
		}
	}
	called = false
	if _, err := a.EditWork(1, publicTagEdit(workDTO(stored), &TagEditInput{NewNames: []string{strings.Repeat("漫", 51)}})); errorCode(t, err) != "invalid_input" || called {
		t.Fatal(called, err)
	}
	for _, id := range []int64{9007199254740993, 9223372036854775807} {
		a.tags = application.NewTagReader(apiTagRepository{values: []manga.Tag{{ID: id, Name: "タグ"}}})
		values, err := a.LoadTags()
		if err != nil {
			t.Fatal(err)
		}
		wire, _ := json.Marshal(values)
		var decoded []TagDTO
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(wire), `"id":"`) {
			t.Fatal(string(wire))
		}
		if _, err := a.EditWork(1, publicTagEdit(workDTO(stored), &TagEditInput{Existing: decoded})); err != nil || captured.Tags.Existing[0].ID != id {
			t.Fatal(captured, err)
		}
	}
}

func TestTagAPIChangedAndDeleteSnapshots(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/tag-snapshot", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{"タグ"}}))
	if err != nil {
		t.Fatal(err)
	}
	w = result.Work
	tags, err := a.LoadWorkTags(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	deletion := deleteSnapshot(w)
	deletion.Tags = &tags
	db, err := sql.Open("sqlite", filepath.Join(dir, "manga.sqlite")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE tags SET name='改名' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	input := publicTagEdit(w, &TagEditInput{Existing: tags, NewNames: []string{"未確定"}})
	input.Title = "保存しない"
	if _, err := a.EditWork(w.ID, input); errorCode(t, err) != "tags_changed" {
		t.Fatal(err)
	}
	if _, err := a.DeleteWork(w.ID, deletion); errorCode(t, err) != "work_changed" {
		t.Fatal(err)
	}
	if got, err := a.store.WorkByID(context.Background(), w.ID); err != nil || got.Title != w.Title {
		t.Fatal(got, err)
	}
	if got, err := a.LoadTags(); err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if _, err := db.Exec("DELETE FROM tags WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(w.ID, input); errorCode(t, err) != "tags_changed" {
		t.Fatal(err)
	}
	// A fresh empty snapshot is valid, and an invalid ID is rejected before delete.
	empty := []TagDTO{}
	deletion.Tags = &empty
	bad := []TagDTO{{ID: "01", Name: "タグ"}}
	badDelete := deletion
	badDelete.Tags = &bad
	if _, err := a.DeleteWork(w.ID, badDelete); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	if _, err := a.DeleteWork(w.ID, deletion); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LoadWorkTags(w.ID); errorCode(t, err) != "work_not_found" {
		t.Fatal(err)
	}
}

type apiTagRepository struct {
	values []manga.Tag
	cause  error
	onRead func(context.Context, int64)
}

func (r apiTagRepository) ListTags(ctx context.Context) ([]manga.Tag, error) {
	if r.onRead != nil {
		r.onRead(ctx, 0)
	}
	return r.values, r.cause
}
func (r apiTagRepository) WorkTags(ctx context.Context, id int64) ([]manga.Tag, error) {
	if r.onRead != nil {
		r.onRead(ctx, id)
	}
	return r.values, r.cause
}

func TestTagAPIReadFailureSafety(t *testing.T) {
	private := errors.New("private diagnostic")
	for _, cause := range []error{private, context.Canceled, manga.ErrTagName, errors.Join(storage.ErrDBStopped, manga.ErrWorkNotFound, private)} {
		a := NewApp()
		a.ctx = context.Background()
		a.tags = application.NewTagReader(apiTagRepository{values: []manga.Tag{{ID: 1, Name: "部分値"}}, cause: cause})
		for _, read := range []func() ([]TagDTO, error){a.LoadTags, func() ([]TagDTO, error) { return a.LoadWorkTags(1) }} {
			got, err := read()
			want := "storage_error"
			if errors.Is(cause, storage.ErrDBStopped) {
				want = "database_stopped"
			}
			if got != nil || errorCode(t, err) != want || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "canceled") {
				t.Fatal(got, err)
			}
		}
		if errors.Is(cause, storage.ErrDBStopped) {
			if _, err := a.coverOperations.BeginRead(); !errors.Is(err, application.ErrCoverStopped) {
				t.Fatal(err)
			}
		}
	}
	for _, bad := range []manga.Tag{{ID: 0, Name: "タグ"}, {ID: 1, Name: " 未正規化"}} {
		a := NewApp()
		a.ctx = context.Background()
		a.tags = application.NewTagReader(apiTagRepository{values: []manga.Tag{bad}})
		if got, err := a.LoadTags(); got != nil || errorCode(t, err) != "storage_error" {
			t.Fatal(got, err)
		}
	}
	a := NewApp()
	if _, err := a.LoadTags(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
	a = testApp(t, t.TempDir())
	a.shutdown(context.Background())
	if _, err := a.LoadWorkTags(1); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
}

func TestTagAPILifetimeAndOperationErrors(t *testing.T) {
	a := NewApp()
	a.ctx = context.Background()
	entered := make(chan struct{})
	release := make(chan struct{})
	a.tags = application.NewTagReader(apiTagRepository{onRead: func(_ context.Context, id int64) {
		if id != 42 {
			t.Error(id)
		}
		close(entered)
		<-release
	}})
	done := make(chan error, 1)
	go func() { _, err := a.LoadWorkTags(42); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("read not started")
	}
	if a.mu.TryLock() {
		a.mu.Unlock()
		close(release)
		t.Fatal("DB lifetime not held")
	}
	shutdown := make(chan struct{})
	go func() { a.shutdown(context.Background()); close(shutdown) }()
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read did not finish")
	}
	select {
	case <-shutdown:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if _, err := a.LoadTags(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
	app := testApp(t, t.TempDir())
	w, err := app.CreateWork(CreateWorkInput{URL: "https://example.com/tag-busy", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	end, err := app.workOperations.BeginDelete(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{"タグ"}})); errorCode(t, err) != "work_busy" {
		t.Fatal(err)
	}
	end()
	for _, cause := range []error{manga.ErrTagName, errors.Join(storage.ErrWorkTagsChanged, storage.ErrDBStopped)} {
		app.editor = application.NewWorkEditor(appEditRepositoryFunc(func(context.Context, int64, manga.EditWorkInput, time.Time) (manga.Work, error) {
			return manga.Work{}, cause
		}), time.Now)
		_, err := app.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{"タグ"}}))
		want := "storage_error"
		if errors.Is(cause, storage.ErrDBStopped) {
			want = "database_stopped"
		}
		if errorCode(t, err) != want {
			t.Fatal(err)
		}
	}
}
