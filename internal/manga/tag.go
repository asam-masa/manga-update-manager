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

// Tag is a saved candidate's identity and exact name, including edit snapshots.
type Tag struct {
	ID   int64
	Name TagName
}

var ErrTagSelection = errors.New("選択したタグを確認してください")

// TagEdit replaces a work's associations. NewNames are only creation intents;
// they are resolved together with the work edit, never saved on normalization.
type TagEdit struct {
	Existing []Tag
	NewNames []string
}

func (input TagEdit) Normalize() (TagEdit, error) {
	result := TagEdit{Existing: make([]Tag, 0), NewNames: make([]string, 0)}
	ids := make(map[int64]TagName)
	for _, tag := range input.Existing {
		if tag.ID <= 0 || tag.Name.Validate() != nil {
			return TagEdit{}, ErrTagSelection
		}
		if name, ok := ids[tag.ID]; ok {
			if name != tag.Name {
				return TagEdit{}, ErrTagSelection
			}
			continue
		}
		ids[tag.ID] = tag.Name
		result.Existing = append(result.Existing, tag)
	}
	names := make(map[TagName]bool)
	for _, value := range input.NewNames {
		name, err := NormalizeTagName(value)
		if err != nil {
			return TagEdit{}, err
		}
		if !names[name] {
			names[name] = true
			result.NewNames = append(result.NewNames, string(name))
		}
	}
	return result, nil
}

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
