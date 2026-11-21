package types

// ConferenceCommitteeMember contains only the fields used by public event cards.
type ConferenceCommitteeMember struct {
	Speaker       *Speaker
	Track         string
	Organizations []*Org
	ProfileURL    string
}
