package getters

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestInitOpenNodeCheckoutAcceptsCreatedAndPreservesTicketSubtotal(t *testing.T) {
	oldClient := openNodeHTTPClient
	openNodeHTTPClient = &http.Client{Transport: openNodeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://opennode.example.test/v1/charges" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		var payload types.OpenNodeRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %s", err)
		}
		if payload.Amount != 165.00 {
			t.Fatalf("sandbox charge amount = %v, want 165.00", payload.Amount)
		}
		if payload.Currency != "USD" {
			t.Fatalf("charge currency = %q, want USD", payload.Currency)
		}
		if payload.Metadata.TicketTotalCents != 6000 || payload.Metadata.AddOnCents != 10500 || payload.Metadata.Quantity != 2 {
			t.Fatalf("unexpected metadata: %+v", payload.Metadata)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"data":{
				"id":"ch_created",
				"status":"unpaid",
				"desc_hash":false,
				"ttl":10,
				"hosted_checkout_url":"https://checkout.example.test/ch_created",
				"metadata":{"email":"buyer@example.test","quantity":2,"tix-local":false,"subscribe":true,"add-on-cents":10500}
			}}`)),
		}, nil
	})}
	t.Cleanup(func() { openNodeHTTPClient = oldClient })

	ctx := &config.AppContext{Env: &types.EnvConfig{
		Host:          "localhost",
		OpenNode:      types.OpenNodeConfig{Key: "test-key", Endpoint: "https://opennode.example.test/v1"},
		LocalExternal: "https://checkout.example.test",
	}}
	ticket := &types.ConfTicket{Currency: "usd"}
	conf := &types.Conf{Ref: "conf-ref", Tag: "dev26", Desc: "DEV26"}

	charge, err := InitOpenNodeCheckout(ctx, 30, 40, ticket, conf, types.TicketTypeGeneral, 2, "buyer@example.test", "", false, 10500, "shop-order")
	if err != nil {
		t.Fatalf("InitOpenNodeCheckout: %s", err)
	}
	if charge.ID != "ch_created" {
		t.Fatalf("charge ID = %q", charge.ID)
	}
	if charge.TTL != 10 || charge.Metadata == nil || charge.Metadata.Quantity != 2 || !charge.Metadata.Subscribe {
		t.Fatalf("unexpected decoded charge: %+v", charge)
	}
}

func TestInitOpenNodeShopCheckoutUsesOrderTotalOutsideProduction(t *testing.T) {
	oldClient := openNodeHTTPClient
	openNodeHTTPClient = &http.Client{Transport: openNodeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var payload types.OpenNodeRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %s", err)
		}
		if payload.Amount != 250.00 {
			t.Fatalf("sandbox shop charge amount = %v, want 250.00", payload.Amount)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":{"id":"ch_shop","status":"unpaid"}}`)),
		}, nil
	})}
	t.Cleanup(func() { openNodeHTTPClient = oldClient })

	ctx := &config.AppContext{Env: &types.EnvConfig{
		Host:          "localhost",
		LocalExternal: "https://checkout.example.test",
		OpenNode:      types.OpenNodeConfig{Key: "test-key", Endpoint: "https://opennode.example.test/v1"},
	}}
	order := &types.ShopOrder{
		ID:         "order-id",
		PublicID:   "public-id",
		BuyerEmail: "buyer@example.test",
		Currency:   "usd",
		TotalCents: 25000,
	}
	if _, err := InitOpenNodeShopCheckout(ctx, order); err != nil {
		t.Fatalf("InitOpenNodeShopCheckout: %s", err)
	}
}

type openNodeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn openNodeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
