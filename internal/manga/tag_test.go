package manga

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTagEditNormalizationCopiesAndDeduplicates(t *testing.T) {
	input := TagEdit{Existing: []Tag{{ID: 1, Name: "既存"}, {ID: 1, Name: "既存"}}, NewNames: []string{" 新規 ", "新規", "ABC", "abc"}}
	got, err := input.Normalize()
	if err != nil || !reflect.DeepEqual(got.Existing, []Tag{{ID: 1, Name: "既存"}}) || !reflect.DeepEqual(got.NewNames, []string{"新規", "ABC", "abc"}) {
		t.Fatal(got, err)
	}
	got.Existing[0].Name = "変更"
	got.NewNames[0] = "変更"
	if input.Existing[0].Name != "既存" || input.NewNames[0] != " 新規 " {
		t.Fatal("mutated caller input")
	}
	for _, bad := range []TagEdit{{Existing: []Tag{{ID: 0, Name: "既存"}}}, {Existing: []Tag{{ID: 1, Name: " 未正規化"}}}, {Existing: []Tag{{ID: 1, Name: "A"}, {ID: 1, Name: "B"}}}, {NewNames: []string{"正常", " "}}} {
		if _, err := bad.Normalize(); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestTagNameNormalizationAndIdentity(t *testing.T) {
	for _, value := range []string{"異世界", "ABC", "abc", "ＡＢＣ", "ﾏﾝｶﾞ", "マンガ", "タグ 名", strings.Repeat("漫", 50), strings.Repeat("😀", 50), strings.Repeat("e\u0301", 25)} {
		got, err := NormalizeTagName(" \u3000" + value + "\u3000 ")
		if err != nil || string(got) != value || got.Validate() != nil {
			t.Fatalf("%q: got %q, error %v", value, got, err)
		}
	}
	if TagName(" 未正規化 ").Validate() == nil {
		t.Fatal("stored name was silently repaired")
	}
	if got, err := NormalizeTagName("\n\tタグ\u0085\u2028\u2029"); err != nil || got != "タグ" {
		t.Fatalf("surrounding whitespace: %q, %v", got, err)
	}
}

func TestTagNameRejectsInvalidInput(t *testing.T) {
	for _, value := range []string{"", " \u3000", strings.Repeat("漫", 51), strings.Repeat("😀", 51), strings.Repeat("e\u0301", 26), "a\x00b", "a\nb", "a\rb", "a\tb", "a\x7fb", "a\u0085b", "a\u2028b", "a\u2029b", string([]byte{0xff})} {
		if got, err := NormalizeTagName(value); !errors.Is(err, ErrTagName) || got != "" {
			t.Fatalf("%q: got %q, error %v", value, got, err)
		}
	}
}
