package getters

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsStripeIncreaseReservationPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Stripe increase reservation') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	checkout, err := ReserveAlertsStripeCheckout(app, person, "increase-reservation", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	id := checkout.Intent.ID
	now := time.Now().UTC().Truncate(time.Second)
	preview := alertsstripe.IncreasePreview{SubscriptionItemID: "si_test", PreviousSlots: 2, PaidSlots: 3, PeriodStart: now.Add(-time.Hour).Unix(), PeriodEnd: now.AddDate(0, 1, 0).Unix(), ProrationDate: now.Unix(), TotalCents: 195, TaxCents: 15, ExpiresAt: now.Add(10 * time.Minute)}
	b, _ := json.Marshal(alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, CustomerID: "cus_" + id, PriceID: "price_" + id, ProductID: "prod_" + id, PaidSlots: 2})
	s, _ := json.Marshal(alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: preview.PeriodStart, PeriodEnd: preview.PeriodEnd})
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, "sub_"+id, b); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot,observed_at) VALUES($1,1,$2,now())`, id, s); err != nil {
		t.Fatal(err)
	}
	quote, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	expired := preview
	expired.ExpiresAt = now.Add(-time.Second)
	if _, err = SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, expired); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("expired preview saved", err)
	}
	// Seed a quote that was valid when issued, but expired before confirmation.
	expired.ProrationDate = now.Add(-11 * time.Minute).Unix()
	expiredRaw, _ := json.Marshal(expired)
	var expiredID string
	if err = app.DB.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,created_at,expires_at) VALUES($1,1,$2,$3,$4) RETURNING id::text`, id, expiredRaw, now.Add(-11*time.Minute), expired.ExpiresAt).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, expiredID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("expired quote confirmed", err)
	}
	if _, err = AlertsStripeIncreaseQuoteForPerson(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign quote readable", err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign quote confirmable", err)
	}
	if _, err = SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 2, preview); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale revision", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_quotes SET preview='{}' WHERE id=$1`, quote.ID); err == nil {
		t.Fatal("quote mutable")
	}
	policy := ServiceLegalPolicy{ProductCode: servicebilling.AlertsProduct, Version: uuid.NewString(), TermsText: "Upgrade terms", PrivacyText: "Upgrade privacy", EffectiveAt: now.Add(-time.Hour)}
	if err := RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	defer app.DB.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, policy.ProductCode)
	for _, version := range []string{"", "stale"} {
		if _, err := ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, quote.ID, version); !errors.Is(err, ErrServiceLegalReview) {
			t.Fatal("unaccepted upgrade reserved", err)
		}
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_increases WHERE quote_id=$1`, quote.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected agreement authorized upgrade", count, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, e := ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, quote.ID, policy.Version)
			if e != nil || status != "pending" {
				t.Errorf("reservation %s %v", status, e)
			}
		}()
	}
	wg.Wait()
	if handled, err := expireUnsubmittedStoppedAlertsIncrease(ctx, app, person, quote.ID); err != nil || handled {
		t.Fatal("unsubmitted upgrade released before closure", handled, err)
	}
	var raw []byte
	if err := app.DB.QueryRow(ctx, `SELECT legal_acceptance FROM alerts_stripe_increases WHERE quote_id=$1`, quote.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var evidence servicebilling.LegalAcceptance
	if err := json.Unmarshal(raw, &evidence); err != nil || evidence.Version != policy.Version || evidence.TermsText != policy.TermsText || evidence.PrivacyText != policy.PrivacyText || len(evidence.ContentSHA256) != 64 || evidence.AcceptedAt.IsZero() {
		t.Fatal("missing acceptance evidence", evidence, err)
	}
	next := policy
	next.Version = uuid.NewString()
	next.EffectiveAt = now.Add(-time.Minute)
	if err := RegisterServiceLegalPolicy(app, next); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, next.ProductCode, next.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, quote.ID, next.Version); !errors.Is(err, ErrServiceLegalReview) {
		t.Fatal("retry rewrote agreement", err)
	}
	if _, err := ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, quote.ID, policy.Version); err != nil {
		t.Fatal("original retry rejected", err)
	}
	var retryRaw []byte
	if err := app.DB.QueryRow(ctx, `SELECT legal_acceptance FROM alerts_stripe_increases WHERE quote_id=$1`, quote.ID).Scan(&retryRaw); err != nil || string(raw) != string(retryRaw) {
		t.Fatal("retry changed evidence", err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET legal_acceptance=NULL WHERE quote_id=$1`, quote.ID); err == nil {
		t.Fatal("acceptance mutable")
	}
	if resumed, e := PrepareAlertsStripeIncrease(ctx, app, person, id, 4, alertsstripe.Client{}); e != nil || resumed == nil || resumed.ID != quote.ID || resumed.Preview.PaidSlots != 3 {
		t.Fatal("pending upgrade replaced by new cart", resumed, e)
	}
	claims := make(chan *alertsIncreaseSubmissionClaim, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, e := claimAlertsStripeIncreaseSubmission(ctx, app, person, quote.ID)
			if e != nil && !errors.Is(e, alertsstripe.ErrReconcile) {
				t.Error(e)
			}
			claims <- claim
		}()
	}
	wg.Wait()
	close(claims)
	winners := 0
	for claim := range claims {
		if claim != nil {
			winners++
			if claim.Attempt != 1 || claim.Intent.ID != quote.ID || claim.Binding.PaidSlots != 2 {
				t.Fatal(claim)
			}
		}
	}
	if winners != 1 {
		t.Fatal("concurrent submission claims", winners)
	}
	if _, err = claimAlertsStripeIncreaseSubmission(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign submission claim", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET lease_until=now()-interval '1 second' WHERE quote_id=$1`, quote.ID); err != nil {
		t.Fatal(err)
	}
	retry, e := claimAlertsStripeIncreaseSubmission(ctx, app, person, quote.ID)
	if e != nil || retry == nil || retry.Attempt != 2 || retry.Intent.ID != quote.ID {
		t.Fatal("stable retry", retry, e)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id=$2 WHERE quote_id=$1`, quote.ID, "in_claim_"+quote.ID); err != nil {
		t.Fatal(err)
	}
	if invoice, e := SubmitAlertsStripeIncrease(ctx, app, person, quote.ID, alertsstripe.Client{}); e != nil || invoice != "in_claim_"+quote.ID {
		t.Fatal("saved reference retry used provider", invoice, e)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id='in_other' WHERE quote_id=$1`, quote.ID); err == nil {
		t.Fatal("invoice reference mutable")
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("overlapping upgrade", err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 1, preview.PeriodEnd); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("cancellation during upgrade", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET state='expired' WHERE quote_id=$1`, quote.ID); err != nil {
		t.Fatal(err)
	}
	if status, e := ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, quote.ID, policy.Version); e != nil || status != "expired" {
		t.Fatal("resolved retry", status, e)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET state='pending' WHERE quote_id=$1`, quote.ID); err == nil {
		t.Fatal("resolved instruction reopened")
	}
	// An expired quote with no submission can be released without provider I/O.
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increases(quote_id,checkout_id) VALUES($1,$2)`, expiredID, id); err != nil {
		t.Fatal(err)
	}
	if _, err = claimAlertsStripeIncreaseSubmission(ctx, app, person, expiredID); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("expired submission claimed", err)
	}
	var expiredState string
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, expiredID).Scan(&expiredState); err != nil || expiredState != "expired" {
		t.Fatal(expiredState, err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=2 WHERE checkout_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale confirmation", err)
	}
	fresh, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 2, preview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 2, preview.PeriodEnd); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, fresh.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("upgrade during cancellation", err)
	}
	grants, err := ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 0 {
		t.Fatal("reservation granted access", grants, err)
	}
	// Expiry cannot release an ambiguous attempted operation. Seed persisted
	// crash evidence directly to exercise recovery after the review window.
	var ambiguousID string
	if err = app.DB.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,created_at,expires_at) VALUES($1,2,$2,$3,$4) RETURNING id::text`, id, expiredRaw, now.Add(-11*time.Minute), expired.ExpiresAt).Scan(&ambiguousID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increases(quote_id,checkout_id) VALUES($1,$2)`, ambiguousID, id); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increase_submissions(quote_id,lease_until) VALUES($1,now()-interval '1 second')`, ambiguousID); err != nil {
		t.Fatal(err)
	}
	if _, err = claimAlertsStripeIncreaseSubmission(ctx, app, person, ambiguousID); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("expired attempted claim", err)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, ambiguousID).Scan(&expiredState); err != nil || expiredState != "pending" {
		t.Fatal("ambiguous operation released", expiredState, err)
	}
	price := &stripe.Price{ID: "price_" + id, Currency: "usd", UnitAmount: 200, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "month", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_" + id, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	meta := map[string]string{"account_id": person, "checkout_intent": id, "product": "alerts.repository-slots"}
	invoice := &stripe.Invoice{ID: "in_void_" + ambiguousID, Status: "void", Currency: "usd", Customer: &stripe.Customer{ID: "cus_" + id}, BillingReason: "subscription_update", Total: 195, AmountDue: 195, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, TotalTaxes: []*stripe.InvoiceTotalTax{{Amount: 15, TaxBehavior: "inclusive"}}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: &stripe.Subscription{ID: "sub_" + id}, Metadata: meta}}, Lines: &stripe.InvoiceLineItemList{}}
	for _, line := range []struct{ quantity, amount int64 }{{2, -200}, {3, 395}} {
		invoice.Lines.Data = append(invoice.Lines.Data, &stripe.InvoiceLineItem{Currency: "usd", Quantity: line.quantity, Amount: line.amount, Period: &stripe.Period{Start: expired.ProrationDate, End: expired.PeriodEnd}, Parent: &stripe.InvoiceLineItemParent{SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: "sub_" + id, SubscriptionItem: "si_test", Proration: true}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: price.Product.ID}}})
	}
	sub := &stripe.Subscription{ID: "sub_" + id, Customer: invoice.Customer, Metadata: meta, Status: "active", Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{ID: "si_test", Quantity: 2, Price: price, CurrentPeriodStart: expired.PeriodStart, CurrentPeriodEnd: expired.PeriodEnd}}}}
	client := alertsstripe.Client{Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return invoice, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }, Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id=$2,lease_until=now()+interval '1 minute' WHERE quote_id=$1`, ambiguousID, invoice.ID); err != nil {
		t.Fatal(err)
	}
	if err = ResolveVoidedAlertsStripeIncrease(ctx, app, person, ambiguousID, client); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("released live submission", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET lease_until=now()-interval '1 second' WHERE quote_id=$1`, ambiguousID); err != nil {
		t.Fatal(err)
	}
	sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{}
	if err = ResolveVoidedAlertsStripeIncrease(ctx, app, person, ambiguousID, client); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("released pending provider update", err)
	}
	sub.PendingUpdate = nil
	for i := 0; i < 2; i++ {
		if err = ResolveVoidedAlertsStripeIncrease(ctx, app, person, ambiguousID, client); err != nil {
			t.Fatal(err)
		}
	}
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, ambiguousID).Scan(&expiredState); err != nil || expiredState != "expired" {
		t.Fatal(expiredState, err)
	}
	if _, err = app.DB.Exec(ctx, `DELETE FROM alerts_stripe_increase_expirations WHERE quote_id=$1`, ambiguousID); err == nil {
		t.Fatal("expiry evidence deleted")
	}
	// Closure cancels an open, attempted upgrade only after its submission lease
	// expires, and persists resolution after a lost provider response.
	var closureID string
	if err = app.DB.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,created_at,expires_at) VALUES($1,2,$2,$3,$4) RETURNING id::text`, id, expiredRaw, now.Add(-11*time.Minute), expired.ExpiresAt).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	invoice.ID = "in_closure_" + closureID
	invoice.Status = "open"
	invoice.AmountRemaining = invoice.Total
	sub.LatestInvoice = &stripe.Invoice{ID: invoice.ID}
	sub.CollectionMethod = "charge_automatically"
	sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{SubscriptionItems: []*stripe.SubscriptionItem{{ID: expired.SubscriptionItemID, Quantity: expired.PaidSlots, Price: price}}}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increases(quote_id,checkout_id) VALUES($1,$2)`, closureID, id); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increase_submissions(quote_id,invoice_id,lease_until) VALUES($1,$2,now()+interval '1 minute')`, closureID, invoice.ID); err != nil {
		t.Fatal(err)
	}
	voidCalls := 0
	client.VoidInvoice = func(ref string, p *stripe.InvoiceVoidInvoiceParams) (*stripe.Invoice, error) {
		voidCalls++
		if ref != invoice.ID || p.IdempotencyKey == nil || *p.IdempotencyKey != "alerts-closure-void-increase:"+closureID {
			t.Fatal("incorrect void")
		}
		invoice.Status = "void"
		invoice.AmountRemaining = 0
		sub.PendingUpdate = nil
		return nil, errors.New("lost void response")
	}
	if _, err = RecoverAlertsStripeIncrease(ctx, app, person, closureID, client); err != nil || voidCalls != 0 {
		t.Fatal("active sales voided invoice", err, voidCalls)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
		t.Fatal(err)
	}
	// Even an expired quote must remain pending during ordinary operation;
	// this helper is only authorized by the permanent sales-stop record.
	if handled, err := expireUnsubmittedStoppedAlertsIncrease(ctx, app, person, closureID); err != nil || handled {
		t.Fatal("active sales release", handled, err)
	}
	if err = StopServiceSales(ctx, app, person, "alerts", "Permanent closure fixture"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `TRUNCATE service_closure_plans,service_sales_closures`); err != nil {
			t.Error(err)
		}
	})
	if handled, err := expireUnsubmittedStoppedAlertsIncrease(ctx, app, person, closureID); err != nil || handled {
		t.Fatal("attempted upgrade released locally", handled, err)
	}
	if _, err = RecoverAlertsStripeIncrease(ctx, app, person, closureID, client); !errors.Is(err, alertsstripe.ErrReconcile) || voidCalls != 0 {
		t.Fatal("live submission voided", err, voidCalls)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET lease_until=now()-interval '1 second' WHERE quote_id=$1`, closureID); err != nil {
		t.Fatal(err)
	}
	if _, err = RecoverAlertsStripeIncrease(ctx, app, person, closureID, client); err == nil || voidCalls != 1 {
		t.Fatal("lost response accepted", err, voidCalls)
	}
	var resolved string
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, closureID).Scan(&resolved); err != nil || resolved != "pending" {
		t.Fatal("premature local resolution", resolved, err)
	}
	if _, err = RecoverAlertsStripeIncrease(ctx, app, person, closureID, client); err != nil || voidCalls != 1 {
		t.Fatal("void recovery", err, voidCalls)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, closureID).Scan(&resolved); err != nil || resolved != "expired" {
		t.Fatal("missing local resolution", resolved, err)
	}

	// A reserved instruction that never reached the submission claim is safe to
	// release without provider I/O. Concurrent recovery increments revision once.
	var unsubmitted string
	if err = app.DB.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,created_at,expires_at) VALUES($1,2,$2,$3,$4) RETURNING id::text`, id, expiredRaw, now.Add(-11*time.Minute), expired.ExpiresAt).Scan(&unsubmitted); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increases(quote_id,checkout_id) VALUES($1,$2)`, unsubmitted, id); err != nil {
		t.Fatal(err)
	}
	if err = ProcessAlertsStripeIncrease(ctx, app, uuid.NewString(), unsubmitted, alertsstripe.Client{}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign reservation released", err)
	}
	var revisionBefore, revisionAfter int64
	var observedBefore, observedAfter time.Time
	if err = app.DB.QueryRow(ctx, `SELECT revision,observed_at FROM alerts_stripe_subscription_state WHERE checkout_id=$1`, id).Scan(&revisionBefore, &observedBefore); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ProcessAlertsStripeIncrease(ctx, app, person, unsubmitted, alertsstripe.Client{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, unsubmitted).Scan(&resolved); err != nil || resolved != "expired" {
		t.Fatal("unsubmitted closure", resolved, err)
	}
	if err = app.DB.QueryRow(ctx, `SELECT revision,observed_at FROM alerts_stripe_subscription_state WHERE checkout_id=$1`, id).Scan(&revisionAfter, &observedAfter); err != nil || revisionAfter != revisionBefore+1 || !observedAfter.Equal(observedBefore) {
		t.Fatal("revision/provider timestamp", revisionBefore, revisionAfter, observedBefore, observedAfter, err)
	}
	if _, err = claimAlertsStripeIncreaseSubmission(ctx, app, person, unsubmitted); !errors.Is(err, ErrServiceSalesClosed) {
		t.Fatal("closed reservation resubmitted", err)
	}
	var providerEvidence int
	if err = app.DB.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_increase_expirations WHERE quote_id=$1`, unsubmitted).Scan(&providerEvidence); err != nil || providerEvidence != 0 {
		t.Fatal("fabricated provider evidence", providerEvidence, err)
	}

}
