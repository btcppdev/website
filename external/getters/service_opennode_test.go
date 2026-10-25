package getters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOpenNodeServiceChargePostgres(t *testing.T) {
	for _, outcome := range []string{"created", "wrong-order", "redirect", "failure"} {
		t.Run(outcome, func(t *testing.T) {
			app := databaseSmokeContext(t)
			ctx := context.Background()
			var person string
			if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local provider adapter test') RETURNING id::text`).Scan(&person); err != nil {
				t.Fatal(err)
			}
			email := "adapter-" + person + "@example.test"
			if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, email); err != nil {
				t.Fatal(err)
			}
			quote := ServiceQuote{ProductCode: "insider.article", ResourceID: "adapter-test", Name: "Service item", Provider: "opennode", Quantity: 1, UnitCents: 1234, TotalCents: 1234, ExpiresAt: time.Now().Add(time.Hour)}
			purchase, err := CreateServicePurchase(app, person, "adapter-purchase-test", quote)
			if err != nil {
				t.Fatal(err)
			}
			chargeID := uuid.NewString()
			var calls atomic.Int32
			var createdAttempt string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method == "GET" && r.URL.Path == "/v2/charge/"+chargeID {
					json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": chargeID, "order_id": createdAttempt, "status": "unpaid", "currency": "USD", "source_fiat_value": 12.34, "hosted_checkout_url": "https://checkout.opennode.com/" + chargeID}})
					return
				}
				if r.Method != "POST" || r.URL.Path != "/v1/charges" || r.Header.Get("Authorization") != "local-provider-key" {
					t.Error("incorrect provider request")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if payload["description"] != "Bitcoin++ service order "+purchase.PublicID || payload["amount"] != "12.34" || payload["currency"] != "USD" || payload["customer_email"] != email || payload["callback_url"] != "http://localhost:8898/callback/services/opennode" {
					t.Error("trusted purchase data not preserved")
				}
				switch outcome {
				case "redirect":
					w.Header().Set("Location", "/must-not-follow")
					w.WriteHeader(307)
					return
				case "failure":
					w.WriteHeader(503)
					return
				case "wrong-order":
					payload["order_id"] = "another-attempt"
				}
				w.WriteHeader(201)
				createdAttempt, _ = payload["order_id"].(string)
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": chargeID, "order_id": payload["order_id"]}})
			}))
			defer server.Close()
			app.Env.OpenNode.Endpoint, app.Env.OpenNode.Key = server.URL+"/v1", "local-provider-key"
			app.Env.Host, app.Env.Port = "localhost", "8898"
			for i := 0; i < 2; i++ {
				ref, err := initOpenNodeServiceCharge(ctx, app, person, purchase.OrderID, server.Client())
				if outcome == "created" {
					if err != nil || ref != chargeID {
						t.Fatalf("charge creation/retry: %q %v", ref, err)
					}
				} else if !errors.Is(err, ErrServiceChargeReconciliation) || ref != "" {
					t.Fatalf("unverified charge accepted: %q %v", ref, err)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("provider called %d times, want 1", calls.Load())
			}
			if _, err := initOpenNodeServiceCharge(ctx, app, uuid.NewString(), purchase.OrderID, server.Client()); !errors.Is(err, ErrServicePayment) || calls.Load() != 1 {
				t.Fatal("cross-account charge request accepted")
			}
			var status string
			if err := app.DB.QueryRow(ctx, "SELECT status FROM shop_orders WHERE id=$1", purchase.OrderID).Scan(&status); err != nil || status != "pending" {
				t.Fatal("charge creation incorrectly settled the order", err)
			}
			if outcome == "created" {
				for i := 0; i < 2; i++ {
					destination, err := openNodeServiceCheckout(ctx, app, person, purchase.OrderID, server.Client())
					if err != nil || destination != "https://checkout.opennode.com/"+chargeID {
						t.Fatal("checkout retry", destination, err)
					}
				}
				if calls.Load() != 3 {
					t.Fatal("checkout retries created another charge")
				}
			}
		})
	}
}
