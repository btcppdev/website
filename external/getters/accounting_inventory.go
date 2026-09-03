package getters

import (
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func accountingCursorTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// ListAccountingInventoryVariants returns a stable keyset-ordered inventory
// snapshot. Inventory events participate in UpdatedAt because they change the
// computed on-hand quantity without updating the variant row itself.
func ListAccountingInventoryVariants(ctx *config.AppContext, after time.Time, afterID string, limit int) ([]*types.AccountingInventoryVariant, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH inventory AS (
			SELECT
				v.id::text AS source_id,
				v.sku,
				p.name AS product_name,
				v.label AS variant_label,
				coalesce(sum(event.quantity_delta), 0)::int AS on_hand,
				greatest(v.updated_at, coalesce(max(event.created_at), v.updated_at)) AS updated_at
			FROM merch_variants v
			JOIN merch_products p ON p.id = v.product_id
			LEFT JOIN merch_inventory_events event ON event.variant_id = v.id
			GROUP BY v.id, p.name
		)
		SELECT source_id, sku, product_name, variant_label, on_hand, updated_at
		FROM inventory
		WHERE $1::timestamptz IS NULL OR (updated_at, source_id) > ($1::timestamptz, $2)
		ORDER BY updated_at, source_id
		LIMIT $3
	`, accountingCursorTime(after), strings.TrimSpace(afterID), limit)
	if err != nil {
		return nil, fmt.Errorf("list accounting inventory variants: %w", err)
	}
	defer rows.Close()
	var out []*types.AccountingInventoryVariant
	for rows.Next() {
		var item types.AccountingInventoryVariant
		if err := rows.Scan(&item.SourceID, &item.SKU, &item.ProductName, &item.VariantLabel, &item.OnHand, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan accounting inventory variant: %w", err)
		}
		out = append(out, &item)
	}
	return out, rows.Err()
}

// ListAccountingInventorySales combines merchandise order lines with ticket
// registrations. Registrations are canonical for tickets because ordinary
// ticket purchases do not always create a shop order item; ticket-shaped shop
// lines are therefore excluded to prevent mixed checkouts from double-counting.
func ListAccountingInventorySales(ctx *config.AppContext, after time.Time, afterID string, limit int) ([]*types.AccountingInventorySale, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH accounting_sales AS (
			SELECT
				'shop_order_item:' || item.id::text AS source_id,
				coalesce(item.variant_id::text, 'sku:' || item.sku_snapshot) AS sellable_source_id,
				coalesce(item.sale_conference_id::text, '') AS event_id,
				'merch'::text AS kind,
				item.product_name_snapshot AS product_name,
				item.variant_label_snapshot AS variant_label,
				item.sku_snapshot AS sku,
				item.quantity::int AS quantity,
				item.refunded_quantity::int AS refunded_quantity,
				greatest(item.line_total_cents - coalesce(sum(refund_item.amount_cents) FILTER (WHERE refund.status = 'succeeded'), 0), 0)::bigint AS revenue_cents,
				item.line_total_cents::bigint AS gross_revenue_cents,
				upper(shop_order.currency) AS currency,
				shop_order.payment_provider_id AS checkout_id,
				lower(shop_order.payment_provider) AS payment_provider,
				shop_order.payment_provider_id,
				CASE WHEN lower(shop_order.payment_provider) IN ('cln','lightning','bitcoin','btc') THEN shop_order.payment_provider_id ELSE '' END AS payment_hash,
				shop_order.paid_at AS sold_at,
				greatest(item.updated_at, shop_order.updated_at, coalesce(max(refund.completed_at) FILTER (WHERE refund.status = 'succeeded'), item.updated_at)) AS updated_at
			FROM shop_order_items item
			JOIN shop_orders shop_order ON shop_order.id = item.order_id
			LEFT JOIN refund_items refund_item ON refund_item.order_item_id = item.id
			LEFT JOIN refunds refund ON refund.id = refund_item.refund_id
			WHERE shop_order.paid_at IS NOT NULL
				AND shop_order.status IN ('paid', 'partially_refunded', 'refunded')
				AND item.product_tag_snapshot <> 'ticket'
			GROUP BY item.id, shop_order.id

			UNION ALL

			SELECT
				'registration:' || registration.id::text AS source_id,
				'sku:ticket:' || conference.tag || ':' || coalesce(nullif(registration.type, ''), 'general') AS sellable_source_id,
				conference.id::text AS event_id,
				'ticket'::text AS kind,
				coalesce(nullif(registration.item_bought, ''), conference.description) AS product_name,
				coalesce(nullif(registration.type, ''), 'general') AS variant_label,
				'ticket:' || conference.tag || ':' || coalesce(nullif(registration.type, ''), 'general') AS sku,
				1 AS quantity,
				CASE WHEN registration.revoked THEN 1 ELSE 0 END AS refunded_quantity,
				CASE WHEN registration.revoked THEN 0 ELSE coalesce(round(registration.amount_paid * 100), 0)::bigint END AS revenue_cents,
				coalesce(round(registration.amount_paid * 100), 0)::bigint AS gross_revenue_cents,
				upper(coalesce(nullif(registration.currency, ''), 'USD')) AS currency,
				registration.checkout_id,
				lower(registration.platform) AS payment_provider,
				registration.checkout_id AS payment_provider_id,
				CASE WHEN lower(registration.platform) IN ('cln','lightning','bitcoin','btc') THEN registration.checkout_id ELSE '' END AS payment_hash,
				coalesce(registration.registered_at, registration.created_at) AS sold_at,
				registration.updated_at AS updated_at
			FROM registrations registration
			JOIN conferences conference ON conference.id = registration.conference_id
			WHERE registration.registered_at IS NOT NULL OR registration.amount_paid IS NOT NULL
		)
		SELECT source_id, sellable_source_id, event_id, kind, product_name, variant_label, sku,
			quantity, refunded_quantity, revenue_cents, gross_revenue_cents, currency,
			checkout_id, payment_provider, payment_provider_id, payment_hash, sold_at, updated_at
		FROM accounting_sales
		WHERE $1::timestamptz IS NULL OR (updated_at, source_id) > ($1::timestamptz, $2)
		ORDER BY updated_at, source_id
		LIMIT $3
	`, accountingCursorTime(after), strings.TrimSpace(afterID), limit)
	if err != nil {
		return nil, fmt.Errorf("list accounting inventory sales: %w", err)
	}
	defer rows.Close()
	var out []*types.AccountingInventorySale
	for rows.Next() {
		var item types.AccountingInventorySale
		if err := rows.Scan(
			&item.SourceID, &item.SellableSourceID, &item.EventID, &item.Kind, &item.ProductName,
			&item.VariantLabel, &item.SKU, &item.Quantity, &item.RefundedQuantity,
			&item.RevenueCents, &item.GrossRevenueCents, &item.Currency,
			&item.CheckoutID, &item.PaymentProvider, &item.PaymentProviderID, &item.PaymentHash,
			&item.SoldAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan accounting inventory sale: %w", err)
		}
		out = append(out, &item)
	}
	return out, rows.Err()
}
