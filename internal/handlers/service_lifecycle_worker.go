package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func lifecycleEndpoint(app *config.AppContext) (string, error) {
	if app == nil || app.Env == nil || strings.TrimSpace(app.Env.InsiderFulfillmentToken) == "" {
		return "", errors.New("lifecycle delivery not configured")
	}
	u, err := url.Parse(strings.TrimSpace(app.Env.InsiderLifecycleEndpoint))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid lifecycle endpoint")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", errors.New("lifecycle endpoint requires HTTPS")
	}
	return u.String(), nil
}

func StartServiceLifecycle(app *config.AppContext) {
	if os.Getenv("SERVICE_FULFILLMENT_ENABLED") != "true" {
		return
	}
	if _, err := lifecycleEndpoint(app); err != nil {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := processServiceLifecycle(ctx, app, "", time.Now())
			cancel()
			if err != nil {
				app.Err.Printf("service lifecycle requires retry or review")
			}
		}
	}()
}

func processServiceLifecycle(ctx context.Context, app *config.AppContext, checkoutID string, now time.Time) (bool, error) {
	endpoint, err := lifecycleEndpoint(app)
	if err != nil {
		return false, err
	}
	job, err := getters.ClaimServiceLifecycle(ctx, app, checkoutID, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	finish := func(state string, cause error) (bool, error) {
		_, err := getters.FinishServiceLifecycle(ctx, app, job, state, now)
		if err != nil {
			return true, err
		}
		return true, cause
	}
	exists, err := getters.ServiceLifecycleAccountExists(ctx, app, job)
	if err != nil {
		return finish("retry", err)
	}
	if !exists {
		return finish("review", errors.New("lifecycle account no longer matches"))
	}
	var event getters.ServiceLifecycleEvent
	if err = json.Unmarshal(job.Payload, &event); err != nil {
		return finish("review", err)
	}
	if err = postServiceLifecycle(ctx, app, endpoint, event.EventID, job.Payload); err != nil {
		return finish("retry", err)
	}
	return finish("accepted", nil)
}

func postServiceLifecycle(ctx context.Context, app *config.AppContext, endpoint, eventID string, payload []byte) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(app.Env.InsiderFulfillmentToken))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", eventID)
	response, err := insiderFulfillmentHTTPClient.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// 202 is deliberately not final: the receiver must persist before acknowledging.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("lifecycle delivery returned status %d", response.StatusCode)
	}
	return nil
}
