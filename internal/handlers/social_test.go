package handlers

import (
	"net/url"
	"reflect"
	"testing"

	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

func TestSelectedSocialIDsOnlyUsesExplicitSelectionField(t *testing.T) {
	form := url.Values{
		"selected_speaker":     {"speaker-a", "", "speaker-a", " speaker-b "},
		"speaker_not_selected": {"on"},
		"text_speaker_hidden":  {"post copy"},
	}

	got := selectedSocialIDs(form, "selected_speaker")
	want := []string{"speaker-a", "speaker-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedSocialIDs() = %#v, want %#v", got, want)
	}
}

func TestEligibleSocialTalks(t *testing.T) {
	accepted := &types.Talk{ID: "accepted", Status: StatusAccepted}
	scheduled := &types.Talk{ID: "scheduled", Status: StatusScheduled}
	applied := &types.Talk{ID: "applied", Status: "Applied"}
	rejected := &types.Talk{ID: "rejected", Status: "Rejected"}

	got := eligibleSocialTalks([]*types.Talk{accepted, nil, applied, scheduled, rejected})
	want := []*types.Talk{accepted, scheduled}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eligibleSocialTalks() = %#v, want %#v", got, want)
	}
}

func TestSpeakerSocialAlreadyPostedRecognizesLegacyTalkRef(t *testing.T) {
	const (
		confTag   = "toronto"
		speakerID = "speaker-1"
	)
	talks := []*types.Talk{{ID: "first-talk"}, {ID: "preferred-talk"}}
	postedRefs := map[string]bool{
		helpers.SpeakerSocialPostRef(confTag, "first-talk", speakerID): true,
	}

	if !speakerSocialAlreadyPosted(postedRefs, confTag, speakerID, talks) {
		t.Fatal("expected a post recorded with the legacy talk ID to suppress the speaker row")
	}
}
