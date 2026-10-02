package handlers

import (
	"reflect"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestBuildEventBlocksCombinesSpeakerRecords(t *testing.T) {
	for _, ended := range []bool{false, true} {
		name := "upcoming"
		days := 30
		if ended {
			name, days = "past", -30
		}
		t.Run(name, func(t *testing.T) {
			conf := &types.Conf{Tag: "berlin26", StartDate: time.Now().AddDate(0, 0, days), EndDate: time.Now().AddDate(0, 0, days+1)}
			panel := &types.Proposal{ID: "panel", Status: StatusScheduled, ScheduleFor: conf}
			secondPanel := &types.Proposal{ID: "second-panel", Status: StatusAccepted, ScheduleFor: conf}
			declined := &types.Proposal{ID: "declined", Status: "WeDecline", ScheduleFor: conf}
			newer := &types.SpeakerConf{ID: "newer", Proposals: []*types.Proposal{panel, secondPanel}}
			older := &types.SpeakerConf{ID: "older", Proposals: []*types.Proposal{declined, panel}}
			for _, records := range [][]*types.SpeakerConf{{newer, older}, {older, newer}} {
				active, past := buildEventBlocks(records, nil, nil, nil, []*types.Conf{conf}, nil)
				blocks := active
				if ended {
					blocks = past
				}
				if len(active)+len(past) != 1 || len(blocks) != 1 {
					t.Fatalf("active=%d past=%d", len(active), len(past))
				}
				got := map[string]int{}
				for _, p := range blocks[0].SpeakerConf.Proposals {
					got[p.ID]++
				}
				if !reflect.DeepEqual(got, map[string]int{"panel": 1, "second-panel": 1, "declined": 1}) {
					t.Fatalf("talks were lost or duplicated: %v", got)
				}
				if !reflect.DeepEqual(blocks[0].SpeakerConfIDs, map[string]bool{"newer": true, "older": true}) {
					t.Fatalf("lost own speaker identities: %v", blocks[0].SpeakerConfIDs)
				}
				if !reflect.DeepEqual(newer.Proposals, []*types.Proposal{panel, secondPanel}) || !reflect.DeepEqual(older.Proposals, []*types.Proposal{declined, panel}) {
					t.Fatal("modified source speaker records")
				}
			}
		})
	}
}

func TestBuildEventBlocksScopesEachProposalToItsEvent(t *testing.T) {
	berlin := &types.Conf{Tag: "berlin26", StartDate: time.Now().AddDate(0, 0, 30)}
	seoul := &types.Conf{Tag: "seoul", StartDate: time.Now().AddDate(0, 0, 60)}
	berlinTalk := &types.Proposal{ID: "berlin-talk", ScheduleFor: berlin}
	seoulTalk := &types.Proposal{ID: "seoul-talk", ScheduleFor: seoul}
	sc := &types.SpeakerConf{ID: "shared", Proposals: []*types.Proposal{nil, berlinTalk, seoulTalk, {ID: "unassigned"}}}
	active, past := buildEventBlocks([]*types.SpeakerConf{nil, {}, sc}, nil, nil, nil, []*types.Conf{berlin, seoul}, nil)
	if len(active) != 2 || len(past) != 0 {
		t.Fatalf("active=%d past=%d", len(active), len(past))
	}
	for _, block := range active {
		if len(block.SpeakerConf.Proposals) != 1 || block.SpeakerConf.Proposals[0].ScheduleFor != block.Conf {
			t.Fatalf("proposal assigned to wrong event: %+v", block)
		}
	}
}
