package settings

import "errors"

type WorkListMode string

const (
	Compact WorkListMode = "compact"
	Rows    WorkListMode = "rows"
	Cards   WorkListMode = "cards"
)

var ErrWorkListMode = errors.New("invalid work list mode")

func (m WorkListMode) Validate() error {
	switch m {
	case Compact, Rows, Cards:
		return nil
	default:
		return ErrWorkListMode
	}
}
