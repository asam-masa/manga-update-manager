package manga

import (
	"errors"
	"time"
)

// ScheduledDate is a Japanese calendar date, not an instant. Empty means unset.
type ScheduledDate string

var ErrScheduledDate = errors.New("更新予定日は実在する日付をYYYY-MM-DDで入力してください")

func (d ScheduledDate) Validate() error {
	if d == "" {
		return nil
	}
	value, err := time.Parse("2006-01-02", string(d))
	if err != nil || len(d) != 10 || value.Year() < 1 || value.Format("2006-01-02") != string(d) {
		return ErrScheduledDate
	}
	return nil
}
