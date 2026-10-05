package platform

import (
	"testing"
	"time"
)

func TestInJapan(t *testing.T) {
	utc := time.Date(2026, 10, 5, 0, 30, 0, 0, time.UTC)
	got := InJapan(utc)

	if got.Location().String() != "Asia/Tokyo" {
		t.Fatalf("Location = %q", got.Location())
	}
	if got.Hour() != 9 || got.Day() != 5 {
		t.Fatalf("InJapan() = %v", got)
	}
}
