package types

import "time"

type OrganizationShopProfile struct {
	OrganizationID string           `json:"organization_id"`
	Revision       int64            `json:"revision"`
	ShipFrom       EasyshipSettings `json:"ship_from"`
	Billing        ShopAddress      `json:"billing"`
	BillingName    string           `json:"billing_name"`
	TaxID          string           `json:"tax_id"`
	Notes          string           `json:"notes"`
}

type OrganizationShopDiscount struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	Code           string     `json:"code"`
	PercentOff     int        `json:"percent_off"`
	Active         bool       `json:"active"`
	StartsAt       *time.Time `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at"`
	MaxUses        int        `json:"max_uses"`
	Uses           int        `json:"uses"`
}
