package handlers

import (
	"btcpp-web/internal/types"
	"sort"
	"strings"
)

// Keep legacy talks without proposal status, as on the conference schedule.
func giftTalkEligible(talk *types.Talk) bool {
	if talk == nil {
		return false
	}
	switch talk.Status {
	case "", StatusAccepted, StatusScheduled:
		return true
	default:
		return false
	}
}

func speakerGiftRows(talks []*types.Talk, staff []*types.Speaker) []*GiftRow {
	// Prefer available artwork, then the smallest-panel talk per speaker. Key on Speaker.ID
	// when available, fall back to lower-cased name (older rows
	// may lack stable IDs).
	type pick struct {
		name    string
		clipart string
		panelN  int
	}
	best := map[string]*pick{}
	for _, talk := range talks {
		if !giftTalkEligible(talk) {
			continue
		}
		n := len(talk.Speakers)
		for _, sp := range talk.Speakers {
			if sp == nil {
				continue
			}
			key := sp.ID
			if key == "" {
				key = "name:" + strings.ToLower(strings.TrimSpace(sp.Name))
			}
			prev, ok := best[key]
			if !ok {
				best[key] = &pick{name: sp.Name, clipart: talk.Clipart, panelN: n}
				continue
			}
			// Never replace usable panel artwork with an empty solo-talk image.
			hasArt := strings.TrimSpace(talk.Clipart) != ""
			prevHasArt := strings.TrimSpace(prev.clipart) != ""
			if (hasArt && !prevHasArt) || (hasArt == prevHasArt && n < prev.panelN) {
				prev.clipart = talk.Clipart
				prev.panelN = n
				prev.name = sp.Name
			}
		}
	}

	rows := make([]*GiftRow, 0, len(best))
	for _, p := range best {
		rows = append(rows, &GiftRow{Clipart: p.clipart, SpeakerName: p.name})
	}

	// {conf}-staff Speakers row too — leading.png as their
	// clipart, skipped if they're already on a talk.
	for _, sp := range staff {
		if sp == nil {
			continue
		}
		key := sp.ID
		if key == "" {
			key = "name:" + strings.ToLower(strings.TrimSpace(sp.Name))
		}
		if _, ok := best[key]; ok {
			continue
		}
		best[key] = &pick{} // mark to dedupe across staff list itself
		rows = append(rows, &GiftRow{
			Clipart:     "leading.png",
			SpeakerName: sp.Name,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].SpeakerName) < strings.ToLower(rows[j].SpeakerName)
	})

	return rows
}
