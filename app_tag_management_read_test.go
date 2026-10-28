package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type managementReadRepository struct {
	usage   []storage.TagUsage
	preview storage.TagDeletePreview
	cause   error
	onRead  func(context.Context, int64)
}

func (r managementReadRepository) ListTagUsage(ctx context.Context) ([]storage.TagUsage, error) {
	if r.onRead != nil {
		r.onRead(ctx, 0)
	}
	return r.usage, r.cause
}

func (r managementReadRepository) PreviewTagDelete(ctx context.Context, id int64) (storage.TagDeletePreview, error) {
	if r.onRead != nil {
		r.onRead(ctx, id)
	}
	return r.preview, r.cause
}

func managementReadApp(repository managementReadRepository) *App {
	a := NewApp()
	a.ctx = context.Background()
	a.tagManagement = application.NewTagManagementReader(repository)
	return a
}

func TestTagManagementReadAPIAndReopen(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	if got, err := a.LoadTagUsage(); err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/management-api", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{"共有", "未使用"}})); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{Existing: []TagDTO{{ID: "1", Name: "共有"}}})); err != nil {
		t.Fatal(err)
	}
	before, err := a.ListWorks()
	if err != nil {
		t.Fatal(err)
	}
	want := []TagUsageDTO{{Tag: TagDTO{ID: "1", Name: "共有"}, WorkCount: 1}, {Tag: TagDTO{ID: "2", Name: "未使用"}}}
	if got, err := a.LoadTagUsage(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	preview, err := a.LoadTagDeletePreview("1")
	if err != nil || !reflect.DeepEqual(preview.Works, []TagWorkDTO{{ID: "1", Title: w.Title, CreatedAt: w.CreatedAt}}) {
		t.Fatal(preview, err)
	}
	unused, err := a.LoadTagDeletePreview("2")
	wire, _ := json.Marshal(unused)
	if err != nil || string(wire) != `{"tag":{"id":"2","name":"未使用"},"works":[]}` {
		t.Fatal(string(wire), err)
	}
	if _, err := a.LoadTagDeletePreview("3"); errorCode(t, err) != "tag_not_found" {
		t.Fatal(err)
	}
	if got, err := a.ListWorks(); err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
	a.shutdown(context.Background())
	a = testApp(t, dir)
	if got, err := a.LoadTagUsage(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	if got, err := a.LoadTagDeletePreview("1"); err != nil || !reflect.DeepEqual(got, preview) {
		t.Fatal(got, err)
	}
}

func TestTagManagementReadAPIIdentityPrecisionAndInput(t *testing.T) {
	const exact = int64(9223372036854775807)
	at := time.Date(2026, 10, 28, 1, 2, 3, 123456789, time.UTC)
	preview := storage.TagDeletePreview{Tag: manga.Tag{ID: exact, Name: "タグ"}, Works: []storage.TagWork{{ID: exact, CreatedAt: at, Title: "作品"}}}
	called := 0
	requestContext := context.WithValue(context.Background(), struct{}{}, "management-read-context")
	a := managementReadApp(managementReadRepository{preview: preview, onRead: func(ctx context.Context, id int64) {
		called++
		if ctx != requestContext || id != exact {
			t.Error(ctx, id)
		}
	}})
	a.ctx = requestContext
	for _, id := range []string{"", "0", "-1", "+1", "01", " 1", "1e3", "9223372036854775808"} {
		if _, err := a.LoadTagDeletePreview(id); errorCode(t, err) != "invalid_input" || called != 0 {
			t.Fatal(id, called, err)
		}
	}
	got, err := a.LoadTagDeletePreview("9223372036854775807")
	if err != nil || called != 1 || got.Tag.ID != "9223372036854775807" || got.Works[0].ID != got.Tag.ID || got.Works[0].CreatedAt != "2026-10-28T10:02:03.123456789+09:00" {
		t.Fatal(got, err)
	}
	wire, _ := json.Marshal(got)
	var roundTrip TagDeletePreviewDTO
	if err := json.Unmarshal(wire, &roundTrip); err != nil || !reflect.DeepEqual(roundTrip, got) {
		t.Fatal(roundTrip, err)
	}
}

func TestTagManagementReadAPIDiscardsPartialAndUnsafeData(t *testing.T) {
	tag := manga.Tag{ID: 1, Name: "タグ"}
	valid := storage.TagUsage{Tag: tag, WorkCount: 1}
	for _, second := range []storage.TagUsage{
		{Tag: manga.Tag{ID: 0, Name: "不正"}},
		{Tag: manga.Tag{ID: 2, Name: " 未正規化"}},
		{Tag: manga.Tag{ID: 2, Name: "負数"}, WorkCount: -1},
		{Tag: manga.Tag{ID: 2, Name: "丸め"}, WorkCount: 1 << 53},
		{Tag: manga.Tag{ID: 1, Name: "重複ID"}},
		{Tag: manga.Tag{ID: 2, Name: "タグ"}},
	} {
		a := managementReadApp(managementReadRepository{usage: []storage.TagUsage{valid, second}})
		if got, err := a.LoadTagUsage(); got != nil || errorCode(t, err) != "storage_error" {
			t.Fatal(got, err)
		}
	}
	work := storage.TagWork{ID: 1, CreatedAt: time.Now().UTC(), Title: "作品"}
	for _, bad := range []storage.TagDeletePreview{{Tag: tag}, {Tag: manga.Tag{ID: 2, Name: "別タグ"}, Works: []storage.TagWork{}}} {
		a := managementReadApp(managementReadRepository{preview: bad})
		if got, err := a.LoadTagDeletePreview("1"); !reflect.DeepEqual(got, TagDeletePreviewDTO{}) || errorCode(t, err) != "storage_error" {
			t.Fatal(got, err)
		}
	}
	for _, bad := range []storage.TagWork{
		{ID: 0, CreatedAt: work.CreatedAt, Title: "作品"},
		{ID: 2, Title: "作品"},
		{ID: 2, CreatedAt: work.CreatedAt, Title: " "},
		{ID: 2, CreatedAt: time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC), Title: "範囲外"},
		work,
	} {
		a := managementReadApp(managementReadRepository{preview: storage.TagDeletePreview{Tag: tag, Works: []storage.TagWork{work, bad}}})
		if got, err := a.LoadTagDeletePreview("1"); !reflect.DeepEqual(got, TagDeletePreviewDTO{}) || errorCode(t, err) != "storage_error" {
			t.Fatal(got, err)
		}
	}
	for _, cause := range []error{errors.New("private diagnostic"), manga.ErrTagName, context.Canceled, storage.ErrTagNotFound, errors.Join(storage.ErrDBStopped, storage.ErrTagNotFound)} {
		a := managementReadApp(managementReadRepository{usage: []storage.TagUsage{valid}, preview: storage.TagDeletePreview{Tag: tag, Works: []storage.TagWork{work}}, cause: cause})
		got, err := a.LoadTagDeletePreview("1")
		want := "storage_error"
		if errors.Is(cause, storage.ErrDBStopped) {
			want = "database_stopped"
		} else if errors.Is(cause, storage.ErrTagNotFound) {
			want = "tag_not_found"
		}
		if !reflect.DeepEqual(got, TagDeletePreviewDTO{}) || errorCode(t, err) != want || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "canceled") {
			t.Fatal(err)
		}
		if got, err := a.LoadTagUsage(); got != nil || errorCode(t, err) != want {
			t.Fatal(got, err)
		}
		if errors.Is(cause, storage.ErrDBStopped) {
			if _, err := a.coverOperations.BeginRead(); !errors.Is(err, application.ErrCoverStopped) {
				t.Fatal(err)
			}
		}
	}
}

func TestTagManagementReadAPILifetime(t *testing.T) {
	for _, preview := range []bool{false, true} {
		entered, release := make(chan struct{}), make(chan struct{})
		a := managementReadApp(managementReadRepository{preview: storage.TagDeletePreview{Tag: manga.Tag{ID: 1, Name: "タグ"}, Works: []storage.TagWork{}}, onRead: func(context.Context, int64) {
			close(entered)
			<-release
		}})
		read := func() error {
			if preview {
				_, err := a.LoadTagDeletePreview("1")
				return err
			}
			_, err := a.LoadTagUsage()
			return err
		}
		done := make(chan error, 1)
		go func() { done <- read() }()
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("read did not start")
		}
		if a.mu.TryLock() {
			a.mu.Unlock()
			close(release)
			t.Fatal("DB lifetime released early")
		}
		closed := make(chan struct{})
		go func() { a.shutdown(context.Background()); close(closed) }()
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
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown did not finish")
		}
		if errorCode(t, read()) != "database_unavailable" {
			t.Fatal("read after shutdown")
		}
	}
	if _, err := NewApp().LoadTagUsage(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
}
