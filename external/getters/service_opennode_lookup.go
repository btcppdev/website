package getters

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

// OpenNodeServiceCharge is authenticated provider evidence. Looking up a charge
// does not record payment or grant access; only Status == "paid" is final.
type OpenNodeServiceCharge struct {
	HostedCheckoutURL string      `json:"hosted_checkout_url"`
	ID                string      `json:"id"`
	AttemptID         string      `json:"order_id"`
	Status            string      `json:"status"`
	Currency          string      `json:"currency"`
	SourceFiatValue   json.Number `json:"source_fiat_value"`
}

// LookupOpenNodeServiceCharge accepts a candidate charge ID, including one from a
// webhook following an uncertain creation response. The authenticated lookup must
// match a locally claimed attempt and its immutable quote before it can be bound.
func LookupOpenNodeServiceCharge(ctx context.Context, app *config.AppContext, chargeID string) (*OpenNodeServiceCharge, error) {
	return lookupOpenNodeServiceCharge(ctx, app, chargeID, openNodeHTTPClient)
}

func lookupOpenNodeServiceCharge(ctx context.Context, app *config.AppContext, chargeID string, client *http.Client) (*OpenNodeServiceCharge, error) {
	if app == nil || app.Env == nil || app.DB == nil || client == nil || strings.TrimSpace(app.Env.OpenNode.Key) == "" {
		return nil, ErrServiceChargeConfiguration
	}
	if id, err := uuid.Parse(chargeID); err != nil || id.String() != chargeID {
		return nil, ErrServicePayment
	}
	u, err := url.Parse(app.Env.OpenNode.Endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" ||
		(u.Scheme != "https" && (app.Env.Prod || u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1"))) {
		return nil, ErrServiceChargeConfiguration
	}
	u.Path, u.RawPath = "/v2/charge/"+chargeID, ""
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrServiceChargeReconciliation
	}
	req.Header.Set("Authorization", app.Env.OpenNode.Key)
	transport := *client
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := transport.Do(req)
	if err != nil {
		return nil, ErrServiceChargeReconciliation
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrServiceChargeReconciliation
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, ErrServiceChargeReconciliation
	}
	var result struct {
		Data OpenNodeServiceCharge `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil || result.Data.ID != chargeID {
		return nil, ErrServicePayment
	}
	charge := result.Data
	if id, err := uuid.Parse(charge.AttemptID); err != nil || id.String() != charge.AttemptID {
		return nil, ErrServicePayment
	}
	var quoteBytes []byte
	var reference string
	err = app.DB.QueryRow(ctx, `SELECT r.quote_snapshot,coalesce(a.provider_reference,'')
		FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id
		JOIN service_charge_creation_claims c ON c.attempt_id=a.id
		WHERE a.id=$1 AND a.provider='opennode'`, charge.AttemptID).Scan(&quoteBytes, &reference)
	if err != nil {
		return nil, ErrServicePayment
	}
	var quote ServiceQuote
	if json.Unmarshal(quoteBytes, &quote) != nil || quote.Provider != "opennode" ||
		(reference != "" && reference != charge.ID) || !charge.matchesUSD(quote.TotalCents) {
		return nil, ErrServicePayment
	}
	if err := BindServicePayment(app, charge.AttemptID, "opennode", charge.ID); err != nil {
		return nil, err
	}
	return &charge, nil
}

func (c OpenNodeServiceCharge) matchesUSD(cents int64) bool {
	// source_fiat_value is the original charge currency amount. fiat_value is
	// the merchant account conversion and cannot establish the customer's price.
	if c.Currency != "USD" || cents <= 0 || len(c.SourceFiatValue) > 64 {
		return false
	}
	// Reject exponents and non-decimal rational syntax before exact arithmetic.
	value := string(c.SourceFiatValue)
	if value == "" || strings.ContainsAny(value, "/eE+-") {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
	}
	r, ok := new(big.Rat).SetString(value)
	return ok && r.Cmp(new(big.Rat).SetFrac(big.NewInt(cents), big.NewInt(100))) == 0
}
