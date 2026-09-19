package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLifecycleDeliveryAcknowledgmentAndRedirects(t *testing.T) {
	payload := []byte(`{"event_id":"subscription:test:1"}`)
	status := http.StatusServiceUnavailable
	redirectCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++; w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("Idempotency-Key") != "subscription:test:1" || string(body) != string(payload) {
			t.Error("delivery identity or payload changed")
		}
		w.Header().Set("Location", target.URL)
		w.WriteHeader(status)
	}))
	defer receiver.Close()
	app := &config.AppContext{Env: &types.EnvConfig{InsiderLifecycleEndpoint: receiver.URL, InsiderFulfillmentToken: "private-token"}}
	endpoint, err := lifecycleEndpoint(app)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{503, 202, 302, 400, 200, 204} {
		status = code
		err := postServiceLifecycle(context.Background(), app, endpoint, "subscription:test:1", payload)
		if (err == nil) != (code == 200 || code == 204) {
			t.Errorf("status %d: %v", code, err)
		}
	}
	if redirectCalls != 0 {
		t.Fatal("credentials followed redirect")
	}
	for _, endpoint := range []string{"http://example.com/events", "https://user:secret@example.com/events", "https://example.com/events?token=secret", "https://example.com/events#fragment", ""} {
		app.Env.InsiderLifecycleEndpoint = endpoint
		if _, err := lifecycleEndpoint(app); err == nil {
			t.Errorf("unsafe endpoint accepted: %s", endpoint)
		}
	}
}
