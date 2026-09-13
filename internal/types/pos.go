package types

import "time"

type POSProduct struct {
	VariantID string
	Name      string
	Label     string
	SKU       string
	Image     string
	Enabled   bool
	PriceSats int64
	Available int
	Central   int
}
type POSCartLine struct {
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
	PriceSats int64  `json:"price_sats"`
}
type POSSaleItem struct {
	VariantID string `json:"variant_id"`
	Name      string `json:"name"`
	Label     string `json:"label"`
	Quantity  int    `json:"quantity"`
	PriceSats int64  `json:"price_sats"`
}
type POSSale struct {
	ID           string        `json:"id"`
	ConferenceID string        `json:"-"`
	OperatorID   string        `json:"-"`
	Status       string        `json:"status"`
	TotalSats    int64         `json:"total_sats"`
	Currency     string        `json:"currency"`
	LocalPerBTC  float64       `json:"local_per_btc"`
	ChargeID     string        `json:"-"`
	Invoice      string        `json:"invoice"`
	ExpiresAt    *time.Time    `json:"expires_at"`
	CreatedAt    time.Time     `json:"created_at"`
	PaidAt       *time.Time    `json:"paid_at"`
	HandedOverAt *time.Time    `json:"handed_over_at"`
	Items        []POSSaleItem `json:"items"`
}
