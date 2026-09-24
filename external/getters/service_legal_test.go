package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
)

func TestServiceLegalCheckoutPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	product := servicebilling.AlertsProduct
	defer app.DB.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, product)
	person := insertSmokePerson(t, app, "Legal checkout fixture")
	if _, err := app.DB.Exec(ctx, `UPDATE person_emails SET email=$2,verified_at=now() WHERE person_id=$1 AND is_primary`, person, "legal-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	policy := ServiceLegalPolicy{ProductCode: product, Version: uuid.NewString(), TermsText: "Reviewed terms", PrivacyText: "Reviewed privacy", EffectiveAt: time.Now().UTC().Add(-time.Hour).Truncate(time.Second)}
	if err := RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal("same revision retry", err)
	}
	modified := policy
	modified.TermsText = "Different terms"
	if err := RegisterServiceLegalPolicy(app, modified); !errors.Is(err, ErrServiceLegalRevision) {
		t.Fatalf("revision changed: %v", err)
	}
	if p, err := CurrentServiceLegalPolicy(app, product); err != nil || p != nil {
		t.Fatalf("draft activated: %+v %v", p, err)
	}
	if err := ActivateServiceLegalPolicy(app, product, policy.Version); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"", "stale"} {
		if _, err := ReserveAlertsStripeCheckoutWithPolicy(app, person, uuid.NewString(), 2, "https://btcpp.dev", version); !errors.Is(err, ErrServiceLegalReview) {
			t.Fatalf("missing/stale acceptance: %v", err)
		}
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_checkouts WHERE person_id=$1`, person).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected acceptance reserved checkout: %d %v", count, err)
	}
	key := uuid.NewString()
	first, err := ReserveAlertsStripeCheckoutWithPolicy(app, person, key, 2, "https://btcpp.dev", policy.Version)
	if err != nil {
		t.Fatal(err)
	}
	evidence := first.Intent.LegalAcceptance
	if evidence == nil || evidence.Version != policy.Version || evidence.TermsText != policy.TermsText || evidence.PrivacyText != policy.PrivacyText || len(evidence.ContentSHA256) != 64 || evidence.AcceptedAt.IsZero() {
		t.Fatalf("missing evidence: %+v", evidence)
	}
	next := policy
	next.Version = uuid.NewString()
	next.EffectiveAt = policy.EffectiveAt.Add(time.Minute)
	if err := RegisterServiceLegalPolicy(app, next); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, product, next.Version); err != nil {
		t.Fatal(err)
	}
	retry, err := ReserveAlertsStripeCheckoutWithPolicy(app, person, key, 2, "https://btcpp.dev", policy.Version)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Intent.ID != first.Intent.ID || *retry.Intent.LegalAcceptance != *evidence {
		t.Fatal("retry rewrote acceptance")
	}
	if _, err := ReserveAlertsStripeCheckoutWithPolicy(app, person, key, 2, "https://btcpp.dev", next.Version); !errors.Is(err, ErrServiceLegalReview) {
		t.Fatalf("existing intent changed agreement: %v", err)
	}
	stored, err := AlertsStripeCheckoutForPerson(app, person, first.Intent.ID)
	if err != nil || stored.Intent.LegalAcceptance == nil || *stored.Intent.LegalAcceptance != *evidence {
		t.Fatalf("roundtrip: %+v %v", stored, err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET intent=jsonb_set(intent,'{legal_acceptance,version}','"changed"') WHERE id=$1`, first.Intent.ID); err == nil {
		t.Fatal("frozen intent allowed changed acceptance")
	}
	if err := ActivateServiceLegalPolicy(app, product, policy.Version); !errors.Is(err, ErrServiceLegalRevision) {
		t.Fatalf("policy rollback: %v", err)
	}
	future := policy
	future.Version = uuid.NewString()
	future.EffectiveAt = time.Now().Add(time.Hour)
	if err := RegisterServiceLegalPolicy(app, future); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, product, future.Version); !errors.Is(err, ErrServiceLegalReview) {
		t.Fatalf("future activation: %v", err)
	}
	if p, err := CurrentServiceLegalPolicy(app, "insider.membership.monthly"); err != nil || p != nil {
		t.Fatalf("cross-product policy: %+v %v", p, err)
	}
}
