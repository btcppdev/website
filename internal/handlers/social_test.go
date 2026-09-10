package handlers

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
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

func TestSocialTicketReplyUsesEventDetails(t *testing.T) {
	conf := &types.Conf{Tag: "berlin26", Location: "Berlin", DateDesc: "September"}
	want := "Tickets are going fast, don't miss the chance to catch the frontier of bitcoin in Berlin this September -> https://btcpp.dev/berlin26#tickets"
	if got := socialTicketReply(conf); got != want {
		t.Fatalf("ticket reply = %q, want %q", got, want)
	}
	if got := (&SocialAdminPage{Conf: conf}).XTicketReply(); got != want {
		t.Fatalf("preview differs from queued reply: %q", got)
	}
}

func TestSocialTicketReplyFromForm(t *testing.T) {
	conf := &types.Conf{Tag: "berlin26", Location: "Berlin", DateDesc: "September"}
	for _, group := range []string{"speaker", "talk", "sponsor"} {
		field := group + "_one"
		for _, raw := range []string{"My edited reply\nhttps://btcpp.dev/berlin26#tickets", "", "  "} {
			form := url.Values{"reply_" + field: {raw}, "reply_" + group + "_other": {"Other post's reply"}}
			r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			want := raw
			if strings.TrimSpace(raw) == "" {
				want = socialTicketReply(conf)
			}
			if got := socialTicketReplyFromForm(conf, r, field); got != want {
				t.Fatalf("%s: got %q, want %q", field, got, want)
			}
		}
	}
}

func TestValidateSocialProgramSelection(t *testing.T) {
	for _, status := range []string{StatusAccepted, StatusScheduled, "Applied", "Rejected", "Withdrawn", ""} {
		t.Run(status, func(t *testing.T) {
			talks := []*types.Talk{{ID: "talk", Name: "Bitcoin", Status: status, Speakers: []*types.Speaker{{ID: "speaker"}}}}
			for _, form := range []url.Values{
				{"selected_speaker": {"speaker"}, "talkid_speakerspeaker": {"talk"}},
				{"selected_talk": {"talk"}},
			} {
				err := validateSocialProgramSelection(talks, form)
				want := status == StatusAccepted || status == StatusScheduled
				if (err == nil) != want {
					t.Fatalf("status %q form %v: error %v", status, form, err)
				}
			}
		})
	}
	talks := []*types.Talk{{ID: "accepted", Name: "Bitcoin", Status: StatusAccepted, Speakers: []*types.Speaker{nil, {ID: "speaker"}}}, {ID: "placeholder", Name: "TBD", Status: StatusScheduled, Speakers: []*types.Speaker{{ID: "speaker"}}}}
	for _, form := range []url.Values{
		{"selected_speaker": {"speaker"}, "talkid_speakerspeaker": {"other-event-talk"}},
		{"selected_speaker": {"other-speaker"}, "talkid_speakerother-speaker": {"accepted"}},
		{"selected_speaker": {"speaker"}},
		{"selected_talk": {"other-event-talk"}},
		{"selected_talk": {"placeholder"}},
		{"selected_talk": {"accepted", "rejected"}},
	} {
		if err := validateSocialProgramSelection(talks, form); err == nil {
			t.Fatalf("accepted invalid selection %v", form)
		}
	}
	// An accepted/scheduled speaker can be announced while their title is TBD.
	if err := validateSocialProgramSelection(talks, url.Values{"selected_speaker": {"speaker"}, "talkid_speakerspeaker": {"placeholder"}}); err != nil {
		t.Fatal(err)
	}
}
