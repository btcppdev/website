package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestOpenNodeTicketItemsMixedOrderUsesTicketQuantityOnly(t *testing.T) {
	charge := &Charge{
		Description: "DEV26 ticket",
		// Development checkouts deliberately use a fixed $1 charge. The
		// metadata retains the actual ticket subtotal for fulfillment.
		FiatVal: 1,
		Metadata: &types.OpenNodeMetadata{
			Quantity:         1,
			AddOnCents:       10500,
			TicketTotalCents: 3000,
		},
	}
	items, total, err := openNodeTicketItems(charge, types.TicketTypeGeneral)
	if err != nil {
		t.Fatalf("openNodeTicketItems: %s", err)
	}
	if len(items) != 1 {
		t.Fatalf("ticket count = %d, want 1", len(items))
	}
	if total != 3000 || items[0].Total != 3000 {
		t.Fatalf("ticket totals = (%d, %d), want 3000", total, items[0].Total)
	}
}

func TestOpenNodeTicketItemsRejectsMalformedQuantity(t *testing.T) {
	for _, quantity := range []float64{0, -1, 1.5, 101} {
		charge := &Charge{FiatVal: 10, Metadata: &types.OpenNodeMetadata{Quantity: quantity}}
		if _, _, err := openNodeTicketItems(charge, types.TicketTypeGeneral); err == nil {
			t.Fatalf("quantity %v returned nil error", quantity)
		}
	}
}

func TestGetChargeUsesConfiguredOpenNodeOrigin(t *testing.T) {
	oldClient := openNodeHTTPClient
	openNodeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v2/charge/ch_test" {
			t.Fatalf("charge path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "test-key" {
			t.Fatalf("authorization header missing")
		}
		return jsonResponse(`{"data":{"id":"ch_test","status":"paid","created_at":"1661215876","fiat_value":30,"metadata":{"quantity":1}}}`), nil
	})}
	t.Cleanup(func() { openNodeHTTPClient = oldClient })
	ctx := &config.AppContext{Env: &types.EnvConfig{OpenNode: types.OpenNodeConfig{Key: "test-key", Endpoint: "https://opennode.example.test/v1"}}}

	charge, err := GetCharge(ctx, "ch_test")
	if err != nil {
		t.Fatalf("GetCharge: %s", err)
	}
	if charge.ID != "ch_test" || charge.Status != "paid" || charge.Metadata == nil || charge.Metadata.Quantity != 1 {
		t.Fatalf("unexpected charge: %+v", charge)
	}
	if int64(charge.CreatedAt) != 1661215876 {
		t.Fatalf("created_at = %d", charge.CreatedAt)
	}
}

func TestOpenNodeUnixTimeAcceptsAPIRepresentations(t *testing.T) {
	for _, input := range []string{`1661215876`, `"1661215876"`, `"2022-08-23T00:51:16Z"`} {
		var got openNodeUnixTime
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			t.Fatalf("Unmarshal(%s): %s", input, err)
		}
		if int64(got) != 1661215876 {
			t.Fatalf("Unmarshal(%s) = %d", input, got)
		}
	}
}

func TestOpenNodeCallbackTrustsFetchedChargeStatus(t *testing.T) {
	oldClient := openNodeHTTPClient
	openNodeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"data":{"id":"ch_unpaid","status":"processing","metadata":{"email":"buyer@example.test","conf-ref":"conf","quantity":1}}}`), nil
	})}
	t.Cleanup(func() { openNodeHTTPClient = oldClient })
	key := "test-key"
	ctx := &config.AppContext{
		Env:   &types.EnvConfig{OpenNode: types.OpenNodeConfig{Key: key, Endpoint: "https://opennode.example.test/v1"}},
		Infos: log.New(io.Discard, "", 0),
		Err:   log.New(io.Discard, "", 0),
	}
	form := url.Values{
		"id":           {"ch_unpaid"},
		"status":       {"paid"},
		"hashed_order": {computeHash(key, "ch_unpaid")},
	}
	req := httptest.NewRequest(http.MethodPost, "/callback/opennode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	OpenNodeCallback(recorder, req, ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestOpenNodeCallbackAcknowledgesSyntheticSimulatorCharge(t *testing.T) {
	oldClient := openNodeHTTPClient
	openNodeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponseWithStatus(http.StatusBadRequest, `{"message":"This checkout does not exist."}`), nil
	})}
	t.Cleanup(func() { openNodeHTTPClient = oldClient })
	key := "test-key"
	ctx := &config.AppContext{
		Env:   &types.EnvConfig{OpenNode: types.OpenNodeConfig{Key: key, Endpoint: "https://opennode.example.test/v1"}},
		Infos: log.New(io.Discard, "", 0),
		Err:   log.New(io.Discard, "", 0),
	}
	form := url.Values{
		"id":           {"synthetic-charge"},
		"status":       {"paid"},
		"hashed_order": {computeHash(key, "synthetic-charge")},
	}
	req := httptest.NewRequest(http.MethodPost, "/callback/opennode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	OpenNodeCallback(recorder, req, ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func jsonResponse(body string) *http.Response {
	return jsonResponseWithStatus(http.StatusOK, body)
}

func jsonResponseWithStatus(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestOpenNodeChargeURL(t *testing.T) {
	got, err := openNodeChargeURL("https://api.opennode.com/v1", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.opennode.com/v2/charge/abc" {
		t.Fatalf("URL = %q", got)
	}
}
