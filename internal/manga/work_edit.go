package manga

import (
	"net/url"
	"strings"
)

// EditWorkInput cannot replace images or timestamps. A nil date preserves it;
// a pointer to the empty date explicitly clears it.
type EditWorkInput struct {
	URL, Title, SiteName, Notes string
	ScheduledDate               *ScheduledDate
	// nil preserves associations; a non-nil empty selection removes all tags.
	Tags *TagEdit
}

func (input EditWorkInput) Normalize() (EditWorkInput, error) {
	input.URL = strings.TrimSpace(input.URL)
	input.Title = strings.TrimSpace(input.Title)
	input.SiteName = strings.TrimSpace(input.SiteName)
	input.Notes = strings.TrimSpace(input.Notes)
	if err := validateWorkText(input.URL, input.Title); err != nil {
		return EditWorkInput{}, err
	}
	if input.ScheduledDate != nil {
		date := *input.ScheduledDate
		if err := date.Validate(); err != nil {
			return EditWorkInput{}, err
		}
		input.ScheduledDate = &date
	}
	if input.Tags != nil {
		tags, err := input.Tags.Normalize()
		if err != nil {
			return EditWorkInput{}, err
		}
		input.Tags = &tags
	}
	return input, nil
}

func validateWorkText(rawURL, title string) error {
	if rawURL == "" {
		return ErrURLRequired
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ErrURLScheme
	}
	if title == "" {
		return ErrTitleRequired
	}
	return nil
}
