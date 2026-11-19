package types

import "time"

// The fee is applied to merchandise after discounts, excluding tax and shipping.
const OrganizationShopFeeBPS = 1500

// OrganizationShopSplit rounds the fee once per order line to the nearest cent
// (or satoshi for event sales). The supplier receives the exact remainder.
func OrganizationShopSplit(amount uint) (fee, supplier uint) {
	fee = amount/10000*OrganizationShopFeeBPS + (amount%10000*OrganizationShopFeeBPS+5000)/10000
	return fee, amount - fee
}

type OrganizationShopPayoutProfile struct {
	Revision         int64  `json:"-"`
	Method           string `json:"method"`
	RecipientName    string `json:"recipient_name"`
	Currency         string `json:"currency"`
	BankName         string `json:"bank_name"`
	BankCountry      string `json:"bank_country"`
	BankAccount      string `json:"bank_account"`
	BankRouting      string `json:"bank_routing"`
	BankSWIFT        string `json:"bank_swift"`
	BitcoinAddress   string `json:"bitcoin_address"`
	LightningAddress string `json:"lightning_address"`
	TaxCountry       string `json:"tax_country"`
	EntityType       string `json:"entity_type"`
	TaxID            string `json:"tax_id"`
	TaxIDType        string `json:"tax_id_type"`
}

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
