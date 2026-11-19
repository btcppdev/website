package getters

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/payoutdocs"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

func OrganizationShopPayoutsConfigured(app *config.AppContext) bool {
	return app != nil && app.Env != nil && strings.TrimSpace(app.Env.TaxFormEncryptionKey) != ""
}

func organizationPayoutKey(app *config.AppContext, orgID string) (string, error) {
	if !OrganizationShopPayoutsConfigured(app) {
		return "", fmt.Errorf("payout details are temporarily unavailable; contact bitcoin++")
	}
	return app.Env.TaxFormEncryptionKey + ":organization-shop:" + orgID, nil
}

func GetOrganizationShopPayoutProfile(app *config.AppContext, orgID string) (*types.OrganizationShopPayoutProfile, error) {
	p := &types.OrganizationShopPayoutProfile{Currency: "USD", EntityType: "company"}
	var encrypted []byte
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT encrypted_details,revision FROM organization_shop_payout_profiles WHERE organization_id=$1`, orgID).Scan(&encrypted, &p.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		profile, err := GetOrganizationShopProfile(app, orgID)
		if err != nil {
			return nil, err
		}
		p.RecipientName, p.TaxCountry, p.TaxID = profile.BillingName, profile.Billing.Country, profile.TaxID
		if p.TaxID != "" {
			p.TaxIDType = "business_tax"
		}
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	key, err := organizationPayoutKey(app, orgID)
	if err != nil {
		return nil, err
	}
	plain, err := payoutdocs.Decrypt(key, encrypted)
	if err != nil {
		return nil, fmt.Errorf("unable to read saved payout details")
	}
	if err := json.Unmarshal(plain, p); err != nil {
		return nil, fmt.Errorf("unable to read saved payout details")
	}
	return p, nil
}

func ValidateOrganizationShopPayoutProfile(p *types.OrganizationShopPayoutProfile) error {
	if p == nil {
		return fmt.Errorf("payout details are required")
	}
	for _, value := range []*string{&p.RecipientName, &p.Currency, &p.BankName, &p.BankCountry, &p.BankAccount, &p.BankRouting, &p.BankSWIFT, &p.BitcoinAddress, &p.LightningAddress, &p.TaxCountry, &p.TaxID} {
		*value = strings.TrimSpace(*value)
		if len(*value) > 200 || strings.ContainsAny(*value, "\r\n\x00") {
			return fmt.Errorf("payment and tax fields must be single lines of at most 200 characters")
		}
	}
	p.TaxCountry = strings.ToUpper(p.TaxCountry)
	p.BankCountry = strings.ToUpper(p.BankCountry)
	p.Currency = strings.ToUpper(p.Currency)
	if p.RecipientName == "" || !shopCountryPattern.MatchString(p.TaxCountry) {
		return fmt.Errorf("enter the legal recipient name and two-letter tax residence country")
	}
	switch p.EntityType {
	case "company", "individual", "nonprofit", "other":
	default:
		return fmt.Errorf("choose the supplier’s legal entity type")
	}
	switch p.TaxIDType {
	case "", "vat", "gst", "business_tax", "other":
	default:
		return fmt.Errorf("choose a tax registration type")
	}
	if p.TaxID != "" && p.TaxIDType == "" {
		return fmt.Errorf("choose the type of tax registration number")
	}
	switch p.Method {
	case "": // Tax details can be saved before a payment method is chosen.
		p.Currency = "USD"
	case "bank_transfer":
		if p.BankName == "" || p.BankAccount == "" || !shopCountryPattern.MatchString(p.BankCountry) || !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(p.Currency) || p.Currency == "BTC" {
			return fmt.Errorf("complete the bank name, account or IBAN, bank country and payment currency")
		}
		if p.BankRouting == "" && p.BankSWIFT == "" {
			return fmt.Errorf("enter a routing code or SWIFT/BIC for the bank transfer")
		}
	case "bitcoin":
		// These are instructions for manual review, not a verified spending destination.
		if !regexp.MustCompile(`^(bc1[ac-hj-np-z02-9]{11,87}|[13][a-km-zA-HJ-NP-Z1-9]{25,34})$`).MatchString(p.BitcoinAddress) {
			return fmt.Errorf("enter a Bitcoin mainnet address")
		}
		p.Currency = "BTC"
	case "lightning":
		a, err := mail.ParseAddress(p.LightningAddress)
		if err != nil || a.Address != p.LightningAddress || !strings.Contains(strings.SplitN(a.Address, "@", 2)[1], ".") {
			return fmt.Errorf("enter a Lightning address such as name@example.com")
		}
		p.Currency = "BTC"
	default:
		return fmt.Errorf("choose a supported payment method")
	}
	if p.Method != "bank_transfer" {
		p.BankName, p.BankCountry, p.BankAccount, p.BankRouting, p.BankSWIFT = "", "", "", "", ""
	}
	if p.Method != "bitcoin" {
		p.BitcoinAddress = ""
	}
	if p.Method != "lightning" {
		p.LightningAddress = ""
	}
	return nil
}

func SaveOrganizationShopPayoutProfile(app *config.AppContext, orgID, actor string, p types.OrganizationShopPayoutProfile) error {
	if err := ValidateOrganizationShopPayoutProfile(&p); err != nil {
		return err
	}
	key, err := organizationPayoutKey(app, orgID)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(p)
	if err != nil {
		return err
	}
	encrypted, err := payoutdocs.Encrypt(key, plain)
	if err != nil {
		return fmt.Errorf("unable to protect payout details")
	}
	c := app.DatabaseContext()
	tx, err := app.DB.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	if _, err = tx.Exec(c, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "organization-payouts:"+orgID); err != nil {
		return err
	}
	var revision int64
	err = tx.QueryRow(c, `SELECT revision FROM organization_shop_payout_profiles WHERE organization_id=$1 FOR UPDATE`, orgID).Scan(&revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if revision != p.Revision {
		return ErrShopEditConflict
	}
	if _, err = tx.Exec(c, `INSERT INTO organization_shop_payout_profiles(organization_id,encrypted_details,updated_by) VALUES($1,$2,$3)
 ON CONFLICT(organization_id) DO UPDATE SET encrypted_details=excluded.encrypted_details,revision=organization_shop_payout_profiles.revision+1,updated_by=excluded.updated_by,updated_at=now()`, orgID, encrypted, nullableUUID(actor)); err != nil {
		return err
	}
	// Move the legacy tax ID into the encrypted profile. Increment the shipping
	// profile revision so an older open profile form cannot restore the old value.
	if _, err = tx.Exec(c, `UPDATE organization_shop_profiles SET data=data-'tax_id',revision=revision+1,updated_at=now() WHERE organization_id=$1 AND coalesce(data->>'tax_id','')<>''`, orgID); err != nil {
		return err
	}
	return tx.Commit(c)
}
