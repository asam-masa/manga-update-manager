package manga

import (
	"errors"
	"testing"
)

func TestEditWorkInputNormalize(t *testing.T) {
	want := EditWorkInput{"https://example.com/new", "漫画", "サイト", "一行\n二行"}
	got, err := (EditWorkInput{" \nhttps://example.com/new\t", " 漫画 ", " サイト ", " 一行\n二行\n"}).Normalize()
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	for _, tc := range []struct {
		input EditWorkInput
		want  error
	}{
		{EditWorkInput{Title: "x"}, ErrURLRequired},
		{EditWorkInput{URL: "file:///x", Title: "x"}, ErrURLScheme},
		{EditWorkInput{URL: "https:///x", Title: "x"}, ErrURLScheme},
		{EditWorkInput{URL: "https://example.com", Title: " \n"}, ErrTitleRequired},
	} {
		if _, err := tc.input.Normalize(); !errors.Is(err, tc.want) {
			t.Fatal(tc, err)
		}
	}
}
