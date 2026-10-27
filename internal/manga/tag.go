package manga

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrTagName = errors.New("タグ名は前後の空白を除いて1〜50文字で、改行・制御文字を含めず入力してください")

// TagName is a saved name, not the normalized text used to search candidates.
type TagName string

func NormalizeTagName(value string) (TagName, error) {
	name := TagName(strings.TrimSpace(value))
	if err := name.Validate(); err != nil {
		return "", err
	}
	return name, nil
}

// Validate also rejects noncanonical saved values; it never repairs stored data.
func (name TagName) Validate() error {
	value := string(name)
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return ErrTagName
	}
	if n := utf8.RuneCountInString(value); n < 1 || n > 50 {
		return ErrTagName
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrTagName
		}
	}
	return nil
}
