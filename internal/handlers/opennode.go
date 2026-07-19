package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type (
	ChargeEvent struct {
		ID          string `schema:"id"`
		Status      string `schema:"status"`
		Description string `schema:"description"`
		HashedOrder string `schema:"hashed_order"`
	}

	Charge struct {
		ID          string                  `json:"id"`
		Status      string                  `json:"status"`
		Description string                  `json:"description"`
		FiatVal     float64                 `json:"fiat_value"`
		Price       int64                   `json:"price"`
		CreatedAt   openNodeUnixTime        `json:"created_at"`
		Metadata    *types.OpenNodeMetadata `json:"metadata"`
	}

	envelope struct {
		Data Charge `json:"data"`
	}

	openNodeHTTPError struct {
		StatusCode int
		Body       string
	}

	openNodeUnixTime int64
)

func (t *openNodeUnixTime) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		*t = 0
		return nil
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err == nil {
		*t = openNodeUnixTime(seconds)
		return nil
	}
	parsed, timeErr := time.Parse(time.RFC3339, raw)
	if timeErr != nil {
		return fmt.Errorf("invalid OpenNode created_at %q: %w", raw, err)
	}
	*t = openNodeUnixTime(parsed.Unix())
	return nil
}

func (e *openNodeHTTPError) Error() string {
	return fmt.Sprintf("OpenNode charge lookup returned %d: %s", e.StatusCode, e.Body)
}

var openNodeHTTPClient = &http.Client{Timeout: 15 * time.Second}

func GetCharge(ctx *config.AppContext, ID string) (*Charge, error) {
	chargeURL, err := openNodeChargeURL(ctx.Env.OpenNode.Endpoint, ID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, chargeURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", ctx.Env.OpenNode.Key)
	req.Header.Set("accept", "application/json")

	res, err := openNodeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, &openNodeHTTPError{StatusCode: res.StatusCode, Body: strings.TrimSpace(string(resBody))}
	}

	var envel envelope
	err = json.Unmarshal(resBody, &envel)
	return &envel.Data, err
}

func openNodeChargeURL(endpoint, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("missing OpenNode charge id")
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "https://api.opennode.com/v1"
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid OpenNode endpoint %q", endpoint)
	}
	u.Path = "/v2/charge/" + url.PathEscape(id)
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
