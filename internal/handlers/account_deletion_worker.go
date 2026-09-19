package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var accountDeletionHTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func validAccountDeletionEndpoint(endpoint, token string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(token) < 32 {
		return false
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	return u.Scheme == "https" || (u.Scheme == "http" && loopback)
}

func StartAccountDeletionDelivery(app *config.AppContext) {
	endpoint := strings.TrimSpace(os.Getenv("ALERTS_ACCOUNT_EVENTS_ENDPOINT"))
	token := os.Getenv("ALERTS_ACCOUNT_EVENTS_TOKEN")
	if endpoint == "" && token == "" {
		return
	}
	if !validAccountDeletionEndpoint(endpoint, token) {
		app.Err.Printf("account deletion delivery configuration is invalid")
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := processAccountDeletionDelivery(ctx, app, "", endpoint, token, time.Now())
			cancel()
			if err != nil {
				app.Err.Printf("account deletion delivery requires retry")
			}
		}
	}()
}

func processAccountDeletionDelivery(ctx context.Context, app *config.AppContext, personID, endpoint, token string, now time.Time) (bool, error) {
	if !validAccountDeletionEndpoint(endpoint, token) {
		return false, errors.New("invalid account deletion endpoint")
	}
	job, err := getters.ClaimAccountDeletionDelivery(ctx, app, personID, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	deliveryErr := postAccountDeletion(ctx, endpoint, token, job.PersonID)
	changed, err := getters.FinishAccountDeletionDelivery(ctx, app, job, deliveryErr == nil, now)
	if err != nil {
		return true, err
	}
	if !changed {
		return true, errors.New("account deletion lease superseded")
	}
	return true, deliveryErr
}

func postAccountDeletion(ctx context.Context, endpoint, token, personID string) error {
	if !validAccountDeletionEndpoint(endpoint, token) {
		return errors.New("invalid account deletion endpoint")
	}
	eventID := "account-deleted:" + personID
	payload, _ := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		EventID       string `json:"event_id"`
		PersonID      string `json:"person_id"`
	}{1, eventID, personID})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", eventID)
	response, err := accountDeletionHTTPClient.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("account deletion returned status %d", response.StatusCode)
	}
	return nil
}
