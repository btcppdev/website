package getters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

type EasyshipWebhookEventInput struct {
	EventType          string
	ResourceType       string
	ResourceID         string
	EasyshipShipmentID string
	Payload            []byte
}

type easyshipStoredWebhookEvent struct {
	ID                 string
	EventType          string
	ResourceType       string
	ResourceID         string
	EasyshipShipmentID string
	Payload            []byte
}

type easyshipWebhookPayload struct {
	Data struct {
		EasyshipShipmentID string `json:"easyship_shipment_id"`
		Status             string `json:"status"`
		LabelURL           string `json:"label_url"`
		TrackingNumber     string `json:"tracking_number"`
		TrackingPageURL    string `json:"tracking_page_url"`
	} `json:"data"`
}

func ListEasyshipWebhookEvents(ctx *config.AppContext, limit int) ([]*types.EasyshipWebhookEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, event_type, resource_id, easyship_shipment_id, status,
			attempts, last_error, received_at, processed_at
		FROM easyship_webhook_events
		ORDER BY received_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list Easyship webhook events: %w", err)
	}
	defer rows.Close()
	var events []*types.EasyshipWebhookEvent
	for rows.Next() {
		var event types.EasyshipWebhookEvent
		if err := rows.Scan(&event.ID, &event.EventType, &event.ResourceID,
			&event.EasyshipShipmentID, &event.Status, &event.Attempts,
			&event.LastError, &event.ReceivedAt, &event.ProcessedAt); err != nil {
			return nil, fmt.Errorf("scan Easyship webhook event: %w", err)
		}
		events = append(events, &event)
	}
	return events, rows.Err()
}

func StoreEasyshipWebhookEvent(ctx *config.AppContext, in EasyshipWebhookEventInput) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(in.EventType) == "" || !json.Valid(in.Payload) {
		return false, fmt.Errorf("valid Easyship event type and payload are required")
	}
	digest := sha256.Sum256(in.Payload)
	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO easyship_webhook_events (
			payload_sha256, event_type, resource_type, resource_id,
			easyship_shipment_id, payload
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT (payload_sha256) DO NOTHING
		RETURNING id::text
	`, hex.EncodeToString(digest[:]), strings.TrimSpace(in.EventType),
		strings.TrimSpace(in.ResourceType), strings.TrimSpace(in.ResourceID),
		strings.TrimSpace(in.EasyshipShipmentID), string(in.Payload)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store Easyship webhook event: %w", err)
	}
	return true, nil
}

func ProcessEasyshipWebhookEvents(ctx *config.AppContext, limit int) (int, error) {
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("database is not configured")
	}
	if limit <= 0 {
		limit = 25
	}
	processed := 0
	for processed < limit {
		found, err := processOneEasyshipWebhookEvent(ctx)
		if err != nil {
			return processed, err
		}
		if !found {
			break
		}
		processed++
	}
	return processed, nil
}

func processOneEasyshipWebhookEvent(ctx *config.AppContext) (bool, error) {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx.DatabaseContext())

	var event easyshipStoredWebhookEvent
	err = tx.QueryRow(ctx.DatabaseContext(), `
		WITH candidate AS (
			SELECT id
			FROM easyship_webhook_events
			WHERE attempts < 10
				AND (
					(status IN ('pending', 'failed') AND next_attempt_at <= now())
					OR (status = 'processing' AND updated_at <= now() - interval '5 minutes')
				)
			ORDER BY received_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE easyship_webhook_events event
		SET status = 'processing', attempts = attempts + 1, last_error = ''
		FROM candidate
		WHERE event.id = candidate.id
		RETURNING event.id::text, event.event_type, event.resource_type,
			event.resource_id, event.easyship_shipment_id, event.payload::text
	`).Scan(&event.ID, &event.EventType, &event.ResourceType, &event.ResourceID,
		&event.EasyshipShipmentID, &event.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim Easyship webhook event: %w", err)
	}

	var payload easyshipWebhookPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		if err := finishEasyshipWebhookEvent(ctx, tx, event.ID, "ignored", "invalid stored payload"); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx.DatabaseContext())
	}
	shipmentID := strings.TrimSpace(event.EasyshipShipmentID)
	if shipmentID == "" {
		shipmentID = strings.TrimSpace(payload.Data.EasyshipShipmentID)
	}
	if shipmentID == "" && event.ResourceType == "shipment" {
		shipmentID = strings.TrimSpace(event.ResourceID)
	}
	if !supportedEasyshipWebhookEvent(event.EventType) {
		if err := finishEasyshipWebhookEvent(ctx, tx, event.ID, "ignored", "unsupported event type"); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx.DatabaseContext())
	}

	var shipment struct {
		ID            string
		OrderID       string
		Status        string
		LabelState    string
		DeliveryState string
	}
	err = tx.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, order_id::text, status, label_state, delivery_state
		FROM shipments
		WHERE provider = 'easyship' AND provider_shipment_id = $1
		FOR UPDATE
	`, shipmentID).Scan(&shipment.ID, &shipment.OrderID, &shipment.Status,
		&shipment.LabelState, &shipment.DeliveryState)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := finishEasyshipWebhookEvent(ctx, tx, event.ID, "ignored", "Easyship shipment is not known locally"); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx.DatabaseContext())
	}
	if err != nil {
		return false, fmt.Errorf("load shipment for Easyship event: %w", err)
	}

	status, labelState, deliveryState, markShipped, markDelivered := easyshipShipmentTransition(
		event.EventType, payload.Data.Status, shipment.Status, shipment.LabelState, shipment.DeliveryState,
	)
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shipments
		SET status = $2,
			label_state = $3,
			delivery_state = $4,
			label_url = coalesce(NULLIF($5, ''), label_url),
			tracking_number = coalesce(NULLIF($6, ''), tracking_number),
			tracking_url = coalesce(NULLIF($7, ''), tracking_url),
			shipped_at = CASE WHEN $8 THEN coalesce(shipped_at, now()) ELSE shipped_at END,
			delivered_at = CASE WHEN $9 THEN coalesce(delivered_at, now()) ELSE delivered_at END,
			last_webhook_at = now(),
			last_synced_at = now(),
			raw_response = raw_response || jsonb_build_object('last_webhook', $10::jsonb)
		WHERE id = $1::uuid
	`, shipment.ID, status, labelState, deliveryState,
		strings.TrimSpace(payload.Data.LabelURL), strings.TrimSpace(payload.Data.TrackingNumber),
		strings.TrimSpace(payload.Data.TrackingPageURL), markShipped, markDelivered, string(event.Payload)); err != nil {
		return false, fmt.Errorf("apply Easyship shipment event: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (
			event_type, actor_type, entity_type, entity_id, order_id, metadata
		) VALUES ($1, 'system', 'shipment', $2::uuid, $3::uuid,
			jsonb_build_object('easyship_event_id', $4::text, 'easyship_shipment_id', $5::text))
	`, "easyship."+event.EventType, shipment.ID, shipment.OrderID, event.ID, shipmentID); err != nil {
		return false, fmt.Errorf("record Easyship shop event: %w", err)
	}
	if err := finishEasyshipWebhookEvent(ctx, tx, event.ID, "processed", ""); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx.DatabaseContext())
}

func finishEasyshipWebhookEvent(ctx *config.AppContext, tx pgx.Tx, id, status, message string) error {
	_, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE easyship_webhook_events
		SET status = $2, last_error = $3, processed_at = now()
		WHERE id = $1::uuid
	`, id, status, message)
	if err != nil {
		return fmt.Errorf("finish Easyship webhook event: %w", err)
	}
	return nil
}

func supportedEasyshipWebhookEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "shipment.label.created", "shipment.label.failed",
		"shipment.tracking.status.changed", "shipment.tracking.checkpoints.created",
		"shipment.cancelled":
		return true
	default:
		return false
	}
}

func easyshipShipmentTransition(eventType, providerStatus, currentStatus, currentLabelState, currentDeliveryState string) (status, labelState, deliveryState string, shipped, delivered bool) {
	status = currentStatus
	labelState = currentLabelState
	deliveryState = currentDeliveryState
	switch strings.TrimSpace(eventType) {
	case "shipment.label.created":
		if currentLabelState != "voided" && currentStatus != "cancelled" {
			labelState = "generated"
			if currentStatus == "pending" || currentStatus == "failed" {
				status = "label_created"
			}
		}
	case "shipment.label.failed":
		if currentLabelState != "generated" && currentLabelState != "voided" {
			labelState = "failed"
			if currentStatus == "pending" || currentStatus == "failed" {
				status = "failed"
			}
		}
	case "shipment.cancelled":
		if currentStatus != "shipped" && currentStatus != "delivered" && currentStatus != "returned" {
			labelState = "voided"
			deliveryState = "Cancelled"
			status = "cancelled"
		}
	case "shipment.tracking.status.changed", "shipment.tracking.checkpoints.created":
		normalized := strings.ToLower(strings.TrimSpace(providerStatus))
		switch {
		case currentStatus == "returned" || currentStatus == "cancelled":
			// Never move a terminal return/cancellation back into delivery.
		case normalized == "delivered":
			deliveryState = strings.TrimSpace(providerStatus)
			status = "delivered"
			shipped = true
			delivered = true
		case currentStatus == "delivered":
			// Never regress a terminal local state when callbacks arrive out of order.
		default:
			if strings.TrimSpace(providerStatus) != "" {
				deliveryState = strings.TrimSpace(providerStatus)
			}
			if normalized == "shipped" || normalized == "shipped (no tracking provided)" ||
				strings.Contains(normalized, "in transit") || normalized == "out for delivery" ||
				strings.Contains(normalized, "consolidation center") {
				status = "shipped"
				shipped = true
			}
		}
	}
	return status, labelState, deliveryState, shipped, delivered
}
