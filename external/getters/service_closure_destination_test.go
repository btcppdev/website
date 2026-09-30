package getters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/internal/bitcoinpayout"
	"github.com/google/uuid"
)

func closureDestinationNode(t *testing.T, chain string, calls *atomic.Int64) *bitcoinpayout.Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "getblockchaininfo":
			result = map[string]any{"chain": chain, "blocks": 100, "headers": 100, "bestblockhash": strings.Repeat("c", 64), "time": time.Now().Unix(), "initialblockdownload": false}
		case "validateaddress":
			address := request.Params[0]
			script := "0014" + strings.Repeat("11", 20)
			if address == "bc1fixture-second" {
				script = "0014" + strings.Repeat("22", 20)
			}
			result = map[string]any{"isvalid": address != "invalid", "address": address, "scriptPubKey": script}
		default:
			t.Errorf("unexpected RPC %s", request.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "payout-verification", "result": result})
	}))
	t.Cleanup(s.Close)
	node, err := bitcoinpayout.New(s.URL, "fixture", "fixture", chain)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestBitcoinClosureDestinationPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		t.Run(product, func(t *testing.T) {
			ctx := context.Background()
			zero := int64(0)
			app, refund, closure := bitcoinRefundProductFixture(t, &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}, product)
			var actor string
			if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Destination reviewer') RETURNING id::text`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int64
			node := closureDestinationNode(t, "main", &calls)
			reviewID := uuid.NewString()
			if _, err := ApproveBitcoinClosureDestination(ctx, app, node, actor, reviewID, "bc1fixture", "Recipient confirmed address"); !errors.Is(err, ErrServiceRefundAdmin) || calls.Load() != 0 {
				t.Fatalf("unauthorized approval: %v calls=%d", err, calls.Load())
			}
			if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
				t.Fatal(err)
			}
			if _, err := ReviewBitcoinClosureRefund(ctx, app, actor, reviewID, refund.AttemptID, closure, 500, "Reviewed amount"); err != nil {
				t.Fatal(err)
			}
			if _, err := ApproveBitcoinClosureDestination(ctx, app, node, actor, reviewID, "invalid", "Recipient confirmed address"); !errors.Is(err, bitcoinpayout.ErrUnverified) {
				t.Fatalf("invalid address: %v", err)
			}
			// Production cannot freeze a test-network payout destination.
			regtest := closureDestinationNode(t, "regtest", &calls)
			production := *app.Env
			production.Prod = true
			prodApp := *app
			prodApp.Env = &production
			if _, err := ApproveBitcoinClosureDestination(ctx, &prodApp, regtest, actor, reviewID, "bc1fixture", "Recipient confirmed address"); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("production testnet approval: %v", err)
			}
			var wg sync.WaitGroup
			results := make(chan *BitcoinClosureDestination, 2)
			failures := make(chan error, 2)
			for _, address := range []string{"bc1fixture", "bc1fixture-second"} {
				wg.Add(1)
				go func(address string) {
					defer wg.Done()
					approval, err := ApproveBitcoinClosureDestination(ctx, app, node, actor, reviewID, address, "Recipient confirmed address")
					if err != nil {
						failures <- err
					} else {
						results <- approval
					}
				}(address)
			}
			wg.Wait()
			close(results)
			close(failures)
			if len(results) != 1 || len(failures) != 1 {
				t.Fatalf("conflicting approvals: successes=%d failures=%d", len(results), len(failures))
			}
			if err := <-failures; !errors.Is(err, ErrServiceRefund) {
				t.Fatal(err)
			}
			first := <-results
			replay, err := ApproveBitcoinClosureDestination(ctx, app, node, actor, reviewID, first.Destination.Address, first.Reason)
			if err != nil || *first != *replay {
				t.Fatalf("approval replay changed: %+v %v", replay, err)
			}
			stored, err := InspectBitcoinClosureDestination(ctx, app, reviewID)
			if err != nil || *first != *stored {
				t.Fatalf("stored approval: %+v %v", stored, err)
			}
			if _, err := ApproveBitcoinClosureDestination(ctx, app, node, actor, reviewID, first.Destination.Address, "Different reason"); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("reason changed: %v", err)
			}
			for _, query := range []string{`UPDATE service_bitcoin_closure_destinations SET destination='changed' WHERE review_id=$1`, `DELETE FROM service_bitcoin_closure_destinations WHERE review_id=$1`} {
				if _, err := app.DB.Exec(ctx, query, reviewID); err == nil {
					t.Fatal("approval was mutable")
				}
			}
			var count int
			if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1`, refund.AttemptID).Scan(&count); err != nil || count != 0 {
				t.Fatal("destination approval recorded a refund")
			}
			staleReview := uuid.NewString()
			if _, err := ReviewBitcoinClosureRefund(ctx, app, actor, staleReview, refund.AttemptID, closure, 500, "Reviewed before another refund"); err != nil {
				t.Fatal(err)
			}
			paid := int64(200)
			refund.AmountSatoshis = &paid
			if err := RecordServiceRefund(ctx, app, refund); err != nil {
				t.Fatal(err)
			}
			if _, err := ApproveBitcoinClosureDestination(ctx, app, node, actor, staleReview, "bc1fixture", "Recipient confirmed address"); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("stale review approval: %v", err)
			}
		})
	}
}
