package publishing

import "time"

// Cancellation does not require a source file, runtime or a new publication
// slot. It does require an idle operation and an identified future schedule.
func recordingCancellationUnavailable(d RecordingItem, now time.Time) string {
	if d.CancelPending {
		return "Cancellation is awaiting confirmation; retry it if synchronization failed"
	}
	switch d.State {
	case "syncing", "needs_review", "creating", "created", "uploading_poster", "poster_uploaded", "finalizing":
		return "Resolve the in-progress schedule change before canceling this item"
	case "live", "ended", "published":
		return "This item is already live or published"
	}
	if d.YouTubePrivacy == "public" || d.YouTubePrivacy == "published" {
		return "This video is already public on YouTube"
	}
	if d.ConfirmedAt == nil && d.At == nil {
		return "This item has no schedule to cancel"
	}
	at := d.ConfirmedAt
	if at == nil {
		at = d.At
	}
	if !at.After(now.Add(time.Minute)) {
		return "The publication has started or is less than a minute away"
	}
	if d.ChannelID == "broadcasts" && d.BroadcastID == "" && d.SourceState != "" && d.SourceState != "canceled" {
		return "Check X Studio and resolve the unidentified broadcast before canceling"
	}
	return ""
}

// Worker checks omit the pending-cancellation guard, but retain all safety
// checks and verify the snapshot of the provider identity before any write.
func validateRecordingCancellation(d RecordingItem, change RecordingChange, now time.Time) string {
	d.CancelPending = false
	if reason := recordingCancellationUnavailable(d, now); reason != "" {
		return reason
	}
	if d.ChannelID == "youtube" && d.YouTubeURL != change.YouTubeURL {
		return "The YouTube video changed. Review its schedule before canceling."
	}
	if d.ChannelID == "broadcasts" && d.BroadcastID != change.BroadcastID {
		return "The X broadcast changed. Review its schedule before canceling."
	}
	return ""
}
