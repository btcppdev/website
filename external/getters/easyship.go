package getters

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

func GetLatestEasyshipRateQuote(ctx *config.AppContext, orderID string) (*types.ShippingRateQuote, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var quote types.ShippingRateQuote
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, order_id::text, provider, provider_quote_id, courier_service_id,
			destination_country, destination_region, destination_postal_code,
			courier_name, service_name, amount_cents, currency, estimated_min_days,
			estimated_max_days, raw_response::text, expires_at, created_at
		FROM shipping_rate_quotes
		WHERE order_id = $1::uuid AND provider = 'easyship'
		ORDER BY created_at DESC
		LIMIT 1
	`, strings.TrimSpace(orderID)).Scan(
		&quote.ID, &quote.OrderID, &quote.Provider, &quote.ProviderQuoteID,
		&quote.CourierServiceID, &quote.DestinationCountry, &quote.DestinationRegion,
		&quote.DestinationPostalCode, &quote.CourierName, &quote.ServiceName,
		&quote.AmountCents, &quote.Currency, &quote.EstimatedMinDays,
		&quote.EstimatedMaxDays, &quote.RawResponse, &quote.ExpiresAt, &quote.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("order has no Easyship shipping rate")
	}
	if err != nil {
		return nil, fmt.Errorf("get Easyship rate quote: %w", err)
	}
	if quote.CourierServiceID == "" {
		quote.CourierServiceID = quote.ProviderQuoteID
	}
	return &quote, nil
}

func PrepareEasyshipShipment(ctx *config.AppContext, orderID, actorEmail string, quote *types.ShippingRateQuote) (*types.Shipment, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if quote == nil || strings.TrimSpace(quote.CourierServiceID) == "" {
		return nil, fmt.Errorf("an Easyship courier service is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var status string
	err = tx.QueryRow(ctx.DatabaseContext(), `
		SELECT status FROM shop_orders WHERE id = $1::uuid FOR UPDATE
	`, strings.TrimSpace(orderID)).Scan(&status)
	if err != nil {
		return nil, fmt.Errorf("load order for Easyship shipment: %w", err)
	}
	if status != types.ShopOrderStatusPaid && status != types.ShopOrderStatusPartiallyRefunded {
		return nil, fmt.Errorf("only paid orders can create an Easyship shipment")
	}
	var shippable uint
	err = tx.QueryRow(ctx.DatabaseContext(), `
		SELECT coalesce(sum(greatest(quantity - fulfilled_quantity - refunded_quantity, 0))
			FILTER (WHERE fulfillment_method = 'ship' AND status NOT IN ('cancelled', 'refunded', 'fulfilled')), 0)
		FROM shop_order_items
		WHERE order_id = $1::uuid
	`, strings.TrimSpace(orderID)).Scan(&shippable)
	if err != nil {
		return nil, fmt.Errorf("load shippable order items: %w", err)
	}
	if shippable == 0 {
		return nil, fmt.Errorf("order has no unfulfilled items to ship")
	}
	var shipment types.Shipment
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO shipments (
			order_id, provider, courier_service_id, courier_name, service_name,
			status, label_state, delivery_state, raw_response
		) VALUES (
			$1::uuid, 'easyship', $2, $3, $4, 'pending', 'not_created', 'not_created', '{}'::jsonb
		)
		ON CONFLICT (order_id) WHERE provider = 'easyship' AND status <> 'cancelled'
		DO UPDATE SET
			courier_service_id = CASE WHEN shipments.provider_shipment_id = '' THEN EXCLUDED.courier_service_id ELSE shipments.courier_service_id END,
			courier_name = coalesce(NULLIF(shipments.courier_name, ''), EXCLUDED.courier_name),
			service_name = coalesce(NULLIF(shipments.service_name, ''), EXCLUDED.service_name),
			last_error = CASE WHEN shipments.provider_shipment_id = '' THEN '' ELSE shipments.last_error END
		RETURNING id::text, order_id::text, provider, provider_shipment_id, provider_label_id,
			courier_service_id, courier_name, service_name, tracking_number, tracking_url,
			label_url, status, label_state, delivery_state, raw_response::text,
			shipped_at, delivered_at, last_webhook_at, last_synced_at, shipping_notified_at,
			create_idempotency_key::text, label_idempotency_key::text, last_error,
			created_at, updated_at
	`, strings.TrimSpace(orderID), strings.TrimSpace(quote.CourierServiceID),
		strings.TrimSpace(quote.CourierName), strings.TrimSpace(quote.ServiceName)).Scan(shipmentScanTargets(&shipment)...)
	if err != nil {
		return nil, fmt.Errorf("prepare Easyship shipment: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shipment_items (
			shipment_id, order_item_id, quantity, sku, description, value_cents,
			weight_grams, length_mm, width_mm, height_mm, hs_code,
			easyship_category, origin_country_alpha2
		)
		SELECT $1::uuid, soi.id,
			greatest(soi.quantity - soi.fulfilled_quantity - soi.refunded_quantity, 0),
			soi.sku_snapshot, soi.product_name_snapshot, soi.unit_price_cents,
			mv.weight_grams, mv.length_mm, mv.width_mm, mv.height_mm,
			mp.hs_code, mp.easyship_category, mp.country_of_origin
		FROM shop_order_items soi
		JOIN merch_variants mv ON mv.id = soi.variant_id
		JOIN merch_products mp ON mp.id = soi.product_id
		WHERE soi.order_id = $2::uuid
			AND soi.fulfillment_method = 'ship'
			AND soi.status NOT IN ('cancelled', 'refunded', 'fulfilled')
			AND soi.quantity > soi.fulfilled_quantity + soi.refunded_quantity
		ON CONFLICT (shipment_id, order_item_id) DO NOTHING
	`, shipment.ID, shipment.OrderID); err != nil {
		return nil, fmt.Errorf("snapshot Easyship shipment items: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (event_type, actor_type, actor_email, entity_type, entity_id, order_id, metadata)
		VALUES ('easyship.shipment.prepared', 'admin', NULLIF($3, '')::citext, 'shipment', $1::uuid, $2::uuid,
			jsonb_build_object('courier_service_id', $4::text))
		ON CONFLICT DO NOTHING
	`, shipment.ID, shipment.OrderID, strings.ToLower(strings.TrimSpace(actorEmail)), shipment.CourierServiceID); err != nil {
		return nil, fmt.Errorf("record Easyship shipment preparation: %w", err)
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, err
	}
	return &shipment, nil
}

func ListEasyshipShipmentParcelItems(ctx *config.AppContext, shipmentID string) ([]*types.ShipmentParcelItem, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT order_item_id::text, quantity, sku, description, value_cents,
			weight_grams, length_mm, width_mm, height_mm, hs_code,
			easyship_category, origin_country_alpha2
		FROM shipment_items
		WHERE shipment_id = $1::uuid
		ORDER BY created_at, order_item_id
	`, strings.TrimSpace(shipmentID))
	if err != nil {
		return nil, fmt.Errorf("list Easyship shipment items: %w", err)
	}
	defer rows.Close()
	var items []*types.ShipmentParcelItem
	for rows.Next() {
		var item types.ShipmentParcelItem
		if err := rows.Scan(&item.OrderItemID, &item.Quantity, &item.SKU, &item.Description,
			&item.ValueCents, &item.WeightGrams, &item.LengthMM, &item.WidthMM,
			&item.HeightMM, &item.HSCode, &item.Category, &item.OriginCountry); err != nil {
			return nil, fmt.Errorf("scan Easyship shipment item: %w", err)
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("Easyship shipment has no parcel items")
	}
	return items, nil
}

func ListEasyshipOrderParcelItems(ctx *config.AppContext, orderID string) ([]*types.ShipmentParcelItem, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT soi.id::text,
			greatest(soi.quantity - soi.fulfilled_quantity - soi.refunded_quantity, 0),
			soi.sku_snapshot, soi.product_name_snapshot, soi.unit_price_cents,
			mv.weight_grams, mv.length_mm, mv.width_mm, mv.height_mm,
			mp.hs_code, mp.easyship_category, mp.country_of_origin
		FROM shop_order_items soi
		JOIN merch_variants mv ON mv.id = soi.variant_id
		JOIN merch_products mp ON mp.id = soi.product_id
		WHERE soi.order_id = $1::uuid
			AND soi.fulfillment_method = 'ship'
			AND soi.status NOT IN ('cancelled', 'refunded', 'fulfilled')
			AND soi.quantity > soi.fulfilled_quantity + soi.refunded_quantity
		ORDER BY soi.created_at, soi.id
	`, strings.TrimSpace(orderID))
	if err != nil {
		return nil, fmt.Errorf("list Easyship order items: %w", err)
	}
	defer rows.Close()
	var items []*types.ShipmentParcelItem
	for rows.Next() {
		var item types.ShipmentParcelItem
		if err := rows.Scan(&item.OrderItemID, &item.Quantity, &item.SKU, &item.Description,
			&item.ValueCents, &item.WeightGrams, &item.LengthMM, &item.WidthMM,
			&item.HeightMM, &item.HSCode, &item.Category, &item.OriginCountry); err != nil {
			return nil, fmt.Errorf("scan Easyship order item: %w", err)
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("order has no unfulfilled parcel items")
	}
	return items, nil
}

func CompleteEasyshipShipmentCreation(ctx *config.AppContext, localShipmentID string, result *types.Shipment, raw json.RawMessage, actorEmail string) error {
	if result == nil || strings.TrimSpace(result.ProviderShipmentID) == "" {
		return fmt.Errorf("Easyship shipment result is required")
	}
	return updateEasyshipShipment(ctx, localShipmentID, actorEmail, "easyship.shipment.created", `
		provider_shipment_id = $2,
		courier_service_id = coalesce(NULLIF($3, ''), courier_service_id),
		courier_name = coalesce(NULLIF($4, ''), courier_name),
		service_name = coalesce(NULLIF($5, ''), service_name),
		label_state = coalesce(NULLIF($6, ''), label_state),
		provider_label_id = coalesce(NULLIF($7, ''), provider_label_id),
		label_url = coalesce(NULLIF($8, ''), label_url),
		tracking_number = coalesce(NULLIF($9, ''), tracking_number),
		tracking_url = coalesce(NULLIF($10, ''), tracking_url),
		raw_response = $11::jsonb,
		last_error = '',
		last_synced_at = now()
	`, result.ProviderShipmentID, result.CourierServiceID, result.CourierName,
		result.ServiceName, result.LabelState, result.ProviderLabelID, result.LabelURL,
		result.TrackingNumber, result.TrackingURL, jsonText(raw))
}

func CompleteEasyshipLabelCreation(ctx *config.AppContext, localShipmentID string, result *types.Shipment, raw json.RawMessage, actorEmail string) error {
	if result == nil {
		return fmt.Errorf("Easyship label result is required")
	}
	status := "label_created"
	if strings.EqualFold(result.LabelState, "pending") || strings.TrimSpace(result.LabelURL) == "" {
		status = "pending"
	}
	return updateEasyshipShipment(ctx, localShipmentID, actorEmail, "easyship.label.requested", `
		provider_label_id = coalesce(NULLIF($2, ''), provider_label_id),
		label_url = coalesce(NULLIF($3, ''), label_url),
		label_state = coalesce(NULLIF($4, ''), 'pending'),
		tracking_number = coalesce(NULLIF($5, ''), tracking_number),
		tracking_url = coalesce(NULLIF($6, ''), tracking_url),
		status = $7,
		raw_response = raw_response || jsonb_build_object('label_response', $8::jsonb),
		last_error = '',
		last_synced_at = now()
	`, result.ProviderLabelID, result.LabelURL, result.LabelState,
		result.TrackingNumber, result.TrackingURL, status, jsonText(raw))
}

func RecordEasyshipShipmentError(ctx *config.AppContext, localShipmentID, message string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE shipments SET last_error = $2 WHERE id = $1::uuid
	`, strings.TrimSpace(localShipmentID), strings.TrimSpace(message))
	return err
}

func updateEasyshipShipment(ctx *config.AppContext, shipmentID, actorEmail, eventType, assignments string, args ...any) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	query := "UPDATE shipments SET " + assignments + " WHERE id = $1::uuid AND provider = 'easyship' RETURNING order_id::text"
	params := append([]any{strings.TrimSpace(shipmentID)}, args...)
	var orderID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), query, params...).Scan(&orderID); err != nil {
		return fmt.Errorf("update Easyship shipment: %w", err)
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (event_type, actor_type, actor_email, entity_type, entity_id, order_id)
		VALUES ($1, 'admin', NULLIF($4, '')::citext, 'shipment', $2::uuid, $3::uuid)
	`, eventType, shipmentID, orderID, strings.ToLower(strings.TrimSpace(actorEmail)))
	return err
}

func jsonText(raw json.RawMessage) string {
	if !json.Valid(raw) {
		return "{}"
	}
	return string(raw)
}

func shipmentScanTargets(shipment *types.Shipment) []any {
	return []any{
		&shipment.ID, &shipment.OrderID, &shipment.Provider, &shipment.ProviderShipmentID,
		&shipment.ProviderLabelID, &shipment.CourierServiceID, &shipment.CourierName,
		&shipment.ServiceName, &shipment.TrackingNumber, &shipment.TrackingURL,
		&shipment.LabelURL, &shipment.Status, &shipment.LabelState, &shipment.DeliveryState,
		&shipment.RawResponse, &shipment.ShippedAt, &shipment.DeliveredAt,
		&shipment.LastWebhookAt, &shipment.LastSyncedAt, &shipment.ShippingNotifiedAt,
		&shipment.CreateIdempotencyKey, &shipment.LabelIdempotencyKey, &shipment.LastError,
		&shipment.CreatedAt, &shipment.UpdatedAt,
	}
}

func GetEasyshipSettings(ctx *config.AppContext) (*types.EasyshipSettings, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var settings types.EasyshipSettings
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT contact_name, company_name, email, phone, line_1, line_2, city,
			region, postal_code, country_alpha2, coalesce(updated_by::text, ''),
			created_at, updated_at
		FROM easyship_settings
		WHERE singleton = true
	`).Scan(
		&settings.ContactName, &settings.CompanyName, &settings.Email, &settings.Phone,
		&settings.Line1, &settings.Line2, &settings.City, &settings.Region,
		&settings.PostalCode, &settings.CountryAlpha2, &settings.UpdatedBy,
		&settings.CreatedAt, &settings.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &settings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get easyship settings: %w", err)
	}
	return &settings, nil
}

func SaveEasyshipSettings(ctx *config.AppContext, settings *types.EasyshipSettings, actorEmail string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if settings == nil {
		return fmt.Errorf("easyship settings are required")
	}
	settings.ContactName = strings.TrimSpace(settings.ContactName)
	settings.CompanyName = strings.TrimSpace(settings.CompanyName)
	settings.Email = strings.ToLower(strings.TrimSpace(settings.Email))
	settings.Phone = strings.TrimSpace(settings.Phone)
	settings.Line1 = strings.TrimSpace(settings.Line1)
	settings.Line2 = strings.TrimSpace(settings.Line2)
	settings.City = strings.TrimSpace(settings.City)
	settings.Region = strings.TrimSpace(settings.Region)
	settings.PostalCode = strings.TrimSpace(settings.PostalCode)
	settings.CountryAlpha2 = strings.ToUpper(strings.TrimSpace(settings.CountryAlpha2))
	if !settings.IsConfigured() {
		return fmt.Errorf("contact name, address, city, region, postal code, and two-letter country are required")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO easyship_settings (
			singleton, contact_name, company_name, email, phone, line_1, line_2,
			city, region, postal_code, country_alpha2, updated_by
		) VALUES (
			true, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, '')::citext
		)
		ON CONFLICT (singleton) DO UPDATE SET
			contact_name = EXCLUDED.contact_name,
			company_name = EXCLUDED.company_name,
			email = EXCLUDED.email,
			phone = EXCLUDED.phone,
			line_1 = EXCLUDED.line_1,
			line_2 = EXCLUDED.line_2,
			city = EXCLUDED.city,
			region = EXCLUDED.region,
			postal_code = EXCLUDED.postal_code,
			country_alpha2 = EXCLUDED.country_alpha2,
			updated_by = EXCLUDED.updated_by
	`, settings.ContactName, settings.CompanyName, settings.Email, settings.Phone,
		settings.Line1, settings.Line2, settings.City, settings.Region,
		settings.PostalCode, settings.CountryAlpha2,
		strings.ToLower(strings.TrimSpace(actorEmail)))
	if err != nil {
		return fmt.Errorf("save easyship settings: %w", err)
	}
	return nil
}
