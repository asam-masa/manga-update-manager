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

type libraryRepository struct {
	value  storage.LibrarySnapshot
	cause  error
	onRead func(context.Context)
}

func (r libraryRepository) LoadLibrarySnapshot(ctx context.Context) (storage.LibrarySnapshot, error) {
	if r.onRead != nil {
		r.onRead(ctx)
	}
	return r.value, r.cause
}

func libraryApp(r libraryRepository) *App {
	a := NewApp()
	a.ctx = context.Background()
	a.library = application.NewLibraryReader(r)
	return a
}

func libraryFixture() storage.LibrarySnapshot {
	at := time.Date(2026, 11, 1, 1, 2, 3, 123456789, time.UTC)
	w, _ := manga.NewWork(manga.NewWorkInput{URL: "https://example.com/library", Title: "作品", SiteName: "合成", Notes: "メモ"}, at)
	w.ID = 9223372036854775807
	w.LastAccessedAt = &at
	w.ScheduledDate = "2028-02-29"
	return storage.LibrarySnapshot{Works: []manga.Work{w}, Tags: []manga.Tag{{ID: w.ID, Name: "タグ"}}, Links: []storage.WorkTagLink{{WorkID: w.ID, TagID: w.ID}}}
}

func TestLibraryAPIIntegrationAndReopen(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	got, err := a.LoadLibrarySnapshot()
	wire, _ := json.Marshal(got)
	if err != nil || string(wire) != `{"works":[],"tags":[],"links":[]}` {
		t.Fatal(string(wire), err)
	}
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/library", Title: "作品"})
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
	got, err = a.LoadLibrarySnapshot()
	if err != nil || len(got.Works) != 1 || got.Works[0].ID != "1" || !reflect.DeepEqual(got.Tags, []TagDTO{{ID: "1", Name: "共有"}, {ID: "2", Name: "未使用"}}) || !reflect.DeepEqual(got.Links, []WorkTagLinkDTO{{WorkID: "1", TagID: "1"}}) {
		t.Fatal(got, err)
	}
	if after, err := a.ListWorks(); err != nil || !reflect.DeepEqual(after, before) {
		t.Fatal(after, err)
	}
	a.shutdown(context.Background())
	a = testApp(t, dir)
	if after, err := a.LoadLibrarySnapshot(); err != nil || !reflect.DeepEqual(after, got) {
		t.Fatal(after, err)
	}
}

func TestLibraryAPIContextLifetimePrecisionAndJSON(t *testing.T) {
	value := libraryFixture()
	a := libraryApp(libraryRepository{value: value})
	ctx := context.WithValue(context.Background(), struct{}{}, "library-context")
	a.ctx = ctx
	called := 0
	a.library = application.NewLibraryReader(libraryRepository{value: value, onRead: func(got context.Context) {
		called++
		if got != ctx {
			t.Error("lost context")
		}
		if a.mu.TryLock() {
			a.mu.Unlock()
			t.Error("DB lifetime lock absent")
		}
	}})
	got, err := a.LoadLibrarySnapshot()
	if err != nil || called != 1 || got.Works[0].ID != "9223372036854775807" || got.Tags[0].ID != got.Works[0].ID || got.Links[0].WorkID != got.Works[0].ID || got.Links[0].TagID != got.Tags[0].ID || got.Works[0].CreatedAt != "2026-11-01T10:02:03.123456789+09:00" {
		t.Fatal(got, err)
	}
	wire, err := json.Marshal(got)
	var restored LibrarySnapshotDTO
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wire, &restored); err != nil || !reflect.DeepEqual(restored, got) {
		t.Fatal(restored, err)
	}
	// Historical Tokyo daylight-saving time must not be hard-coded as +09:00.
	value.Works[0].CreatedAt = time.Date(1949, 7, 1, 0, 0, 0, 123456789, time.UTC)
	a = libraryApp(libraryRepository{value: value})
	if got, err := a.LoadLibrarySnapshot(); err != nil || got.Works[0].CreatedAt != "1949-07-01T10:00:00.123456789+10:00" {
		t.Fatal(got, err)
	}
}

func TestLibraryAPIRejectsInvalidWholeSnapshot(t *testing.T) {
	mutations := []func(*storage.LibrarySnapshot){
		func(v *storage.LibrarySnapshot) { v.Works = nil }, func(v *storage.LibrarySnapshot) { v.Tags = nil }, func(v *storage.LibrarySnapshot) { v.Links = nil },
		func(v *storage.LibrarySnapshot) { v.Works[0].ID = 0 }, func(v *storage.LibrarySnapshot) { v.Works = append(v.Works, v.Works[0]) },
		func(v *storage.LibrarySnapshot) { v.Works[0].Title = "" }, func(v *storage.LibrarySnapshot) { v.Works[0].Notes = string([]byte{0xff}) },
		func(v *storage.LibrarySnapshot) { v.Works[0].URL = "file:///private" }, func(v *storage.LibrarySnapshot) { v.Works[0].ThumbnailPath = "../private" },
		func(v *storage.LibrarySnapshot) { v.Works[0].CreatedAt = time.Time{} },
		func(v *storage.LibrarySnapshot) {
			v.Works[0].UpdatedAt = time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC)
		},
		func(v *storage.LibrarySnapshot) { bad := time.Time{}; v.Works[0].LastAccessedAt = &bad },
		func(v *storage.LibrarySnapshot) { v.Works[0].ScheduledDate = "2028-02-30" },
		func(v *storage.LibrarySnapshot) { v.Tags[0].ID = 0 }, func(v *storage.LibrarySnapshot) { v.Tags[0].Name = " 不正 " },
		func(v *storage.LibrarySnapshot) { v.Tags = append(v.Tags, v.Tags[0]) },
		func(v *storage.LibrarySnapshot) {
			v.Tags = append([]manga.Tag{{ID: 1, Name: v.Tags[0].Name}}, v.Tags...)
		},
		func(v *storage.LibrarySnapshot) { v.Links[0].WorkID = 1 }, func(v *storage.LibrarySnapshot) { v.Links[0].TagID = 1 },
		func(v *storage.LibrarySnapshot) { v.Links = append(v.Links, v.Links[0]) },
	}
	for i, mutate := range mutations {
		v := libraryFixture()
		mutate(&v)
		a := libraryApp(libraryRepository{value: v})
		got, err := a.LoadLibrarySnapshot()
		if !reflect.DeepEqual(got, LibrarySnapshotDTO{}) || errorCode(t, err) != "storage_error" {
			t.Fatal(i, got, err)
		}
	}
}

func TestLibraryAPIErrorsStopAndUnavailable(t *testing.T) {
	for _, cause := range []error{errors.New("H:/private/db.sqlite secret"), context.Canceled, errors.Join(storage.ErrDBStopped, errors.New("private"))} {
		a := libraryApp(libraryRepository{value: libraryFixture(), cause: cause})
		got, err := a.LoadLibrarySnapshot()
		code := errorCode(t, err)
		want := "storage_error"
		if errors.Is(cause, storage.ErrDBStopped) {
			want = "database_stopped"
		}
		if !reflect.DeepEqual(got, LibrarySnapshotDTO{}) || code != want || strings.Contains(err.Error(), "private") {
			t.Fatal(got, err)
		}
		op, admissionErr := a.coverOperations.BeginRead()
		if op != nil {
			_ = op.End()
		}
		if want == "database_stopped" && admissionErr == nil {
			t.Fatal("stop not observed")
		}
	}
	a := NewApp()
	if _, err := a.LoadLibrarySnapshot(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
	a = testApp(t, t.TempDir())
	a.shutdown(context.Background())
	if _, err := a.LoadLibrarySnapshot(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
}

func TestLibraryAPIReadAndShutdown(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	a := libraryApp(libraryRepository{value: libraryFixture(), onRead: func(context.Context) {
		close(entered)
		<-release
	}})
	done := make(chan error, 1)
	go func() { _, err := a.LoadLibrarySnapshot(); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not start")
	}
	if a.mu.TryLock() {
		a.mu.Unlock()
		close(release)
		t.Fatal("lifetime lock absent")
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
		t.Fatal("read did not complete")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	if _, err := a.LoadLibrarySnapshot(); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
}
