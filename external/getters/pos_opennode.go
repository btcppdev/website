package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Currency is intentionally absent: OpenNode interprets integer amount as sats.
type posChargeRequest struct {
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
	OrderID     string `json:"order_id"`
	CallbackURL string `json:"callback_url"`
	TTL         int    `json:"ttl"`
}

func POSOpenNodeCreate(ctx *config.AppContext, s *types.POSSale) (*types.OpenNodePayment, error) {
	payload, err := json.Marshal(posChargeRequest{Amount: s.TotalSats, Description: "bitcoin++ conference merch", OrderID: s.ID, CallbackURL: ctx.Env.GetURI() + "/callback/opennode-pos/" + s.ID, TTL: 10})
	if err != nil {
		return nil, err
	}
	var result types.OpenNodeResponse
	if err = posOpenNodeRequest(ctx, http.MethodPost, strings.TrimRight(ctx.Env.OpenNode.Endpoint, "/")+"/charges", payload, &result); err != nil {
		return nil, err
	}
	p := result.Data
	if p == nil || p.ID == "" || p.Amount != uint64(s.TotalSats) || p.OrderID != s.ID || p.LNInvoice.Invoice == "" || p.LNInvoice.ExpiresAt == 0 {
		return nil, fmt.Errorf("OpenNode returned an incomplete or mismatched Lightning charge")
	}
	return p, nil
}

type POSCharge struct {
	ID      string `json:"id"`
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Price   int64  `json:"price"`
	Amount  int64  `json:"amount"`
}

func POSOpenNodeCharge(ctx *config.AppContext, id string) (*POSCharge, error) {
	u, err := url.Parse(ctx.Env.OpenNode.Endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = "/v2/charge/" + url.PathEscape(id)
	u.RawPath = ""
	u.RawQuery = ""
	var result struct {
		Data POSCharge `json:"data"`
	}
	if err = posOpenNodeRequest(ctx, http.MethodGet, u.String(), nil, &result); err != nil {
		return nil, err
	}
	if result.Data.ID != id {
		return nil, fmt.Errorf("charge ID mismatch")
	}
	if result.Data.Price == 0 {
		result.Data.Price = result.Data.Amount
	}
	return &result.Data, nil
}
func posOpenNodeRequest(ctx *config.AppContext, method, endpoint string, payload []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx.DatabaseContext(), method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", ctx.Env.OpenNode.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := openNodeHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OpenNode returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

var posRateCache struct {
	sync.Mutex
	values  map[string]float64
	expires time.Time
}

func POSLocalRate(ctx *config.AppContext, currency string) (float64, error) {
	posRateCache.Lock()
	defer posRateCache.Unlock()
	key := ctx.Env.OpenNode.Endpoint + ":" + currency
	if time.Now().Before(posRateCache.expires) && posRateCache.values[key] > 0 {
		return posRateCache.values[key], nil
	}
	var result struct {
		Data map[string]map[string]json.RawMessage `json:"data"`
	}
	if err := posOpenNodeRequest(ctx, http.MethodGet, strings.TrimRight(ctx.Env.OpenNode.Endpoint, "/")+"/rates", nil, &result); err != nil {
		return 0, err
	}
	var rate float64
	if err := json.Unmarshal(result.Data["BTC"+currency][currency], &rate); err != nil || rate <= 0 {
		return 0, fmt.Errorf("no exchange rate for %s", currency)
	}
	if posRateCache.values == nil || time.Now().After(posRateCache.expires) {
		posRateCache.values = map[string]float64{}
	}
	posRateCache.values[key] = rate
	posRateCache.expires = time.Now().Add(5 * time.Minute)
	return rate, nil
}
