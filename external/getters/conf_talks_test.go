package getters

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestTalkFromConfTalkMarksRecordingRestricted(t *testing.T) {
	talk := talkFromConfTalk(&types.ConfTalk{ID: "ct-1"}, &types.Proposal{
		Title: "No Camera Please",
		Speakers: []*types.SpeakerConf{
			{RecordOK: "RecordingOK", Speaker: &types.Speaker{ID: "sp-1", Name: "Ada"}},
			{RecordOK: "NoRecord", Speaker: &types.Speaker{ID: "sp-2", Name: "Grace"}},
		},
	})

	if !talk.RecordingRestricted {
		t.Fatal("RecordingRestricted = false, want true")
	}
}

func TestTalkFromConfTalkMarksNoRecordingAliasesRestricted(t *testing.T) {
	for _, recordOK := range []string{"NoRecord", "NoRecording", "No Recording", "NoFace", "No Face"} {
		t.Run(recordOK, func(t *testing.T) {
			talk := talkFromConfTalk(&types.ConfTalk{ID: "ct-1"}, &types.Proposal{
				Title: "No Recording Please",
				Speakers: []*types.SpeakerConf{
					{RecordOK: recordOK, Speaker: &types.Speaker{ID: "sp-1", Name: "Ada"}},
				},
			})

			if !talk.RecordingRestricted {
				t.Fatal("RecordingRestricted = false, want true")
			}
			if talk.RecordingAudioOnly {
				t.Fatal("RecordingAudioOnly = true, want false")
			}
		})
	}
}

func TestTalkFromConfTalkMarksAudioOnly(t *testing.T) {
	talk := talkFromConfTalk(&types.ConfTalk{ID: "ct-1"}, &types.Proposal{
		Title: "Audio Please",
		Speakers: []*types.SpeakerConf{
			{RecordOK: "AudioOnly", Speaker: &types.Speaker{ID: "sp-1", Name: "Ada"}},
		},
	})

	if !talk.RecordingAudioOnly {
		t.Fatal("RecordingAudioOnly = false, want true")
	}
	if talk.RecordingRestricted {
		t.Fatal("RecordingRestricted = true, want false")
	}
	if got, want := talk.Speakers[0].RecordingEmoji, "🔇"; got != want {
		t.Fatalf("speaker RecordingEmoji = %q, want %q", got, want)
	}
}

func TestTalkFromConfTalkLeavesRecordingOKUnflagged(t *testing.T) {
	for _, recordOK := range []string{"RecordOK", "RecordingOK", ""} {
		t.Run(recordOK, func(t *testing.T) {
			talk := talkFromConfTalk(&types.ConfTalk{ID: "ct-1"}, &types.Proposal{
				Title: "Recording OK",
				Speakers: []*types.SpeakerConf{
					{RecordOK: recordOK, Speaker: &types.Speaker{ID: "sp-1", Name: "Ada"}},
				},
			})

			if talk.RecordingRestricted {
				t.Fatal("RecordingRestricted = true, want false")
			}
			if talk.RecordingAudioOnly {
				t.Fatal("RecordingAudioOnly = true, want false")
			}
			if got := talk.Speakers[0].RecordingEmoji; got != "" {
				t.Fatalf("speaker RecordingEmoji = %q, want empty", got)
			}
		})
	}
}

func TestTalkFromConfTalkIgnoresBlankRecordingConsent(t *testing.T) {
	talk := talkFromConfTalk(&types.ConfTalk{ID: "ct-1"}, &types.Proposal{
		Title: "Legacy Talk",
		Speakers: []*types.SpeakerConf{
			{RecordOK: "", Speaker: &types.Speaker{ID: "sp-1", Name: "Ada"}},
		},
	})

	if talk.RecordingRestricted {
		t.Fatal("RecordingRestricted = true, want false")
	}
}
