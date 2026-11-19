package getters

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"btcpp-web/internal/types"
	"github.com/google/uuid"
)

func TestOrganizationShopPayoutValidation(t *testing.T) {
	base := types.OrganizationShopPayoutProfile{RecipientName: "Example Ltd", TaxCountry: "DE", EntityType: "company", Method: "bank_transfer", Currency: "EUR", BankName: "Example Bank", BankCountry: "DE", BankAccount: "DE89370400440532013000", BankSWIFT: "COBADEFFXXX"}
	for _, tc := range []struct {
		name   string
		change func(*types.OrganizationShopPayoutProfile)
		valid  bool
	}{
		{"bank", func(p *types.OrganizationShopPayoutProfile) {}, true},
		{"tax details only", func(p *types.OrganizationShopPayoutProfile) { p.Method = "" }, true},
		{"missing account", func(p *types.OrganizationShopPayoutProfile) { p.BankAccount = "" }, false},
		{"missing routing", func(p *types.OrganizationShopPayoutProfile) { p.BankSWIFT = "" }, false},
		{"lightning", func(p *types.OrganizationShopPayoutProfile) {
			p.Method = "lightning"
			p.LightningAddress = "merch@example.com"
		}, true},
		{"lightning URL", func(p *types.OrganizationShopPayoutProfile) {
			p.Method = "lightning"
			p.LightningAddress = "https://example.com"
		}, false},
		{"bitcoin", func(p *types.OrganizationShopPayoutProfile) {
			p.Method = "bitcoin"
			p.BitcoinAddress = "1BoatSLRHtKNngkdXEeobR76b53LETtpyT"
		}, true},
		{"testnet", func(p *types.OrganizationShopPayoutProfile) { p.Method = "bitcoin"; p.BitcoinAddress = "tb1qexample" }, false},
		{"tax registration without type", func(p *types.OrganizationShopPayoutProfile) { p.TaxID = "DE123456789" }, false},
		{"unknown method", func(p *types.OrganizationShopPayoutProfile) { p.Method = "cash" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.change(&p)
			if err := ValidateOrganizationShopPayoutProfile(&p); (err == nil) != tc.valid {
				t.Fatalf("valid=%t error=%v", tc.valid, err)
			}
			if tc.valid && p.Method != "bank_transfer" && p.BankAccount != "" {
				t.Fatal("retained inactive bank account")
			}
		})
	}
}

func TestOrganizationShopPayoutStorage(t *testing.T) {
	app := postgresSmokeContext(t)
	app.Env = &types.EnvConfig{TaxFormEncryptionKey: "disposable-test-key"}
	c := context.Background()
	org, other := uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM organization_shop_profiles WHERE organization_id::text=ANY($1)`, []string{org, other})
		app.DB.Exec(c, `DELETE FROM organizations WHERE id::text=ANY($1)`, []string{org, other})
	})
	for _, id := range []string{org, other} {
		if _, err := app.DB.Exec(c, `INSERT INTO organizations(id,name) VALUES($1,$2)`, id, "Payout test "+id); err != nil {
			t.Fatal(err)
		}
	}
	_, err := app.DB.Exec(c, `INSERT INTO organization_shop_profiles(organization_id,data) VALUES($1,'{"tax_id":"legacy-private-tax-id","ship_from":{"Line1":"Unchanged warehouse"}}')`, org)
	if err != nil {
		t.Fatal(err)
	}
	p, err := GetOrganizationShopPayoutProfile(app, org)
	if err != nil || p.TaxID != "legacy-private-tax-id" {
		t.Fatalf("legacy prefill: %+v %v", p, err)
	}
	p.RecipientName, p.TaxCountry, p.EntityType = "Supplier Ltd", "US", "company"
	p.Method, p.Currency, p.BankName, p.BankCountry = "bank_transfer", "USD", "Example Bank", "US"
	p.BankAccount, p.BankRouting = "private-account-12345", "021000021"
	if err = SaveOrganizationShopPayoutProfile(app, org, "", *p); err != nil {
		t.Fatal(err)
	}
	if err = SaveOrganizationShopPayoutProfile(app, org, "", *p); !errors.Is(err, ErrShopEditConflict) {
		t.Fatalf("stale update: %v", err)
	}
	saved, err := GetOrganizationShopPayoutProfile(app, org)
	if err != nil || saved.Revision != 1 || saved.BankAccount != p.BankAccount || saved.TaxID != p.TaxID {
		t.Fatalf("round trip: %+v %v", saved, err)
	}
	var encrypted []byte
	if err = app.DB.QueryRow(c, `SELECT encrypted_details FROM organization_shop_payout_profiles WHERE organization_id=$1`, org).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(p.BankAccount)) || bytes.Contains(encrypted, []byte(p.TaxID)) {
		t.Fatal("sensitive data saved without encryption")
	}
	var taxID, origin string
	var revision int64
	if err = app.DB.QueryRow(c, `SELECT coalesce(data->>'tax_id',''),data->'ship_from'->>'Line1',revision FROM organization_shop_profiles WHERE organization_id=$1`, org).Scan(&taxID, &origin, &revision); err != nil || taxID != "" || origin != "Unchanged warehouse" || revision != 1 {
		t.Fatalf("legacy migration: %q %q %d %v", taxID, origin, revision, err)
	}
	if _, err = app.DB.Exec(c, `INSERT INTO organization_shop_payout_profiles(organization_id,encrypted_details) VALUES($1,$2)`, other, encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err = GetOrganizationShopPayoutProfile(app, other); err == nil {
		t.Fatal("ciphertext is not bound to the organization")
	}
	app.Env.TaxFormEncryptionKey = ""
	if err = SaveOrganizationShopPayoutProfile(app, org, "", *saved); err == nil {
		t.Fatal("missing encryption key allowed a write")
	}
	if _, err = GetOrganizationShopPayoutProfile(app, org); err == nil {
		t.Fatal("missing encryption key silently discarded saved details")
	}
}
