package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestRecordingSpeakersForProposalResolvesSpeakerConfRefs(t *testing.T) {
	speaker := &types.Speaker{ID: "speaker-recordings-test-a", Name: "Ada"}

	got := recordingSpeakersForProposal(&types.Proposal{
		Speakers: []*types.SpeakerConf{
			{ID: "speakerconf-recordings-test-a", Speaker: speaker},
		},
	})

	if len(got) != 1 {
		t.Fatalf("got %d speakers, want 1", len(got))
	}
	if got[0] != speaker {
		t.Fatalf("got speaker %#v, want %#v", got[0], speaker)
	}
}

func TestRecordingSpeakersForProposalDedupesResolvedAndEnrichedSpeakers(t *testing.T) {
	speaker := &types.Speaker{ID: "speaker-recordings-test-b", Name: "Grace"}

	got := recordingSpeakersForProposal(&types.Proposal{
		Speakers: []*types.SpeakerConf{
			{ID: "speakerconf-recordings-test-b", Speaker: speaker},
			{Speaker: speaker},
		},
	})

	if len(got) != 1 {
		t.Fatalf("got %d speakers, want 1", len(got))
	}
	if got[0] != speaker {
		t.Fatalf("got speaker %#v, want %#v", got[0], speaker)
	}
}

func TestRecordingXSocialPostUsesTwitterInternally(t *testing.T) {
	rec := &types.Recording{ID: "recording-recordings-test-x"}

	if got := recordingSocialPostRef(rec, recordingPlatformX); got != "recording:recording-recordings-test-x:twitter" {
		t.Fatalf("x social post ref = %q", got)
	}
	if recordingPlatformX != "twitter" {
		t.Fatalf("x platform = %q, want twitter", recordingPlatformX)
	}
}

func TestRecordingBulkUploadAllowsExplicitRetryOfFailures(t *testing.T) {
	row := &RecordingRow{
		Recording: &types.Recording{
			ID:      "recording-manual-retry-test",
			FileURI: "toronto/recordings/talk.mp4",
		},
		YTStatus: recordingStatusFailed,
	}
	if reason := recordingBulkUploadSkipReason(row); reason != "" {
		t.Fatalf("failed recording skip reason = %q, want eligible for manual retry", reason)
	}

	row.YTStatus = recordingStatusAuthRequired
	if reason := recordingBulkUploadSkipReason(row); reason != "" {
		t.Fatalf("auth-required recording skip reason = %q, want eligible after manual reauthorization", reason)
	}

	row.YTStatus = recordingStatusUploading
	if reason := recordingBulkUploadSkipReason(row); reason != "YouTube status is uploading" {
		t.Fatalf("uploading recording skip reason = %q", reason)
	}
}
