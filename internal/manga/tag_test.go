package manga

import (
	"errors"
	"strings"
	"testing"
)

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
