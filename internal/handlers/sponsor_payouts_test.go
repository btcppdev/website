package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestSponsorPayoutInstructions(t *testing.T) {
	tmpl, err := template.New("payouts").Funcs(template.FuncMap{"sats": groupSatsCommas}).ParseFiles("../../templates/embeds/sponsor_payouts.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	amount := int64(500000)
	for _, tc := range []struct {
		name, status, distribution, want string
		tax, address, ready              bool
	}{
		{"unallocated", "", "", "Awaiting payout allocation", true, true, false},
		{"paperwork", "pending", "d", "Awaiting winner paperwork", false, true, false},
		{"address", "pending", "d", "Awaiting payment address", true, false, false},
		{"pending", "pending", "d", "Ready to pay", true, true, true},
		{"ready", "ready", "d", "Ready to pay", true, true, true},
		{"sent", "sent", "d", "No further payment is needed", true, true, false},
		{"claimed", "claimed", "d", "No further payment is needed", true, true, false},
		{"cancelled", "cancelled", "d", "This payout was cancelled", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &types.SponsorPayoutRecipient{Name: "Winner <script>alert(1)</script>", Status: tc.status,
				DistributionID: tc.distribution, AmountSats: &amount, TaxFormOnFile: tc.tax}
			if tc.address {
				r.LightningAddress, r.BitcoinAddress = "private@example.test", "bc1qprivate"
			}
			page := &SponsorDashboardPage{Organization: &types.Org{Ref: "sponsor-org"}, CSRF: "test-csrf", Payouts: []*types.SponsorPayout{{
				ProjectTitle: "Winning project", PrizeSats: 1000000, Recipients: []*types.SponsorPayoutRecipient{r},
			}}}
			var out bytes.Buffer
			if err := tmpl.ExecuteTemplate(&out, "sponsor_payouts", page); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			for _, want := range []string{tc.want, "500,000 sats", "1,000,000 sats", "Send from your wallet", "Record your payment"} {
				if !strings.Contains(html, want) {
					t.Fatalf("missing %q", want)
				}
			}
			for _, address := range []string{"private@example.test", "bc1qprivate"} {
				if strings.Contains(html, address) != tc.ready {
					t.Fatalf("incorrect address visibility for %s", tc.name)
				}
			}
			if strings.Contains(html, "<script>alert") || strings.Contains(html, "/admin/") {
				t.Fatal("unsafe sponsor payout HTML")
			}
			if strings.Contains(html, "<form") != tc.ready {
				t.Fatal("payment action offered for a blocked or completed payout")
			}
			if tc.ready && (!strings.Contains(html, "/dashboard/sponsor/sponsor-org/payouts/d/paid") || !strings.Contains(html, "test-csrf") || !strings.Contains(html, `name="ConfirmPaid"`)) {
				t.Fatal("missing scoped action, CSRF token, or payment confirmation")
			}
		})
	}
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "sponsor_payouts", &SponsorDashboardPage{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No finalized Bitcoin prize winners yet") {
		t.Fatal("missing empty state")
	}
}
