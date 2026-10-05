package platform

import (
	"time"
	_ "time/tzdata"
)

var japanLocation = mustLoadJapanLocation()

// JapanLocation returns the fixed application timezone for user-facing dates and schedules.
func JapanLocation() *time.Location {
	return japanLocation
}

// InJapan converts an instant to the application timezone.
func InJapan(value time.Time) time.Time {
	return value.In(japanLocation)
}

func mustLoadJapanLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic("load Asia/Tokyo: " + err.Error())
	}
	return location
}
