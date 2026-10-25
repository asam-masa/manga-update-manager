package manga

import (
	"errors"
	"testing"
)

func TestScheduledDateValidation(t *testing.T) {
	for _, value := range []ScheduledDate{"", "0001-01-01", "9999-12-31", "2024-02-29", "2000-02-29", "2026-10-30"} {
		if err := value.Validate(); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []ScheduledDate{"0000-01-01", "10000-01-01", "1900-02-29", "2026-02-29", "2026-04-31", "2026-00-01", "2026-01-00", "2026-1-01", "2026-01-1", "2026/01/01", " 2026-01-01", "2026-01-01 ", "2026-01-01T00:00:00Z"} {
		if err := value.Validate(); !errors.Is(err, ErrScheduledDate) {
			t.Fatal(value, err)
		}
		if _, err := (EditWorkInput{URL: "https://example.com", Title: "x", ScheduledDate: &value}).Normalize(); !errors.Is(err, ErrScheduledDate) {
			t.Fatal(value, err)
		}
	}
}

func TestScheduledDateEditOmissionAndExplicitClear(t *testing.T) {
	input := EditWorkInput{URL: "https://example.com", Title: "x"}
	got, err := input.Normalize()
	if err != nil || got.ScheduledDate != nil {
		t.Fatal(got, err)
	}
	for _, date := range []ScheduledDate{"", "2026-10-30"} {
		input.ScheduledDate = &date
		got, err := input.Normalize()
		if err != nil || got.ScheduledDate == nil || *got.ScheduledDate != date {
			t.Fatal(got, err)
		}
		date = "invalid"
		if *got.ScheduledDate == date {
			t.Fatal("normalized input aliases caller's date")
		}
	}
}
