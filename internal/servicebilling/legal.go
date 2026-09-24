package servicebilling

import "time"

// LegalAcceptance is server-created evidence frozen with a purchase intent.
// Text is retained so later changes to a linked policy page cannot rewrite it.
type LegalAcceptance struct {
	Version       string    `json:"version"`
	TermsText     string    `json:"terms_text"`
	PrivacyText   string    `json:"privacy_text"`
	ContentSHA256 string    `json:"content_sha256"`
	AcceptedAt    time.Time `json:"accepted_at"`
}
