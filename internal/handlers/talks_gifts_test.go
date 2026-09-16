package handlers

import (
	"btcpp-web/internal/types"
	"reflect"
	"testing"
)

func TestSpeakerGiftRowsExcludeDeclinedTalks(t *testing.T) {
	vincenzo := &types.Speaker{ID: "v", Name: "Vincenzo"}
	chris := &types.Speaker{ID: "c", Name: "Chris Ritter"}
	active := &types.Speaker{ID: "a", Name: "Active speaker"}
	staff := &types.Speaker{ID: "s", Name: "Staff member"}
	talks := []*types.Talk{
		nil,
		{Status: "TheyDecline", Clipart: "declined.png", Speakers: []*types.Speaker{vincenzo, chris}},
		{Status: "TheyDecline", Clipart: "old-solo.png", Speakers: []*types.Speaker{active}},
		{Status: StatusAccepted, Clipart: "panel.png", Speakers: []*types.Speaker{active, nil}},
	}
	want := []*GiftRow{{SpeakerName: "Active speaker", Clipart: "panel.png"}, {SpeakerName: "Staff member", Clipart: "leading.png"}}
	if got := speakerGiftRows(talks, []*types.Speaker{staff, active}); !reflect.DeepEqual(got, want) {
		t.Fatalf("gift rows = %+v, want %+v", got, want)
	}
	// A declined talk does not revoke an independently assigned staff gift.
	rows := speakerGiftRows(talks, []*types.Speaker{chris})
	if len(rows) != 2 || rows[1].SpeakerName != chris.Name || rows[1].Clipart != "leading.png" {
		t.Fatalf("explicit staff member missing: %+v", rows)
	}
}

func TestGiftTalkEligibleStatuses(t *testing.T) {
	for status, want := range map[string]bool{
		"": true, StatusAccepted: true, StatusScheduled: true,
		"TheyDecline": false, "WeDecline": false, "Rejected": false,
		"Waitlisted": false, "Invited": false, "Applied": false,
	} {
		t.Run(status, func(t *testing.T) {
			if got := giftTalkEligible(&types.Talk{Status: status}); got != want {
				t.Fatalf("eligible = %v, want %v", got, want)
			}
		})
	}
}

func TestSpeakerGiftRowsPreferPanelArtworkOverBlankSolo(t *testing.T) {
	michael := &types.Speaker{ID: "michael", Name: "Michael1011"}
	other := &types.Speaker{ID: "other", Name: "Other panelist"}
	blank := &types.Talk{Status: StatusAccepted, Speakers: []*types.Speaker{michael}}
	panel := &types.Talk{Status: StatusScheduled, Clipart: "berlin26_panel_20cd.png", Speakers: []*types.Speaker{michael, other}}
	for _, talks := range [][]*types.Talk{{blank, panel}, {panel, blank}} {
		rows := speakerGiftRows(talks, nil)
		if len(rows) != 2 || rows[0].SpeakerName != michael.Name || rows[0].Clipart != panel.Clipart {
			t.Fatalf("panel artwork lost: %+v", rows)
		}
	}
	solo := &types.Talk{Status: StatusAccepted, Clipart: "solo.png", Speakers: []*types.Speaker{michael}}
	rows := speakerGiftRows([]*types.Talk{panel, solo}, nil)
	if rows[0].Clipart != solo.Clipart {
		t.Fatal("illustrated solo talk should still beat illustrated panel")
	}
}
