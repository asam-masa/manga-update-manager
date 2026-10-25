package manga

import (
	"errors"
	"testing"
)

func TestEditWorkInputNormalize(t *testing.T) {
	want := EditWorkInput{URL: "https://example.com/new", Title: "漫画", SiteName: "サイト", Notes: "一行\n二行"}
	got, err := (EditWorkInput{URL: " \nhttps://example.com/new\t", Title: " 漫画 ", SiteName: " サイト ", Notes: " 一行\n二行\n"}).Normalize()
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
