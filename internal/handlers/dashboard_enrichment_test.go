package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"fmt"
	"io"
	"log"
	"testing"
)

func TestDashboardEnrichmentBatchesAndPreservesRelationships(t *testing.T) {
	ctx := &config.AppContext{Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0)}
	own := &types.SpeakerConf{ID: "own"}
	other := &types.SpeakerConf{ID: "other"}
	draft := &types.Proposal{ID: "draft", Status: "Submitted", SpeakerConfRefs: []string{"own", "other"}}
	own.Proposals = append(own.Proposals, draft)
	for i := 0; i < 59; i++ {
		own.Proposals = append(own.Proposals, &types.Proposal{ID: fmt.Sprint(i), Status: StatusScheduled, SpeakerConfRefs: []string{"other", "own"}})
	}
	var speakers, talks, recordings int
	load := dashboardEnrichmentLoaders{
		speakers: func(_ *config.AppContext, ids []string, _ map[string]*types.Speaker, props map[string]*types.Proposal) ([]*types.SpeakerConf, error) {
			speakers++
			if len(ids) != 1 || ids[0] != "other" || len(props) != 60 {
				t.Fatalf("unexpected co-speaker batch: %v, %d", ids, len(props))
			}
			return []*types.SpeakerConf{other}, nil
		},
		talks: func(_ *config.AppContext, props map[string]*types.Proposal) ([]*types.ConfTalk, error) {
			talks++
			if len(props) != 59 || props["draft"] != nil {
				t.Fatal("wrong scheduled proposal selection")
			}
			var out []*types.ConfTalk
			for id, p := range props {
				out = append(out, &types.ConfTalk{ID: "talk-" + id, Proposal: p})
			}
			return out, nil
		},
		recordings: func(_ *config.AppContext, ids []string) ([]*types.Recording, error) {
			recordings++
			if len(ids) != 59 {
				t.Fatalf("recording batch size %d", len(ids))
			}
			var out []*types.Recording
			for _, id := range ids {
				out = append(out, &types.Recording{ConfTalkID: id})
			}
			return out, nil
		},
	}
	enrichDashboardProposalsWith(ctx, []*types.SpeakerConf{nil, own, own}, load)
	if speakers != 1 || talks != 1 || recordings != 1 {
		t.Fatalf("load counts: %d %d %d", speakers, talks, recordings)
	}
	for _, p := range own.Proposals[1:] {
		if len(p.Speakers) != 2 || p.Speakers[0] != other || p.Speakers[1] != own || p.ConfTalk.ID != "talk-"+p.ID || p.Recording.ConfTalkID != p.ConfTalk.ID {
			t.Fatalf("lost proposal relationships: %+v", p)
		}
	}
	if draft.ConfTalk != nil || draft.Recording != nil || len(draft.Speakers) != 2 {
		t.Fatal("draft enrichment changed")
	}
	enrichDashboardProposalsWith(ctx, nil, load)
	if speakers != 1 || talks != 1 || recordings != 1 {
		t.Fatal("empty dashboard performed reads")
	}
}
