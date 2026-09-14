package types

const MerchProductTypeBadgeCanvas = "badge_canvas"

// BadgeCanvas is an order-time snapshot, not a live link to grant metadata.
// Grant and event IDs remain audit identifiers even after an account merge.
type BadgeCanvas struct {
	Reference       string `json:"reference"`
	BadgeName       string `json:"badge_name"`
	ArtworkURL      string `json:"artwork_url"`
	IssuerPubkey    string `json:"issuer_pubkey"`
	BadgeIdentifier string `json:"badge_identifier"`
	AwardEventID    string `json:"award_event_id"`
}

func (p *MerchProduct) CategoryName() string {
	if p.ProductType == MerchProductTypeBadgeCanvas {
		return "prints"
	}
	return p.ProductType
}
