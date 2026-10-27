package manga

// WorkDeleteInput is the exact content confirmed by the user, not new input.
// Do not normalize it or include timestamps: compare the current values only.
type WorkDeleteInput struct {
	URL           string
	Title         string
	SiteName      string
	Notes         string
	ThumbnailPath string
	ScheduledDate ScheduledDate
	// nil keeps compatibility with callers that have no tag feature yet.
	// A supplied snapshot must exactly match the current ID/name set.
	Tags *[]Tag
}
