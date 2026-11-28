package restore

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

const examplePlan = `{
 "version":1,
 "restoreId":"11111111111111111111111111111111",
 "workspaceId":"22222222222222222222222222222222",
 "oldDatabase":{"volumeSerial":"0011223344556677","fileId":"33333333333333333333333333333333"},
 "oldCovers":null,
 "newDatabase":{"stagedId":"44444444444444444444444444444444","identity":{"volumeSerial":"0011223344556677","fileId":"55555555555555555555555555555555"}},
 "newImages":[]
}`

func observed(data string) Observation { return Observation{State: Present, Bytes: []byte(data)} }

func completionFor(plan string) string {
	return fmt.Sprintf(`{"version":1,"restoreId":"11111111111111111111111111111111","planSha256":"%x"}`, sha256.Sum256([]byte(plan)))
}

func TestClassifyRecoveryRecords(t *testing.T) {
	missing := Observation{State: Absent}
	readErr := errors.New("synthetic read failure")
	cases := []struct {
		name             string
		plan, completion Observation
		unpublished      ObservationState
		want             Decision
		wantErr          error
	}{
		{"both absent", missing, missing, Absent, NoRecords, nil},
		{"plan only", observed(examplePlan), missing, Absent, IncompleteCandidate, nil},
		{"matched pair", observed(examplePlan), observed(completionFor(examplePlan)), Absent, CompleteCandidate, nil},
		{"completion alone", missing, observed(completionFor(examplePlan)), Absent, Stop, ErrRecordPairMismatch},
		{"different digest by one space", observed(examplePlan + " "), observed(completionFor(examplePlan)), Absent, Stop, ErrRecordPairMismatch},
		{"different restore ID", observed(examplePlan), observed(strings.Replace(completionFor(examplePlan), strings.Repeat("1", 32), strings.Repeat("2", 32), 1)), Absent, Stop, ErrRecordPairMismatch},
		{"unknown completion", observed(examplePlan), Observation{Err: readErr}, Absent, Stop, readErr},
		{"unknown observation", Observation{}, missing, Absent, Stop, ErrRecordsUncertain},
		{"unknown unpublished", missing, missing, Unknown, Stop, ErrRecordsUncertain},
		{"unpublished present", observed(examplePlan), observed(completionFor(examplePlan)), Present, Stop, ErrRecordsUncertain},
		{"empty bytes", observed(""), missing, Absent, Stop, ErrInvalidRecord},
		{"contradictory absence", Observation{State: Absent, Bytes: []byte("{}")}, missing, Absent, Stop, ErrRecordsUncertain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, plan, err := Classify(c.plan, c.completion, c.unpublished)
			needPlan := got == IncompleteCandidate || got == CompleteCandidate
			if got != c.want || !errors.Is(err, c.wantErr) || (plan != nil) != needPlan {
				t.Fatal(got, plan, err)
			}
			if plan != nil && (plan.OldCovers != nil || plan.NewImages == nil || plan.RestoreID != strings.Repeat("1", 32)) {
				t.Fatal(plan)
			}
		})
	}
}

func TestInvalidPlanFields(t *testing.T) {
	for _, c := range []struct{ name, from, to string }{
		{"future version", `"version":1`, `"version":2`},
		{"float version", `"version":1`, `"version":1.0`},
		{"unknown key", `"version":1`, `"version":1,"path":"../elsewhere"`},
		{"escaped duplicate key", `"version":1`, `"version":1,"versi\u006fn":1`},
		{"duplicate null key", `"oldCovers":null`, `"oldCovers":null,"oldCovers":null`},
		{"missing old covers", `"oldCovers":null,`, ""},
		{"null images", `"newImages":[]`, `"newImages":null`},
		{"invalid path token", strings.Repeat("2", 32), "../workspace"},
		{"uppercase ID", strings.Repeat("2", 32), strings.Repeat("A", 32)},
		{"duplicate old DB identity", strings.Repeat("5", 32), strings.Repeat("3", 32)},
		{"old covers aliases DB", `"oldCovers":null`, `"oldCovers":{"volumeSerial":"0011223344556677","fileId":"33333333333333333333333333333333"}`},
		{"trailing object", "\n}", "\n}{}"},
		{"invalid UTF8", "\n}", string([]byte{0xff}) + "}"},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := strings.Replace(examplePlan, c.from, c.to, 1)
			if data == examplePlan {
				t.Fatal("fixture was not changed")
			}
			if got, plan, err := Classify(observed(data), Observation{State: Absent}, Absent); got != Stop || plan != nil || !errors.Is(err, ErrInvalidRecord) {
				t.Fatal(got, plan, err)
			}
		})
	}
}

func TestRecoveryRecordSizeAndCountBoundaries(t *testing.T) {
	absent := Observation{State: Absent}
	plan := examplePlan + strings.Repeat(" ", MaxPlanBytes-len(examplePlan))
	if got, _, err := Classify(observed(plan), absent, Absent); got != IncompleteCandidate || err != nil {
		t.Fatal(got, err)
	}
	if got, _, err := Classify(observed(plan+" "), absent, Absent); got != Stop || !errors.Is(err, ErrInvalidRecord) {
		t.Fatal(got, err)
	}
	done := completionFor(examplePlan)
	done += strings.Repeat(" ", MaxCompletionBytes-len(done))
	if got, _, err := Classify(observed(examplePlan), observed(done), Absent); got != CompleteCandidate || err != nil {
		t.Fatal(got, err)
	}
	if got, _, err := Classify(observed(examplePlan), observed(done+" "), Absent); got != Stop || !errors.Is(err, ErrInvalidRecord) {
		t.Fatal(got, err)
	}

	var imgs []string
	for i := 1; i <= backup.MaxImages+1; i++ {
		imgs = append(imgs, fmt.Sprintf(
			`{"imageId":"%032x","stagedId":"%032x","identity":{"volumeSerial":"0011223344556677","fileId":"%032x"}}`, i, i, i))
	}
	planWithImages := func(images []string) Observation {
		return observed(strings.Replace(examplePlan, `"newImages":[]`, `"newImages":[`+strings.Join(images, ",")+`]`, 1))
	}
	if got, parsed, err := Classify(planWithImages(imgs[:backup.MaxImages]), absent, Absent); got != IncompleteCandidate || err != nil || len(parsed.NewImages) != backup.MaxImages {
		t.Fatal(got, err)
	}
	if got, _, err := Classify(planWithImages(imgs), absent, Absent); got != Stop || !errors.Is(err, ErrInvalidRecord) {
		t.Fatal(got, err)
	}
}

func TestRecoveryRecordRejectsImageAliases(t *testing.T) {
	image := func(id, staged, file string) string {
		return fmt.Sprintf(`{"imageId":"%s","stagedId":"%s","identity":{"volumeSerial":"0011223344556677","fileId":"%s"}}`,
			strings.Repeat(id, 32), strings.Repeat(staged, 32), strings.Repeat(file, 32))
	}
	first := image("a", "b", "c")
	for _, tc := range []struct{ name, second string }{
		{"duplicate image id", image("a", "d", "e")},
		{"duplicate staged id", image("d", "b", "e")},
		{"staged id aliases new DB", image("d", "4", "e")},
		{"duplicate file identity", image("d", "e", "c")},
		{"file identity aliases new DB", image("d", "e", "5")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.Replace(examplePlan, `"newImages":[]`, `"newImages":[`+first+","+tc.second+`]`, 1)
			got, plan, err := Classify(observed(data), Observation{State: Absent}, Absent)
			if got != Stop || plan != nil || !errors.Is(err, ErrInvalidRecord) {
				t.Fatal(got, plan, err)
			}
		})
	}
}
