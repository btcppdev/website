package getters

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrServiceStripeCheckoutConflict = errors.New("a service checkout or subscription already needs attention")

type ServiceStripeCheckout struct {
	Intent    servicestripe.CheckoutIntent
	Reference string
	Closed    bool
}

// ReserveServiceStripeCheckout freezes server-authorized Insider checkout terms.
// The caller must authorize the product and return origin. Identity, email and
// creation time are supplied here, never copied from browser checkout input.
// Alerts retains its existing intent adapter until its lifecycle is migrated.
func ReserveServiceStripeCheckout(app *config.AppContext, person, key string, terms servicestripe.CheckoutIntent) (*ServiceStripeCheckout, error) {
	plan, planErr := servicebilling.LookupProductPlan(terms.ProductCode)
	if _, err := uuid.Parse(person); err != nil || planErr != nil || plan.Family != "insider" || len(key) < 16 || len(key) > 128 {
		return nil, ErrServiceQuote
	}
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return nil, err
	}
	out := &ServiceStripeCheckout{}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT intent,coalesce(stripe_session_id,''),closed_at IS NOT NULL FROM service_stripe_checkouts WHERE person_id=$1 AND product_family=$3 AND (request_key=$2 OR closed_at IS NULL) ORDER BY (request_key=$2) DESC LIMIT 1`, person, key, plan.Family).Scan(&raw, &out.Reference, &out.Closed)
	if err == nil {
		if err = json.Unmarshal(raw, &out.Intent); err != nil {
			return nil, err
		}
		saved := out.Intent
		if out.Closed || saved.PersonID != person || saved.ProductCode != terms.ProductCode || saved.Quantity != terms.Quantity || saved.TaxCode != terms.TaxCode || saved.ReturnOrigin != terms.ReturnOrigin || saved.SuccessURL != terms.SuccessURL || saved.CancelURL != terms.CancelURL {
			return nil, ErrServiceStripeCheckoutConflict
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var occupied bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id WHERE p.person_id=$1 AND p.quote_snapshot->>'product_code' IN ('insider.membership','insider.membership.monthly','insider.membership.annual') AND ((o.status='pending' AND (o.checkout_expires_at>now() OR EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=p.id))) OR (o.status='paid' AND (p.quote_snapshot->>'period_end')::timestamptz>now())))`, person).Scan(&occupied)
	if err != nil {
		return nil, err
	}
	if occupied {
		return nil, ErrServiceStripeCheckoutConflict
	}
	terms.ID, terms.PersonID, terms.CreatedAt = uuid.NewString(), person, time.Now().UTC().Truncate(time.Second)
	err = tx.QueryRow(ctx, `SELECT email::text FROM person_emails WHERE person_id=$1 AND verified_at IS NOT NULL ORDER BY is_primary DESC,email LIMIT 1 FOR SHARE`, person).Scan(&terms.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServiceBuyer
	}
	if err != nil {
		return nil, err
	}
	if _, err = servicestripe.CheckoutParams(ctx, terms, time.Now().UTC()); err != nil {
		return nil, err
	}
	out.Intent = terms
	raw, err = json.Marshal(terms)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_stripe_checkouts(id,person_id,request_key,product_code,intent) VALUES($1,$2,$3,$4,$5)`, terms.ID, person, key, terms.ProductCode, raw)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// ServiceStripeCheckoutForPerson scopes both ownership and product identity.
func ServiceStripeCheckoutForPerson(app *config.AppContext, person, id, product string) (*ServiceStripeCheckout, error) {
	plan, planErr := servicebilling.LookupProductPlan(product)
	if _, err := uuid.Parse(id); err != nil || planErr != nil || plan.Family != "insider" {
		return nil, pgx.ErrNoRows
	}
	out := &ServiceStripeCheckout{}
	var raw []byte
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT intent,coalesce(stripe_session_id,''),closed_at IS NOT NULL FROM service_stripe_checkouts WHERE person_id=$1 AND id=$2 AND product_code=$3`, person, id, product).Scan(&raw, &out.Reference, &out.Closed)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.Intent); err != nil {
		return nil, err
	}
	if out.Intent.PersonID != person || out.Intent.ID != id || out.Intent.ProductCode != product {
		return nil, servicestripe.ErrCheckout
	}
	return out, nil
}

func OpenServiceStripeCheckout(ctx context.Context, app *config.AppContext, person, id, product string, client servicestripe.CheckoutClient) (string, error) {
	stored, err := ServiceStripeCheckoutForPerson(app, person, id, product)
	if err != nil {
		return "", err
	}
	if stored.Closed {
		return "", ErrServiceStripeCheckoutConflict
	}
	reference, destination, providerErr := client.Open(ctx, stored.Intent, stored.Reference, time.Now().UTC())
	if reference != "" {
		result, err := app.DB.Exec(app.DatabaseContext(), `UPDATE service_stripe_checkouts SET stripe_session_id=$4 WHERE person_id=$1 AND id=$2 AND product_code=$3 AND closed_at IS NULL AND (stripe_session_id IS NULL OR stripe_session_id=$4)`, person, id, product, reference)
		if err != nil {
			return "", err
		}
		if result.RowsAffected() != 1 {
			return "", ErrServiceStripeCheckoutConflict
		}
	}
	if providerErr != nil {
		return "", providerErr
	}
	return destination, nil
}
