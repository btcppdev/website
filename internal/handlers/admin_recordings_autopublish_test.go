package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestRecordingAutopublishEligibility(t *testing.T) {
	future := time.Now().Add(time.Hour)

	rec := &types.Recording{
		FileURI:   "videos/talk.mp4",
		PublishAt: &future,
	}
	row := &RecordingRow{Recording: rec}
	if !shouldUploadRecordingToYouTube(row) {
		t.Fatalf("recording with FileURI, PublishAt, and no YTLink should upload")
	}
	row.YTStatus = recordingStatusFailed
	if shouldUploadRecordingToYouTube(row) {
		t.Fatalf("failed YouTube recording should wait for explicit retry")
	}
	row.YTStatus = recordingStatusPending
	row.YTURL = "https://youtu.be/example"
	if shouldUploadRecordingToYouTube(row) {
		t.Fatalf("recording with YTLink should not upload again")
	}

}

func TestRecordingSourceObjectKeyNormalizesSpacesValues(t *testing.T) {
	tests := map[string]string{
		" videos/talk.mp4 ": "videos/talk.mp4",
		"/videos/talk.mp4":  "videos/talk.mp4",
		"https://btcpp.nyc3.digitaloceanspaces.com/videos/talk.mp4":     "videos/talk.mp4",
		"https://btcpp.nyc3.digitaloceanspaces.com/videos/talk%201.mp4": "videos/talk 1.mp4",
	}

	for in, want := range tests {
		if got := recordingSourceObjectKey(in); got != want {
			t.Fatalf("recordingSourceObjectKey(%q) = %q, want %q", in, got, want)
		}
	}
}
