// Package backup validates the backup container format, not database contents.
package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	MaxManifestBytes int64 = 8 << 20
	MaxDatabaseBytes int64 = 256 << 20
	MaxImageBytes    int64 = 20 << 20
	MaxTotalBytes    int64 = 2 << 30
	MaxImages              = 10000
)

var ErrManifest = errors.New("invalid backup manifest")

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Image struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type OmittedImage struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type Manifest struct {
	FormatVersion int            `json:"formatVersion"`
	CreatedAt     string         `json:"createdAt"`
	AppVersion    string         `json:"appVersion"`
	Migrations    []string       `json:"migrations"`
	Database      File           `json:"database"`
	Images        []Image        `json:"images"`
	OmittedImages []OmittedImage `json:"omittedImages"`
}

// DecodeManifest reads at most MaxManifestBytes+1 bytes and returns no partial
// result. Success does not authorize extraction, migration, or restoration.
func DecodeManifest(r io.Reader) (Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxManifestBytes+1))
	if err != nil {
		return Manifest{}, err
	}
	if len(data) == 0 || int64(len(data)) > MaxManifestBytes || !utf8.Valid(data) {
		return Manifest{}, ErrManifest
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	value, err := readValue(d, 0)
	if err != nil {
		return Manifest{}, ErrManifest
	}
	if _, err = d.Token(); err != io.EOF {
		return Manifest{}, ErrManifest
	}
	root, ok := value.(map[string]any)
	if !ok || !keys(root, "formatVersion", "createdAt", "appVersion", "migrations", "database", "images", "omittedImages") {
		return Manifest{}, ErrManifest
	}
	if number(root["formatVersion"]) != 1 {
		return Manifest{}, ErrManifest
	}
	created, ok := root["createdAt"].(string)
	if !ok || !utcTimestamp.MatchString(created) {
		return Manifest{}, ErrManifest
	}
	if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
		return Manifest{}, ErrManifest
	}
	if _, ok := root["appVersion"].(string); !ok {
		return Manifest{}, ErrManifest
	}
	migrations, ok := root["migrations"].([]any)
	if !ok || len(migrations) == 0 || len(migrations) > len(knownMigrations) {
		return Manifest{}, ErrManifest
	}
	for i, v := range migrations {
		if v != knownMigrations[i] {
			return Manifest{}, ErrManifest
		}
	}
	db, ok := root["database"].(map[string]any)
	if !ok || !keys(db, "path", "size", "sha256") || db["path"] != "database.sqlite" {
		return Manifest{}, ErrManifest
	}
	size := number(db["size"])
	if size <= 0 || size > MaxDatabaseBytes || !hex(db["sha256"], 64) {
		return Manifest{}, ErrManifest
	}
	total := int64(len(data)) + size
	images, ok := root["images"].([]any)
	if !ok {
		return Manifest{}, ErrManifest
	}
	omitted, ok := root["omittedImages"].([]any)
	if !ok || len(images) > MaxImages || len(omitted) > MaxImages-len(images) {
		return Manifest{}, ErrManifest
	}
	seen := map[string]bool{}
	for _, v := range images {
		image, ok := v.(map[string]any)
		if !ok || !keys(image, "id", "path", "size", "sha256") || !identity(image, seen) || !hex(image["sha256"], 64) {
			return Manifest{}, ErrManifest
		}
		n := number(image["size"])
		if n <= 0 || n > MaxImageBytes || n > MaxTotalBytes-total {
			return Manifest{}, ErrManifest
		}
		total += n
	}
	for _, v := range omitted {
		image, ok := v.(map[string]any)
		if !ok || !keys(image, "id", "path", "reason") || !identity(image, seen) {
			return Manifest{}, ErrManifest
		}
		if image["reason"] != "missing" && image["reason"] != "corrupt" {
			return Manifest{}, ErrManifest
		}
	}
	if total > MaxTotalBytes {
		return Manifest{}, ErrManifest
	}
	var result Manifest
	if err := json.Unmarshal(data, &result); err != nil {
		return Manifest{}, ErrManifest
	}
	return result, nil
}

var knownMigrations = []string{"001_create_works.sql", "002_last_access.sql", "003_display_settings.sql", "004_managed_covers.sql", "005_work_list_mode.sql", "006_work_sort.sql", "007_scheduled_date.sql", "008_schedule_settings.sql", "009_tags.sql"}

// time.Parse also accepts a comma fraction and a single-digit hour. The format
// contract requires the RFC 3339 spelling and an explicit UTC offset.
var utcTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|\+00:00)$`)

func identity(m map[string]any, seen map[string]bool) bool {
	if !hex(m["id"], 32) {
		return false
	}
	id := m["id"].(string)
	if seen[id] || m["path"] != "covers/"+id+".png" {
		return false
	}
	seen[id] = true
	return true
}
func keys(m map[string]any, names ...string) bool {
	if len(m) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := m[name]; !ok {
			return false
		}
	}
	return true
}
func hex(value any, length int) bool {
	s, ok := value.(string)
	if !ok || len(s) != length {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func number(value any) int64 {
	n, ok := value.(json.Number)
	if !ok {
		return -1
	}
	result, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return -1
	}
	return result
}

// Token parsing preserves duplicate keys (including escaped spellings) as an
// error before encoding/json can silently replace them. Depth is bounded.
func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 16 {
		return nil, ErrManifest
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	switch delim {
	case '{':
		result := map[string]any{}
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := token.(string)
			if !ok {
				return nil, ErrManifest
			}
			if _, exists := result[key]; exists {
				return nil, ErrManifest
			}
			if len(result) >= 7 {
				return nil, ErrManifest
			}
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, ErrManifest
		}
		return result, nil
	case '[':
		result := []any{}
		for d.More() {
			if len(result) >= MaxImages {
				return nil, ErrManifest
			}
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, ErrManifest
		}
		return result, nil
	default:
		return nil, ErrManifest
	}
}
