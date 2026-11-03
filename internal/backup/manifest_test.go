package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func fixture() Manifest {
	return Manifest{FormatVersion: 1, CreatedAt: "2026-11-03T00:00:00Z", AppVersion: "test", Migrations: append([]string{}, knownMigrations...), Database: File{"database.sqlite", 1, strings.Repeat("a", 64)}, Images: []Image{}, OmittedImages: []OmittedImage{}}
}
func encoded(t *testing.T, m Manifest) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func decode(t *testing.T, m Manifest) (Manifest, error) {
	t.Helper()
	return DecodeManifest(strings.NewReader(encoded(t, m)))
}
func image(i int, size int64) Image {
	id := fmt.Sprintf("%032x", i)
	return Image{id, "covers/" + id + ".png", size, strings.Repeat("b", 64)}
}
func reject(t *testing.T, data string) {
	t.Helper()
	got, err := DecodeManifest(strings.NewReader(data))
	if !errors.Is(err, ErrManifest) || !reflect.DeepEqual(got, Manifest{}) {
		t.Fatalf("got partial value or wrong error: %+v %v", got, err)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	m := fixture()
	m.Images = []Image{image(1, 20)}
	m.OmittedImages = []OmittedImage{{image(2, 1).ID, image(2, 1).Path, "missing"}, {image(3, 1).ID, image(3, 1).Path, "corrupt"}}
	got, err := decode(t, m)
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Fatal(got, err)
	}
	m = fixture()
	if got, err := decode(t, m); err != nil || got.Images == nil || got.OmittedImages == nil {
		t.Fatal(got, err)
	}
	for _, date := range []string{"2028-02-29T12:00:00Z", "2026-11-03T00:00:00.123456789Z", "2026-11-03T00:00:00+00:00"} {
		m.CreatedAt = date
		if _, err := decode(t, m); err != nil {
			t.Fatalf("valid UTC timestamp %s: %v", date, err)
		}
	}
	for n := 1; n <= len(knownMigrations); n++ {
		m.Migrations = append([]string{}, knownMigrations[:n]...)
		if _, err := decode(t, m); err != nil {
			t.Fatalf("known migration prefix %d: %v", n, err)
		}
	}
}

func TestManifestJSONRefusals(t *testing.T) {
	valid := encoded(t, fixture())
	for name, data := range map[string]string{
		"unknown":           strings.Replace(valid, "{", "{\"unknown\":0,", 1),
		"duplicate":         strings.Replace(valid, "{", "{\"formatVersion\":1,", 1),
		"escaped duplicate": strings.Replace(valid, "{", "{\"format\\u0056ersion\":1,", 1),
		"nested duplicate":  strings.Replace(valid, "\"path\":", "\"path\":\"database.sqlite\",\"path\":", 1),
		"missing":           strings.Replace(valid, "\"images\":[],", "", 1),
		"null":              strings.Replace(valid, "\"images\":[]", "\"images\":null", 1),
		"fraction":          strings.Replace(valid, "\"size\":1", "\"size\":1.0", 1),
		"exponent":          strings.Replace(valid, "\"size\":1", "\"size\":1e0", 1),
		"huge":              strings.Replace(valid, "\"size\":1", "\"size\":9223372036854775808", 1),
		"trailing":          valid + " {}", "truncated": valid[:len(valid)-1], "root array": "[]", "root null": "null",
		"invalid UTF8": strings.Replace(valid, "test", string([]byte{255}), 1),
		"deep":         strings.Repeat("[", 18) + strings.Repeat("]", 18),
	} {
		t.Run(name, func(t *testing.T) { reject(t, data) })
	}
}

func TestManifestFieldRefusals(t *testing.T) {
	for name, change := range map[string]func(*Manifest){
		"version":           func(m *Manifest) { m.FormatVersion = 2 },
		"date":              func(m *Manifest) { m.CreatedAt = "2026-02-30T00:00:00Z" },
		"offset":            func(m *Manifest) { m.CreatedAt = "2026-11-03T00:00:00+09:00" },
		"short hour":        func(m *Manifest) { m.CreatedAt = "2026-11-03T0:00:00Z" },
		"comma fraction":    func(m *Manifest) { m.CreatedAt = "2026-11-03T00:00:00,1Z" },
		"unknown offset":    func(m *Manifest) { m.CreatedAt = "2026-11-03T00:00:00-00:00" },
		"DB path":           func(m *Manifest) { m.Database.Path = "../database.sqlite" },
		"hash":              func(m *Manifest) { m.Database.SHA256 = strings.Repeat("A", 64) },
		"zero":              func(m *Manifest) { m.Database.Size = 0 },
		"unknown migration": func(m *Manifest) { m.Migrations[8] = "010_unknown.sql" },
		"migration gap":     func(m *Manifest) { m.Migrations = m.Migrations[1:] },
		"no migration":      func(m *Manifest) { m.Migrations = []string{} },
		"duplicate image":   func(m *Manifest) { m.Images = []Image{image(1, 1), image(1, 1)} },
		"included and omitted": func(m *Manifest) {
			m.Images = []Image{image(1, 1)}
			m.OmittedImages = []OmittedImage{{image(1, 1).ID, image(1, 1).Path, "missing"}}
		},
		"path mismatch": func(m *Manifest) { m.Images = []Image{image(1, 1)}; m.Images[0].Path = "covers/other.png" },
		"reason":        func(m *Manifest) { m.OmittedImages = []OmittedImage{{image(1, 1).ID, image(1, 1).Path, "permission"}} },
	} {
		t.Run(name, func(t *testing.T) { m := fixture(); change(&m); reject(t, encoded(t, m)) })
	}
}

func TestManifestLimits(t *testing.T) {
	m := fixture()
	m.Database.Size = MaxDatabaseBytes
	if _, err := decode(t, m); err != nil {
		t.Fatal(err)
	}
	m.Database.Size++
	reject(t, encoded(t, m))
	m = fixture()
	m.Images = []Image{image(1, MaxImageBytes)}
	if _, err := decode(t, m); err != nil {
		t.Fatal(err)
	}
	m.Images[0].Size++
	reject(t, encoded(t, m))
	m = fixture()
	for i := 0; i < MaxImages; i++ {
		m.OmittedImages = append(m.OmittedImages, OmittedImage{image(i, 1).ID, image(i, 1).Path, "missing"})
	}
	if _, err := decode(t, m); err != nil {
		t.Fatal(err)
	}
	m.Images = []Image{image(MaxImages, 1)}
	reject(t, encoded(t, m))
	m = fixture()
	data := encoded(t, m)
	padding := strings.Repeat(" ", int(MaxManifestBytes)-len(data))
	if _, err := DecodeManifest(strings.NewReader(data + padding)); err != nil {
		t.Fatal(err)
	}
	reject(t, data+padding+" ")
	m = fixture()
	m.Database.Size = MaxDatabaseBytes
	for i := 0; i < 89; i++ {
		m.Images = append(m.Images, image(i, MaxImageBytes))
	}
	m.Images = append(m.Images, image(89, 1_000_000))
	for range 4 {
		m.Images[89].Size += MaxTotalBytes - int64(len(encoded(t, m))) - m.Database.Size - 89*MaxImageBytes - m.Images[89].Size
	}
	if _, err := decode(t, m); err != nil {
		t.Fatal(err)
	}
	m.Images[89].Size++
	reject(t, encoded(t, m))
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestManifestReadFailure(t *testing.T) {
	got, err := DecodeManifest(brokenReader{})
	if !errors.Is(err, io.ErrUnexpectedEOF) || !reflect.DeepEqual(got, Manifest{}) {
		t.Fatal(got, err)
	}
}
