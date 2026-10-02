package getters

import (
	"btcpp-web/internal/config"
	"fmt"
	"time"
)

type AccountPurchase struct {
	Kind, Reference, Description, Status, Provider, Amount, Currency string
	CreatedAt                                                        time.Time
}

// AccountPurchases merges account-owned orders, paid conference sales and legacy
// tickets. Tickets backed by an order appear once, as part of that order.
func AccountPurchases(app *config.AppContext, person string, page int) ([]AccountPurchase, bool, error) {
	if page < 1 || page > 1000000 {
		return nil, false, fmt.Errorf("invalid purchase page")
	}
	rows, err := app.DB.Query(app.DatabaseContext(), `WITH purchases AS (
 SELECT CASE WHEN o.checkout_kind IN ('service','digital') THEN 'service' ELSE 'order' END kind,o.public_id reference,
 coalesce((SELECT string_agg(i.product_name_snapshot, ', ' ORDER BY i.created_at,i.id) FROM shop_order_items i WHERE i.order_id=o.id),
 CASE o.checkout_kind WHEN 'ticket' THEN 'Event tickets' WHEN 'mixed' THEN 'Tickets and merchandise' WHEN 'service' THEN 'Subscription payment' ELSE 'Merchandise' END) description,
 o.status,o.payment_provider provider,(o.total_cents::numeric/100)::numeric(20,2)::text amount,upper(o.currency) currency,o.created_at
 FROM shop_orders o WHERE o.buyer_person_id=$1
 UNION ALL
 SELECT 'pos',s.id::text,c.location || ' · Conference merchandise',s.status,'lightning',s.total_sats::text,'sats',s.created_at
 FROM conference_pos_sales s JOIN conferences c ON c.id=s.conference_id WHERE s.buyer_person_id=$1 AND s.status='paid'
 UNION ALL
 SELECT 'ticket',r.ref_id,coalesce(nullif(r.item_bought,''),'Event ticket') || coalesce(' · ' || c.location,''),
 CASE WHEN r.revoked THEN 'Ticket revoked' WHEN r.amount_paid IS NULL THEN 'Ticket issued' WHEN r.amount_paid=0 THEN 'Complimentary' ELSE 'Paid' END,
 r.platform,coalesce(r.amount_paid::text,'Not recorded'),upper(r.currency),coalesce(r.registered_at,r.created_at)
 FROM registrations r LEFT JOIN conferences c ON c.id=r.conference_id
 WHERE r.person_id=$1 AND NOT EXISTS(SELECT 1 FROM shop_orders o WHERE o.buyer_person_id=$1
 AND r.checkout_id<>'' AND r.checkout_id IN (o.id::text,o.public_id,o.payment_provider_id))
 ) SELECT kind,reference,description,status,provider,amount,currency,created_at FROM purchases
 WHERE EXISTS(SELECT 1 FROM people p WHERE p.id=$1 AND NOT p.is_deleted_account)
 ORDER BY created_at DESC,kind,reference LIMIT 51 OFFSET $2`, person, (page-1)*50)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []AccountPurchase{}
	for rows.Next() {
		var p AccountPurchase
		if err := rows.Scan(&p.Kind, &p.Reference, &p.Description, &p.Status, &p.Provider, &p.Amount, &p.Currency, &p.CreatedAt); err != nil {
			return nil, false, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > 50
	if more {
		out = out[:50]
	}
	return out, more, nil
}

func (p AccountPurchase) StatusLabel() string {
	switch p.Status {
	case "paid":
		return "Paid"
	case "pending":
		return "Payment pending"
	case "cancelled":
		return "Canceled"
	case "refunded":
		return "Refunded"
	case "partially_refunded":
		return "Partially refunded"
	default:
		return p.Status
	}
}
func (p AccountPurchase) PaymentMethod() string {
	switch p.Provider {
	case "stripe":
		return "Card · Stripe"
	case "opennode":
		return "Bitcoin · OpenNode"
	case "lightning":
		return "Bitcoin · Lightning"
	default:
		return p.Provider
	}
}
