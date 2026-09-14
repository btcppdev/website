package getters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

var ErrServiceChargeReconciliation = errors.New("service charge needs provider reconciliation")
var ErrServiceChargeConfiguration = errors.New("service charge provider is not configured")

// InitOpenNodeServiceCharge creates at most one charge per durable attempt. The
// amount and buyer are loaded from central commerce, never from browser input.
// This adapter grants no access; settlement must be independently verified.
func InitOpenNodeServiceCharge(ctx context.Context, app *config.AppContext, personID, orderID string) (string, error) {
	return initOpenNodeServiceCharge(ctx, app, personID, orderID, openNodeHTTPClient)
}

func initOpenNodeServiceCharge(ctx context.Context, app *config.AppContext, personID, orderID string, client *http.Client) (string, error) {
	if app == nil || app.Env == nil || app.DB == nil || client == nil || strings.TrimSpace(app.Env.OpenNode.Key) == "" {
		return "", ErrServiceChargeConfiguration
	}
	endpoint, err := url.Parse(strings.TrimRight(app.Env.OpenNode.Endpoint, "/") + "/charges")
	if err != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Host == "" ||
		(endpoint.Scheme != "https" && (app.Env.Prod || endpoint.Scheme != "http" || (endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost"))) {
		return "", ErrServiceChargeConfiguration
	}
	attempt, err := BeginServicePayment(app, personID, orderID)
	if err != nil {
		return "", err
	}
	if attempt.Provider != "opennode" {
		return "", ErrServicePayment
	}
	if attempt.Reference != "" {
		return attempt.Reference, nil
	}
	if !attempt.CanCreateCharge {
		return "", ErrServiceChargeReconciliation
	}
	payload, err := json.Marshal(map[string]any{
		"amount":   fmt.Sprintf("%d.%02d", attempt.TotalCents/100, attempt.TotalCents%100),
		"currency": "USD", "description": "Bitcoin++ service order " + attempt.OrderPublicID,
		"customer_email": attempt.BuyerEmail, "order_id": attempt.ID, "auto_settle": false,
		"callback_url": app.Env.GetURI() + "/callback/services/opennode",
		"success_url":  app.Env.GetURI() + "/services/orders/" + url.PathEscape(attempt.OrderPublicID),
		"ttl":          10,
	})
	if err != nil {
		return "", ErrServiceChargeReconciliation
	}
	attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return "", ErrServiceChargeReconciliation
	}
	req.Header.Set("Authorization", app.Env.OpenNode.Key)
	req.Header.Set("Content-Type", "application/json")
	// Never send provider credentials to a redirect destination.
	transport := *client
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := transport.Do(req)
	if err != nil {
		return "", ErrServiceChargeReconciliation
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ErrServiceChargeReconciliation
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return "", ErrServiceChargeReconciliation
	}
	var result struct {
		Data struct {
			ID      string `json:"id"`
			OrderID string `json:"order_id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil || result.Data.OrderID != attempt.ID {
		return "", ErrServiceChargeReconciliation
	}
	if id, err := uuid.Parse(result.Data.ID); err != nil || id.String() != result.Data.ID {
		return "", ErrServiceChargeReconciliation
	}
	if err := BindServicePayment(app, attempt.ID, "opennode", result.Data.ID); err != nil {
		return "", ErrServiceChargeReconciliation
	}
	return result.Data.ID, nil
}
