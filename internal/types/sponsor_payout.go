package types

import "time"

// SponsorPayout is one cash prize for one finalized winning project. Keep this
// separate from admin payout records: tax documents and internal notes must
// never be loaded into the sponsor dashboard.
type SponsorPayout struct {
	AwardID         string
	AwardTitle      string
	ProjectID       string
	ProjectTitle    string
	PrizeID         string
	PrizeTitle      string
	PrizeSats       int64
	ConferenceTitle string
	Recipients      []*SponsorPayoutRecipient
}

type SponsorPayoutRecipient struct {
	PersonID         string
	Name             string
	DistributionID   string
	AmountSats       *int64
	Status           string
	TaxFormOnFile    bool
	LightningAddress string
	BitcoinAddress   string
	CompletedAt      *time.Time
}

func (r *SponsorPayoutRecipient) ReadyToPay() bool {
	return r != nil && r.DistributionID != "" && r.AmountSats != nil && *r.AmountSats > 0 &&
		(r.Status == "pending" || r.Status == "ready") && r.TaxFormOnFile &&
		(r.LightningAddress != "" || r.BitcoinAddress != "")
}

func (r *SponsorPayoutRecipient) StatusLabel() string {
	switch r.Status {
	case "sent", "claimed":
		return "Paid"
	case "cancelled":
		return "Cancelled"
	}
	if r.DistributionID == "" || r.AmountSats == nil || *r.AmountSats <= 0 {
		return "Awaiting payout allocation"
	}
	if !r.TaxFormOnFile {
		return "Awaiting winner paperwork"
	}
	if r.LightningAddress == "" && r.BitcoinAddress == "" {
		return "Awaiting payment address"
	}
	if r.ReadyToPay() {
		return "Ready to pay"
	}
	return "Awaiting organizer review"
}
