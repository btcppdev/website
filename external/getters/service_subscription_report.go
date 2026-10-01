package getters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

type PendingServiceSubscriptionAction struct {
	Kind              string     `json:"kind"`
	ID                string     `json:"id"`
	CheckoutID        string     `json:"checkout_id"`
	ProductCode       string     `json:"product_code"`
	State             string     `json:"state"`
	ProviderReference string     `json:"provider_reference,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	ObservedAt        *time.Time `json:"observed_at,omitempty"`
}

type PendingServiceSubscriptionActions struct {
	Service string                             `json:"service"`
	Items   []PendingServiceSubscriptionAction `json:"items"`
	Next    string                             `json:"next,omitempty"`
}

type subscriptionActionCursor struct {
	Report  string `json:"report"`
	Service string `json:"service"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
}

// PagePendingServiceSubscriptionActions reads local unresolved instructions and
// subscriptions without a stored observation of renewal cancellation. It does
// not query providers or establish that renewals or closure are complete.
func PagePendingServiceSubscriptionActions(ctx context.Context, app *config.AppContext, service, after string) (*PendingServiceSubscriptionActions, error) {
	return pagePendingServiceSubscriptionActions(ctx, app, service, after, 100)
}

func pagePendingServiceSubscriptionActions(ctx context.Context, app *config.AppContext, service, after string, limit int) (*PendingServiceSubscriptionActions, error) {
	if (service != "alerts" && service != "insider") || limit < 1 || limit > 100 {
		return nil, ErrServiceQuote
	}
	cursor := subscriptionActionCursor{}
	if after != "" {
		if len(after) > 1024 {
			return nil, ErrServiceRefundCursor
		}
		raw, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Report != "subscription-actions" || cursor.Service != service {
			return nil, ErrServiceRefundCursor
		}
		if id, err := uuid.Parse(cursor.ID); err != nil || id.String() != cursor.ID {
			return nil, ErrServiceRefundCursor
		}
		switch cursor.Kind {
		case "checkout", "increase", "decrease", "withdrawal", "cancellation", "subscription":
		default:
			return nil, ErrServiceRefundCursor
		}
	}
	rows, err := app.DB.Query(ctx, `WITH actions AS (
 SELECT 'checkout'::text kind,c.id,c.id checkout_id,'unbound'::text state,c.stripe_session_id reference,c.created_at,NULL::timestamptz observed_at FROM service_stripe_checkouts c
 WHERE c.closed_at IS NULL AND NOT EXISTS(SELECT 1 FROM service_stripe_subscriptions s WHERE s.checkout_id=c.id)
 UNION ALL
 SELECT 'increase',i.quote_id,i.checkout_id,i.state,a.invoice_id,i.created_at,NULL::timestamptz FROM alerts_stripe_increases i LEFT JOIN alerts_stripe_increase_submissions a ON a.quote_id=i.quote_id WHERE i.state IN ('pending','review')
 UNION ALL
 SELECT 'decrease',d.quote_id,d.checkout_id,d.state,a.schedule_id,d.created_at,NULL::timestamptz FROM alerts_stripe_decreases d LEFT JOIN alerts_stripe_decrease_attachments a ON a.quote_id=d.quote_id WHERE d.state IN ('pending','scheduled','review')
 UNION ALL
 SELECT 'withdrawal',w.id,d.checkout_id,w.state,s.schedule_id,w.created_at,NULL::timestamptz FROM alerts_stripe_decrease_withdrawals w JOIN alerts_stripe_decreases d ON d.quote_id=w.quote_id JOIN alerts_stripe_decrease_schedules s ON s.quote_id=w.quote_id WHERE w.state IN ('pending','review')
 UNION ALL
 SELECT 'cancellation',x.id,x.checkout_id,x.state,s.subscription_id,x.created_at,NULL::timestamptz FROM service_stripe_cancellations x JOIN service_stripe_subscriptions s ON s.checkout_id=x.checkout_id WHERE x.state IN ('pending','review')
 UNION ALL
 SELECT 'subscription',s.checkout_id,s.checkout_id,coalesce(nullif(v.snapshot->>'providerStatus',''),'unknown'),s.subscription_id,s.created_at,v.observed_at
 FROM service_stripe_subscriptions s LEFT JOIN service_stripe_subscription_state v ON v.checkout_id=s.checkout_id
 WHERE NOT coalesce(v.snapshot->>'providerStatus' IN ('canceled','incomplete_expired'),false)
 AND NOT coalesce(v.snapshot->>'providerStatus' IN ('active','trialing','past_due','unpaid','paused','incomplete')
   AND v.snapshot->>'cancelAtPeriodEnd'='true' AND
   (v.snapshot->>'cancelAt'='0' OR v.snapshot->'cancelAt'=v.snapshot->'PeriodEnd'),false)
 ) SELECT a.kind,a.id::text,a.checkout_id::text,c.product_code,a.state,coalesce(a.reference,''),a.created_at,a.observed_at FROM actions a JOIN service_stripe_checkouts c ON c.id=a.checkout_id
 WHERE c.product_family=$1 AND (a.kind,a.id::text)>($2,$3) ORDER BY a.kind,a.id::text LIMIT $4`, service, cursor.Kind, cursor.ID, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &PendingServiceSubscriptionActions{Service: service, Items: []PendingServiceSubscriptionAction{}}
	for rows.Next() {
		var item PendingServiceSubscriptionAction
		if err := rows.Scan(&item.Kind, &item.ID, &item.CheckoutID, &item.ProductCode, &item.State, &item.ProviderReference, &item.CreatedAt, &item.ObservedAt); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		raw, err := json.Marshal(subscriptionActionCursor{Report: "subscription-actions", Service: service, Kind: last.Kind, ID: last.ID})
		if err != nil {
			return nil, err
		}
		out.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
