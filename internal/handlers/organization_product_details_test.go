package handlers

import (
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
)

func TestOrganizationShopClassificationPreservesAdminSettings(t *testing.T) {
	custom := &types.MerchProduct{StripeTaxCode: "txcd_30011200", EasyshipCategory: "fashion", HSCode: "61091000"}
	in := getters.OrganizationProductInput{MerchProductInput: getters.MerchProductInput{StripeTaxCode: "txcd_00000000", EasyshipCategory: "tampered", HSCode: custom.HSCode}}
	if err := setOrganizationProductClassification(&in, custom, "saved"); err != nil || in.StripeTaxCode != custom.StripeTaxCode || in.EasyshipCategory != custom.EasyshipCategory || in.HSCode != custom.HSCode {
		t.Fatal("overwrote existing admin classification", in, err)
	}
	if err := setOrganizationProductClassification(&in, custom, "hats"); err != nil || in.StripeTaxCode != "txcd_30060006" || in.HSCode != "" {
		t.Fatal("changed item retained an unrelated customs code", in, err)
	}
	if err := setOrganizationProductClassification(&in, nil, "saved"); err == nil {
		t.Fatal("new item claimed a nonexistent saved classification")
	}
	if got := normalizeOrganizationCustomsCode(" 6109.10.00 "); got != "61091000" {
		t.Fatal("supplier code formatting", got)
	}
}
