package getters

import (
	"encoding/json"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
)

// AccountCardSubscription is a saved billing observation, not an access grant.
// Provider identifiers and payment details are deliberately not exposed.
type AccountCardSubscription struct {
	Service    string
	State      servicestripe.SubscriptionState
	ObservedAt *time.Time
}

func AccountCardSubscriptions(app *config.AppContext, personID string) ([]AccountCardSubscription, error) {
	rows, err := app.DB.Query(app.DatabaseContext(), `SELECT DISTINCT ON (c.product_family)
 c.product_family,coalesce(v.snapshot,'{}'::jsonb),v.observed_at
 FROM service_stripe_checkouts c
 JOIN service_stripe_subscriptions s ON s.checkout_id=c.id
 LEFT JOIN service_stripe_subscription_state v ON v.checkout_id=c.id
 WHERE c.person_id=$1 AND EXISTS(SELECT 1 FROM people p WHERE p.id=c.person_id AND NOT p.is_deleted_account)
 ORDER BY c.product_family,c.created_at DESC,c.id DESC`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccountCardSubscription{}
	for rows.Next() {
		var item AccountCardSubscription
		var raw []byte
		if err := rows.Scan(&item.Service, &raw, &item.ObservedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.State); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
