package main

import (
	"context"
	"testing"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/manga"
)

func TestExistingEditAPIPreservesInternalTags(t *testing.T) {
	a := testApp(t, t.TempDir())
	w, err := a.CreateWork(CreateWorkInput{URL: "https://example.com/tag-compatibility", Title: "作品"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = a.store.EditWork(ctx, w.ID, manga.EditWorkInput{URL: w.URL, Title: w.Title, Tags: &manga.TagEdit{NewNames: []string{"既存API保持"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.WorkTags(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.EditWork(w.ID, EditWorkInput{URL: w.URL, Title: "公開APIから編集"})
	if err != nil || result.Work.Title != "公開APIから編集" {
		t.Fatal(result, err)
	}
	after, err := a.store.WorkTags(ctx, w.ID)
	if err != nil || len(after) != 1 || len(before) != 1 || after[0] != before[0] {
		t.Fatal(before, after, err)
	}
}
