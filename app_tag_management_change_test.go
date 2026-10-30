package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

type managementChangeRepository struct {
	rename func(context.Context, manga.Tag, string) (manga.Tag, error)
	remove func(context.Context, storage.TagDeletePreview) (manga.Tag, error)
}

func (r managementChangeRepository) RenameTag(ctx context.Context, tag manga.Tag, name string) (manga.Tag, error) {
	return r.rename(ctx, tag, name)
}
func (r managementChangeRepository) DeleteTag(ctx context.Context, preview storage.TagDeletePreview) (manga.Tag, error) {
	return r.remove(ctx, preview)
}

func TestTagManagementChangesPersistAndPreserveWorks(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, dir)
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/tag-change", Title: "作品", Notes: "保持"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{NewNames: []string{"共有", "別タグ"}})); err != nil {
		t.Fatal(err)
	}
	before, err := a.ListWorks()
	if err != nil {
		t.Fatal(err)
	}
	old := TagDTO{ID: "1", Name: "共有"}
	rename, err := a.RenameTag(old, " 新しい名前 ")
	if err != nil || rename.Tag != (TagDTO{ID: "1", Name: "新しい名前"}) || rename.Warnings == nil || len(rename.Warnings) != 0 {
		t.Fatal(rename, err)
	}
	if _, err := a.RenameTag(old, "古い確認"); errorCode(t, err) != "tag_changed" {
		t.Fatal(err)
	}
	if _, err := a.RenameTag(rename.Tag, "別タグ"); errorCode(t, err) != "tag_duplicate_name" {
		t.Fatal(err)
	}
	preview, err := a.LoadTagDeletePreview("1")
	if err != nil {
		t.Fatal(err)
	}
	// A different title/order is display-only, but identities must be exact.
	preview.Works[0].Title = "確認後の表示名"
	deleted, err := a.DeleteTag(preview)
	if err != nil || deleted.Tag != rename.Tag || deleted.Warnings == nil {
		t.Fatal(deleted, err)
	}
	if _, err := a.DeleteTag(preview); errorCode(t, err) != "tag_not_found" {
		t.Fatal(err)
	}
	if got, err := a.ListWorks(); err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
	if tags, err := a.LoadWorkTags(w.ID); err != nil || !reflect.DeepEqual(tags, []TagDTO{{ID: "2", Name: "別タグ"}}) {
		t.Fatal(tags, err)
	}
	a.shutdown(context.Background())
	a = testApp(t, dir)
	if tags, err := a.LoadTags(); err != nil || !reflect.DeepEqual(tags, []TagDTO{{ID: "2", Name: "別タグ"}}) {
		t.Fatal(tags, err)
	}
	if got, err := a.ListWorks(); err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
	// Explicit empty array confirms the now-unused remaining tag.
	if _, err := a.EditWork(w.ID, publicTagEdit(w, &TagEditInput{})); err != nil {
		t.Fatal(err)
	}
	unused, err := a.LoadTagDeletePreview("2")
	if err != nil || unused.Works == nil || len(unused.Works) != 0 {
		t.Fatal(unused, err)
	}
	if _, err := a.DeleteTag(unused); err != nil {
		t.Fatal(err)
	}
}

func TestTagChangeInputPrecisionAndNoRepositoryCallOnInvalid(t *testing.T) {
	const id = "9223372036854775807"
	tag := TagDTO{ID: id, Name: "タグ"}
	at := "2026-10-28T10:02:03.123456789+09:00"
	ctx := context.WithValue(context.Background(), struct{}{}, "tag-change")
	calls := 0
	a := NewApp()
	a.ctx = ctx
	a.tagManager = application.NewTagManager(managementChangeRepository{
		rename: func(got context.Context, expected manga.Tag, value string) (manga.Tag, error) {
			calls++
			if got != ctx || expected.ID != 9223372036854775807 || expected.Name != "タグ" || value != "新規" {
				t.Error(got, expected, value)
			}
			return manga.Tag{ID: expected.ID, Name: manga.TagName(value)}, nil
		},
		remove: func(got context.Context, expected storage.TagDeletePreview) (manga.Tag, error) {
			calls++
			if got != ctx || expected.Works[0].ID != 9223372036854775807 || expected.Works[0].CreatedAt.Nanosecond() != 123456789 {
				t.Error(expected)
			}
			return expected.Tag, nil
		},
	})
	for _, bad := range []TagDTO{{ID: "01", Name: "タグ"}, {ID: "0", Name: "タグ"}, {ID: "9223372036854775808", Name: "タグ"}, {ID: id, Name: " タグ"}} {
		if _, err := a.RenameTag(bad, "新規"); errorCode(t, err) != "invalid_input" {
			t.Fatal(bad, err)
		}
		if _, err := a.DeleteTag(TagDeletePreviewDTO{Tag: bad, Works: []TagWorkDTO{}}); errorCode(t, err) != "invalid_input" {
			t.Fatal(bad, err)
		}
	}
	if _, err := a.RenameTag(tag, "\n"); errorCode(t, err) != "invalid_input" {
		t.Fatal(err)
	}
	for _, works := range [][]TagWorkDTO{nil, {{ID: id, CreatedAt: "2026-10-28"}}, {{ID: "01", CreatedAt: at}}, {{ID: id, CreatedAt: "0001-01-01T00:00:00Z"}}, {{ID: id, CreatedAt: at}, {ID: id, CreatedAt: at}}} {
		if _, err := a.DeleteTag(TagDeletePreviewDTO{Tag: tag, Works: works}); errorCode(t, err) != "invalid_input" {
			t.Fatal(works, err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached repository", calls)
	}
	if got, err := a.RenameTag(tag, " 新規 "); err != nil || got.Tag.ID != id {
		t.Fatal(got, err)
	}
	if got, err := a.DeleteTag(TagDeletePreviewDTO{Tag: tag, Works: []TagWorkDTO{{ID: id, CreatedAt: at}}}); err != nil || got.Tag != tag {
		t.Fatal(got, err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestTagDeleteAPIReconfirmsMembershipAndNanoseconds(t *testing.T) {
	a := testApp(t, t.TempDir())
	one, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/one-tag", Title: "一つ目"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/two-tag", Title: "二つ目"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(one.ID, publicTagEdit(one, &TagEditInput{NewNames: []string{"共有"}})); err != nil {
		t.Fatal(err)
	}
	preview, err := a.LoadTagDeletePreview("1")
	if err != nil {
		t.Fatal(err)
	}
	// A one-nanosecond difference must never be rounded into a matching identity.
	at, _ := time.Parse(time.RFC3339Nano, preview.Works[0].CreatedAt)
	changed := preview
	changed.Works = append([]TagWorkDTO{}, preview.Works...)
	changed.Works[0].CreatedAt = at.Add(time.Nanosecond).Format(time.RFC3339Nano)
	if _, err := a.DeleteTag(changed); errorCode(t, err) != "tag_changed" {
		t.Fatal(err)
	}
	if _, err := a.EditWork(one.ID, publicTagEdit(one, &TagEditInput{})); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditWork(two.ID, publicTagEdit(two, &TagEditInput{Existing: []TagDTO{preview.Tag}})); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteTag(preview); errorCode(t, err) != "tag_changed" {
		t.Fatal("same count with different targets", err)
	}
	// The same membership change does not invalidate a rename.
	if _, err := a.RenameTag(preview.Tag, "変更"); err != nil {
		t.Fatal(err)
	}
	if got, err := a.LoadTags(); err != nil || !reflect.DeepEqual(got, []TagDTO{{ID: "1", Name: "変更"}}) {
		t.Fatal(got, err)
	}
}

func TestTagChangeOutcomeMapping(t *testing.T) {
	tag := manga.Tag{ID: 1, Name: "タグ"}
	for _, tc := range []struct {
		cause   error
		code    string
		warning bool
	}{
		{errors.New("private path diagnostic"), "storage_error", false},
		{manga.ErrTagName, "storage_error", false},
		{context.Canceled, "storage_error", false},
		{storage.ErrTagChanged, "tag_changed", false},
		{storage.ErrTagDuplicateName, "tag_duplicate_name", false},
		{storage.ErrTagNotFound, "tag_not_found", false},
		{errors.Join(storage.ErrTagChanged, storage.ErrDBStopped), "database_stopped", false},
		{errors.Join(storage.ErrTagChangeOutcomeUnknown, storage.ErrDBStopped), "tag_change_outcome_unknown", false},
		{errors.Join(storage.ErrTagChangeCommitted, storage.ErrDBStopped), "", true},
	} {
		for _, remove := range []bool{false, true} {
			a := NewApp()
			a.ctx = context.Background()
			a.tagManager = application.NewTagManager(managementChangeRepository{
				rename: func(context.Context, manga.Tag, string) (manga.Tag, error) { return tag, tc.cause },
				remove: func(context.Context, storage.TagDeletePreview) (manga.Tag, error) { return tag, tc.cause },
			})
			var got TagChangeResult
			var err error
			if remove {
				got, err = a.DeleteTag(TagDeletePreviewDTO{Tag: TagDTO{ID: "1", Name: "タグ"}, Works: []TagWorkDTO{}})
			} else {
				got, err = a.RenameTag(TagDTO{ID: "1", Name: "タグ"}, "タグ")
			}
			if tc.warning {
				if err != nil || got.Tag.ID != "1" || len(got.Warnings) != 1 || got.Warnings[0].Code != "database_stopped_after_commit" {
					t.Fatal(got, err)
				}
			} else if errorCode(t, err) != tc.code || !reflect.DeepEqual(got, TagChangeResult{}) || strings.Contains(err.Error(), "private") {
				t.Fatal(got, err)
			}
			if errors.Is(tc.cause, storage.ErrDBStopped) {
				if _, err := a.coverOperations.BeginRead(); !errors.Is(err, application.ErrCoverStopped) {
					t.Fatal(err)
				}
			}
			end, err := a.workOperations.BeginTagChange(1)
			if err != nil {
				t.Fatal("reservation leaked", err)
			}
			end()
		}
	}
}

func TestTagChangeAdmissionLifetimeAndClose(t *testing.T) {
	for _, remove := range []bool{false, true} {
		a := testApp(t, t.TempDir())
		if _, err := a.LoadCoverWarnings(); err != nil {
			t.Fatal(err)
		}
		w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/concurrent", Title: "作品"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.ctx = ctx
		entered, release := make(chan struct{}), make(chan struct{})
		wait := func() {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		a.tagManager = application.NewTagManager(managementChangeRepository{
			rename: func(context.Context, manga.Tag, string) (manga.Tag, error) {
				wait()
				return manga.Tag{ID: 1, Name: "タグ"}, nil
			},
			remove: func(context.Context, storage.TagDeletePreview) (manga.Tag, error) {
				wait()
				return manga.Tag{ID: 1, Name: "タグ"}, nil
			},
		})
		done := make(chan error, 1)
		go func() {
			var err error
			if remove {
				_, err = a.DeleteTag(TagDeletePreviewDTO{Tag: TagDTO{ID: "1", Name: "タグ"}, Works: []TagWorkDTO{}})
			} else {
				_, err = a.RenameTag(TagDTO{ID: "1", Name: "タグ"}, "タグ")
			}
			done <- err
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if a.mu.TryLock() {
			a.mu.Unlock()
			t.Fatal("DB lifetime released")
		}
		if _, err := a.RenameTag(TagDTO{ID: "1", Name: "タグ"}, "別名"); errorCode(t, err) != "tag_busy" {
			t.Fatal(err)
		}
		if _, err := a.DeleteTag(TagDeletePreviewDTO{Tag: TagDTO{ID: "1", Name: "タグ"}, Works: []TagWorkDTO{}}); errorCode(t, err) != "tag_busy" {
			t.Fatal(err)
		}
		// A real work edit with the same numeric ID must still be admitted.
		if _, err := a.EditWork(w.ID, EditWorkInput{URL: w.URL, Title: "並行編集"}); err != nil {
			t.Fatal(err)
		}
		notices := 0
		a.tagBusyNotice = func(context.Context) { notices++ }
		if !a.beforeClose(ctx) || notices != 1 {
			t.Fatal("normal close not refused", notices)
		}
		other, err := a.workOperations.BeginTagChange(2)
		if err != nil {
			t.Fatal("refusal closed admission", err)
		}
		other()
		close(release)
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if a.beforeClose(ctx) {
			t.Fatal("finished operation prevents close")
		}
		if _, err := a.RenameTag(TagDTO{ID: "1", Name: "タグ"}, "別名"); errorCode(t, err) != "database_unavailable" {
			t.Fatal(err)
		}
		a.shutdown(ctx)
		if _, err := a.DeleteTag(TagDeletePreviewDTO{}); errorCode(t, err) != "database_unavailable" {
			t.Fatal(err)
		}
	}
	if _, err := NewApp().RenameTag(TagDTO{}, "名前"); errorCode(t, err) != "database_unavailable" {
		t.Fatal(err)
	}
}
