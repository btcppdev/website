package handlers

import (
	"btcpp-web/internal/types"
	"testing"
)

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
