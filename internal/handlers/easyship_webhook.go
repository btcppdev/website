package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

type easyshipWebhookEnvelope struct {
	EventType    string          `json:"event_type"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Data         json.RawMessage `json:"data"`
}

type easyshipWebhookData struct {
	EasyshipShipmentID string `json:"easyship_shipment_id"`
}

func EasyshipCallback(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxWebhookBodyBytes)
	secret := ""
	if ctx != nil && ctx.Env != nil {
		secret = strings.TrimSpace(ctx.Env.Easyship.WebhookSecret)
	}
	if secret == "" {
		ctx.Err.Printf("easyship callback received while EASYSHIP_WEBHOOK_SECRET is not configured")
		http.Error(w, "webhook unavailable", http.StatusServiceUnavailable)
		return
	}
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		ctx.Err.Printf("easyship callback read body: %s", err)
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := verifyEasyshipSignature(r.Header.Get("X-EASYSHIP-SIGNATURE"), secret, time.Now()); err != nil {
		ctx.Err.Printf("easyship callback signature rejected: %s", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var envelope easyshipWebhookEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		ctx.Err.Printf("easyship callback decode: %s", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	envelope.EventType = strings.TrimSpace(envelope.EventType)
	envelope.ResourceType = strings.TrimSpace(envelope.ResourceType)
	envelope.ResourceID = strings.TrimSpace(envelope.ResourceID)
	if envelope.EventType == "" || envelope.ResourceType == "" || envelope.ResourceID == "" || !json.Valid(envelope.Data) {
		http.Error(w, "incomplete payload", http.StatusBadRequest)
		return
	}
	var data easyshipWebhookData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		http.Error(w, "invalid event data", http.StatusBadRequest)
		return
	}
	shipmentID := strings.TrimSpace(data.EasyshipShipmentID)
	if shipmentID == "" && envelope.ResourceType == "shipment" {
		shipmentID = envelope.ResourceID
	}
	if _, err := getters.StoreEasyshipWebhookEvent(ctx, getters.EasyshipWebhookEventInput{
		EventType: envelope.EventType, ResourceType: envelope.ResourceType,
		ResourceID: envelope.ResourceID, EasyshipShipmentID: shipmentID, Payload: payload,
	}); err != nil {
		ctx.Err.Printf("easyship callback store %s/%s: %s", envelope.EventType, envelope.ResourceID, err)
		http.Error(w, "unable to accept event", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func verifyEasyshipSignature(token, secret string, now time.Time) error {
	token = strings.TrimSpace(token)
	secret = strings.TrimSpace(secret)
	if token == "" || secret == "" {
		return errors.New("signature or secret is empty")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("signature is not a JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode header: %w", err)
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "HS256" {
		return errors.New("signature algorithm must be HS256")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("signature mismatch")
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode claims: %w", err)
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return fmt.Errorf("decode claims JSON: %w", err)
	}
	if expiry, ok, err := easyshipJWTUnixClaim(claims, "exp"); err != nil {
		return err
	} else if ok && !now.Before(time.Unix(expiry, 0)) {
		return errors.New("signature expired")
	}
	if notBefore, ok, err := easyshipJWTUnixClaim(claims, "nbf"); err != nil {
		return err
	} else if ok && now.Before(time.Unix(notBefore, 0)) {
		return errors.New("signature is not valid yet")
	}
	return nil
}

func easyshipJWTUnixClaim(claims map[string]json.RawMessage, name string) (int64, bool, error) {
	raw, ok := claims[name]
	if !ok {
		return 0, false, nil
	}
	var value json.Number
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false, fmt.Errorf("invalid %s claim", name)
	}
	unix, err := value.Int64()
	if err != nil {
		return 0, false, fmt.Errorf("invalid %s claim", name)
	}
	return unix, true, nil
}
