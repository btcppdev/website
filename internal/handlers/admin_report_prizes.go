package handlers

import (
	"sort"
	"strings"
)

// AwardedSats is a sourced award amount; Sats is the independently recorded
// sent/claimed payout sum. Do not turn pending prizes into paid amounts.
func (w reportWinner) PrizeFigure() *reportMetric {
	if w.AwardedSats > 0 {
		return &reportMetric{Value: compactReportSats(w.AwardedSats), Label: "sats awarded"}
	}
	if w.Sats > 0 {
		return &reportMetric{Value: compactReportSats(w.Sats), Label: "sats sent / claimed"}
	}
	return nil
}
func (p sponsorReportPage) WinnerAwardTotal() string {
	var total int64
	for _, w := range p.Winners {
		total += w.AwardedSats
	}
	if total == 0 {
		return ""
	}
	return compactReportSats(total)
}

func (w reportWinner) PublicID() string { return strings.TrimPrefix(w.ID, "featured-") }

func (p reportPerson) Initials() string {
	parts := strings.Fields(p.Name)
	var letters []rune
	for _, part := range parts {
		r := []rune(part)
		if len(r) > 0 {
			letters = append(letters, r[0])
		}
		if len(letters) == 2 {
			break
		}
	}
	return strings.ToUpper(string(letters))
}

func (w reportWinner) PlaceRank() int {
	if w.Rank != 0 {
		return w.Rank
	}
	// Compatibility for the reviewed historical report fixtures.
	switch strings.ToLower(strings.TrimSpace(w.Awards)) {
	case "1st overall", "first place", "1st place":
		return 1
	case "2nd overall", "second place", "2nd place":
		return 2
	case "3rd overall", "third place", "3rd place":
		return 3
	}
	return -1
}
func (w reportWinner) PlaceLabel() string {
	switch w.PlaceRank() {
	case 1:
		return "1st place"
	case 2:
		return "2nd place"
	case 3:
		return "3rd place"
	}
	return ""
}
func (p sponsorReportPage) PodiumWinners() []reportWinner {
	var out []reportWinner
	for _, w := range p.Winners {
		if r := w.PlaceRank(); r >= 1 && r <= 3 {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PlaceRank() < out[j].PlaceRank() })
	return out
}
func (p sponsorReportPage) OtherWinners() []reportWinner {
	var out []reportWinner
	for _, w := range p.Winners {
		if r := w.PlaceRank(); r < 1 || r > 3 {
			out = append(out, w)
		}
	}
	return out
}
