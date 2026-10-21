package manga

import (
	"net/url"
	"strings"
)

// EditWorkInput contains only editable text; it cannot replace images or times.
type EditWorkInput struct {
	URL, Title, SiteName, Notes string
}

func (input EditWorkInput) Normalize() (EditWorkInput, error) {
	input.URL = strings.TrimSpace(input.URL)
	input.Title = strings.TrimSpace(input.Title)
	input.SiteName = strings.TrimSpace(input.SiteName)
	input.Notes = strings.TrimSpace(input.Notes)
	if err := validateWorkText(input.URL, input.Title); err != nil {
		return EditWorkInput{}, err
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
