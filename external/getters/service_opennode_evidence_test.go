package getters

import (
	"encoding/json"
	"testing"
)

func TestOpenNodeBitcoinEvidenceValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"price only", `{"price":150077}`, true},
		{"fee and overpayment", `{"price":150077,"fee":1500,"missing_amt":0,"overpaid_by":10}`, true},
		{"missing price", `{}`, false},
		{"negative", `{"price":-1}`, false},
		{"fractional", `{"price":1.5}`, false},
		{"overflow", `{"price":9223372036854775808}`, false},
		{"too much", `{"price":2100000000000001}`, false},
		{"underpaid", `{"price":150077,"missing_amt":1}`, false},
		{"negative fee", `{"price":150077,"fee":-1}`, false},
		{"fee exceeds price", `{"price":150077,"fee":150078}`, false},
		{"negative overpayment", `{"price":150077,"overpaid_by":-1}`, false},
		{"total exceeds supply", `{"price":2100000000000000,"overpaid_by":1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var charge OpenNodeServiceCharge
			err := json.Unmarshal([]byte(tc.body), &charge)
			evidence := &OpenNodeBitcoinEvidence{PriceSatoshis: charge.PriceSatoshis, FeeSatoshis: charge.FeeSatoshis, OverpaidSatoshis: charge.OverpaidSatoshis, MissingSatoshis: charge.MissingSatoshis}
			if valid := err == nil && evidence.valid(); valid != tc.valid {
				t.Fatalf("valid=%v want %v: %+v, %v", valid, tc.valid, evidence, err)
			}
			if tc.name == "price only" && (evidence.FeeSatoshis != nil || evidence.OverpaidSatoshis != nil || evidence.MissingSatoshis != nil) {
				t.Fatal("missing fields converted into evidence of zero")
			}
		})
	}
	var absent *OpenNodeBitcoinEvidence
	if absent.valid() {
		t.Fatal("absent evidence accepted")
	}
}
