package settings

import "errors"

// WorkSort keeps the sort key and direction in one validated preference.
type WorkSort string

const (
	RegistrationAsc  WorkSort = "registration_asc"
	RegistrationDesc WorkSort = "registration_desc"
	TitleAsc         WorkSort = "title_asc"
	TitleDesc        WorkSort = "title_desc"
	LastAccessAsc    WorkSort = "last_access_asc"
	LastAccessDesc   WorkSort = "last_access_desc"
	ScheduledDate    WorkSort = "scheduled_date"
)

var ErrWorkSort = errors.New("invalid work sort")

func (s WorkSort) Validate() error {
	switch s {
	case RegistrationAsc, RegistrationDesc, TitleAsc, TitleDesc, LastAccessAsc, LastAccessDesc, ScheduledDate:
		return nil
	default:
		return ErrWorkSort
	}
}
