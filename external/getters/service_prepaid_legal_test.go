package getters

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
)

func TestPrepaidLegalAcceptancePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	person := insertSmokePerson(t, app, "prepaid-policy")
	if _, err := app.DB.Exec(ctx, `UPDATE person_emails SET verified_at=now() WHERE person_id=$1`, person); err != nil {
		t.Fatal(err)
	}
	product := servicebilling.AlertsProduct
	defer app.DB.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, product)
	now := time.Now().UTC().Truncate(time.Second)
	end := now.AddDate(1, 0, 0)
	expires := now.Add(time.Hour)
	policy := ServiceLegalPolicy{ProductCode: product, Version: uuid.NewString(), TermsText: "Prepaid terms", PrivacyText: "Privacy", EffectiveAt: now.Add(-time.Hour)}
	if err := RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, product, policy.Version); err != nil {
		t.Fatal(err)
	}
	revision, err := AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	q := ServiceQuote{AccountRevision: revision, BillingMonths: 12, ProductCode: product, Name: "Alerts", Provider: "opennode", Quantity: 2, UnitCents: 2400, TotalCents: 4800, TaxCents: 200, PeriodStart: &now, PeriodEnd: &end, ExpiresAt: expires}
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + person, SalesTaxAmountCents: 200, DestinationCountry: "US", RawResponse: `{"amount_total":4800}`, ExpiresAt: &expires}
	stored, err := SaveAlertsCheckoutQuote(app, person, q, tax)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"", "stale"} {
		if _, err := ConfirmServiceCheckoutQuoteWithPolicy(app, person, stored.ID, version); !errors.Is(err, ErrServiceLegalReview) {
			t.Fatalf("missing/stale acceptance: %v", err)
		}
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_purchase_requests WHERE person_id=$1`, person).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected order: %d %v", count, err)
	}
	purchase, err := ConfirmServiceCheckoutQuoteWithPolicy(app, person, stored.ID, policy.Version)
	if err != nil {
		t.Fatal(err)
	}
	load := func() ServiceQuote {
		t.Helper()
		var raw []byte
		if err := app.DB.QueryRow(ctx, `SELECT quote_snapshot FROM service_purchase_requests WHERE id=$1`, purchase.PurchaseID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var got ServiceQuote
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := load()
	if first.LegalAcceptance == nil || first.LegalAcceptance.TermsText != policy.TermsText || first.LegalAcceptance.Version != policy.Version || first.LegalAcceptance.AcceptedAt.IsZero() {
		t.Fatalf("missing evidence: %+v", first.LegalAcceptance)
	}
	next := policy
	next.Version = uuid.NewString()
	next.EffectiveAt = now.Add(-time.Minute)
	if err := RegisterServiceLegalPolicy(app, next); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, product, next.Version); err != nil {
		t.Fatal(err)
	}
	retry, err := ConfirmServiceCheckoutQuoteWithPolicy(app, person, stored.ID, policy.Version)
	if err != nil || retry.OrderID != purchase.OrderID {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	again := load()
	if *first.LegalAcceptance != *again.LegalAcceptance {
		t.Fatal("retry changed agreement")
	}
	if _, err := ConfirmServiceCheckoutQuoteWithPolicy(app, person, stored.ID, next.Version); !errors.Is(err, ErrServiceLegalReview) {
		t.Fatalf("agreement replaced: %v", err)
	}
	forged := q
	forged.LegalAcceptance = first.LegalAcceptance
	if _, err := CreateTaxedServicePurchaseWithPolicy(app, person, uuid.NewString(), forged, tax, next.Version); !errors.Is(err, ErrServiceLegalReview) {
		t.Fatalf("caller supplied evidence: %v", err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_purchase_requests SET quote_snapshot=jsonb_set(quote_snapshot,'{legal_acceptance,version}','"changed"') WHERE id=$1`, purchase.PurchaseID); err == nil {
		t.Fatal("immutable order policy changed")
	}
}
