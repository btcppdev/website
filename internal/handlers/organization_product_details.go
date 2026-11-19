package handlers

import (
	"fmt"
	"sort"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

type organizationMerchKind struct {
	ID, Label, TaxCode, ShippingCategory string
}

// Explicit product choices, independent of the storefront category. Verified
// against https://docs.stripe.com/tax/tax-codes and Easyship's rates examples.
var organizationMerchKinds = []organizationMerchKind{
	{"clothing", "Everyday adult clothing — T-shirts, hoodies, etc.", "txcd_30011000", "fashion"},
	{"hats", "Hats & caps", "txcd_30060006", "fashion"},
	{"other", "Other physical merchandise", types.StripeTaxCodeTangibleGood, ""},
}

func (p *organizationShopPage) MerchandiseKinds() []organizationMerchKind {
	return organizationMerchKinds
}
func (p *organizationShopPage) MerchandiseKind() string {
	return organizationMerchKindFor(p.Product)
}
func organizationMerchKindFor(p *types.MerchProduct) string {
	if p == nil || p.StripeTaxCode == "" {
		return ""
	}
	for _, kind := range organizationMerchKinds {
		if p.StripeTaxCode == kind.TaxCode {
			return kind.ID
		}
	}
	return "saved"
}

func setOrganizationProductClassification(in *getters.OrganizationProductInput, existing *types.MerchProduct, kindID string) error {
	// Never trust provider codes submitted by an organization form. Existing
	// admin settings survive ordinary edits, including storefront recategorizing.
	in.StripeTaxCode, in.EasyshipCategory = "", ""
	if existing != nil && kindID == organizationMerchKindFor(existing) {
		in.StripeTaxCode, in.EasyshipCategory = existing.StripeTaxCode, existing.EasyshipCategory
		return nil
	}
	// A changed product kind needs its customs classification checked again.
	if existing != nil && in.HSCode == normalizeOrganizationCustomsCode(existing.HSCode) {
		in.HSCode = ""
	}
	if kindID == "" {
		return nil // An uncertain product can still be saved as a draft.
	}
	for _, kind := range organizationMerchKinds {
		if kindID == kind.ID {
			in.StripeTaxCode, in.EasyshipCategory = kind.TaxCode, kind.ShippingCategory
			return nil
		}
	}
	return fmt.Errorf("choose what you are selling, or select ‘I’m not sure yet’")
}

type organizationCountryOption struct{ Code, Name string }

var organizationManufacturingCountries = func() []organizationCountryOption {
	var countries []organizationCountryOption
	names := display.English.Regions()
	for _, region := range language.Supported.Regions() {
		if region.IsCountry() && len(region.String()) == 2 {
			countries = append(countries, organizationCountryOption{region.String(), names.Name(region)})
		}
	}
	sort.Slice(countries, func(i, j int) bool { return countries[i].Name < countries[j].Name })
	return countries
}()

func (p *organizationShopPage) ManufacturingCountries() []organizationCountryOption {
	return organizationManufacturingCountries
}

func normalizeOrganizationCustomsCode(raw string) string {
	return strings.NewReplacer(".", "", " ", "").Replace(strings.TrimSpace(raw))
}
