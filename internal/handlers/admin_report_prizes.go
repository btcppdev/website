package handlers

import "strings"

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
