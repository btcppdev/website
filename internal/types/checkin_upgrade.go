package types

import "time"

type CheckInRequirement struct {
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

type CheckInEligibility struct {
	Requirements []CheckInRequirement
	Verified     bool
	Upgraded     bool
	Revoked      bool
	Revision     int64
}

func (e *CheckInEligibility) NeedsProof() bool {
	return e != nil && !e.Revoked && !e.Verified && !e.Upgraded && len(e.Requirements) > 0
}

type CheckInUpgrade struct {
	ID, TicketRef, OwnerKey, OriginalType, Currency      string
	Revision, PreviousCents, FullPriceCents, AmountCents int64
	Status, ChargeID, Invoice, ChainAddress              string
	TotalSats                                            int64
	ExpiresAt                                            *time.Time
}
