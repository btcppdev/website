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
	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

const payoutAddress = "bc1fixture-refund"

var payoutScript = "0014" + strings.Repeat("11", 20)

func closurePayoutNode(t *testing.T, beforeTransaction func(), calls *atomic.Int64) *bitcoinpayout.Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "getblockchaininfo":
			result = map[string]any{"chain": "main", "blocks": 100, "headers": 100, "bestblockhash": strings.Repeat("c", 64), "time": time.Now().Unix(), "initialblockdownload": false}
		case "validateaddress":
			result = map[string]any{"isvalid": true, "address": payoutAddress, "scriptPubKey": payoutScript}
		case "getrawtransaction":
			if beforeTransaction != nil {
				beforeTransaction()
			}
			var txid, block string
			json.Unmarshal(request.Params[0], &txid)
			json.Unmarshal(request.Params[2], &block)
			result = map[string]any{"txid": txid, "blockhash": block, "in_active_chain": true, "vin": []any{map[string]any{"txid": strings.Repeat("d", 64)}}, "vout": []any{
				map[string]any{"n": 0, "value": json.Number("0.00000500"), "scriptPubKey": map[string]any{"hex": payoutScript}},
				map[string]any{"n": 1, "value": json.Number("0.00000500"), "scriptPubKey": map[string]any{"hex": payoutScript}},
			}}
		case "getblockheader":
			var hash string
			json.Unmarshal(request.Params[0], &hash)
			result = map[string]any{"hash": hash, "height": 95, "confirmations": 6}
		default:
			t.Errorf("unexpected RPC %s", request.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "payout-verification", "result": result})
	}))
	t.Cleanup(s.Close)
	node, err := bitcoinpayout.New(s.URL, "fixture", "fixture", "main")
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func closurePayoutFixture(t *testing.T, product string, node *bitcoinpayout.Client) (*config.AppContext, string, BitcoinClosurePayoutRequest, VerifiedServiceRefund) {
	t.Helper()
	ctx := context.Background()
	zero := int64(0)
	app, refund, closure := bitcoinRefundProductFixture(t, &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}, product)
	var actor string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Payout recorder') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
		t.Fatal(err)
	}
	review := uuid.NewString()
	if _, err := ReviewBitcoinClosureRefund(ctx, app, actor, review, refund.AttemptID, closure, 500, "Closure review"); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproveBitcoinClosureDestination(ctx, app, node, actor, review, payoutAddress, "Recipient confirmed address"); err != nil {
		t.Fatal(err)
	}
	// Each fixture has its own transaction; tests can deliberately reuse an
	// outpoint across products to exercise the shared allocation constraint.
	txid := strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
	return app, actor, BitcoinClosurePayoutRequest{ReviewID: review, TxID: txid, BlockHash: strings.Repeat("b", 64)}, refund
}

func TestBitcoinClosurePayoutPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		t.Run(product, func(t *testing.T) {
			ctx := context.Background()
			var calls atomic.Int64
			node := closurePayoutNode(t, nil, &calls)
			app, actor, req, refund := closurePayoutFixture(t, product, node)
			var wg sync.WaitGroup
			results := make(chan *BitcoinClosurePayout, 2)
			failures := make(chan error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					out, err := RecordBitcoinClosurePayout(ctx, app, node, actor, req)
					if err != nil {
						failures <- err
					} else {
						results <- out
					}
				}()
			}
			wg.Wait()
			close(results)
			close(failures)
			for err := range failures {
				t.Fatal(err)
			}
			if len(results) != 2 {
				t.Fatal("matching concurrent replay failed")
			}
			first, second := <-results, <-results
			if first.Refund.AmountCents != 499 || first.Refund.AmountSatoshis == nil || *first.Refund.AmountSatoshis != 500 || !first.RecordedAt.Equal(second.RecordedAt) || !first.Evidence.VerifiedAt.Equal(second.Evidence.VerifiedAt) {
				t.Fatalf("changed replay: %+v %+v", first, second)
			}
			before := calls.Load()
			if _, err := RecordBitcoinClosurePayout(ctx, app, node, actor, req); err != nil || calls.Load() != before {
				t.Fatal("replay needed another node call", err)
			}
			changed := req
			changed.TxID = strings.Repeat("f", 64)
			if _, err := RecordBitcoinClosurePayout(ctx, app, node, actor, changed); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("changed replay accepted: %v", err)
			}
			var ledger, jobs, outputs int
			if err := app.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1),(SELECT count(*) FROM service_refund_jobs WHERE attempt_id=$1),(SELECT count(*) FROM service_bitcoin_closure_payouts WHERE review_id=$2)`, refund.AttemptID, req.ReviewID).Scan(&ledger, &jobs, &outputs); err != nil || ledger != 1 || jobs != 1 || outputs != 1 {
				t.Fatalf("atomic records: %d %d %d %v", ledger, jobs, outputs, err)
			}
			for _, query := range []string{`UPDATE service_bitcoin_closure_payouts SET txid=repeat('a',64) WHERE review_id=$1`, `DELETE FROM service_bitcoin_closure_payouts WHERE review_id=$1`} {
				if _, err := app.DB.Exec(ctx, query, req.ReviewID); err == nil {
					t.Fatal("payout audit mutable")
				}
			}
			otherApp, otherActor, otherReq, otherRefund := closurePayoutFixture(t, "insider.membership.annual", node)
			otherReq.TxID = req.TxID
			if _, err := RecordBitcoinClosurePayout(ctx, otherApp, node, otherActor, otherReq); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("same output reused: %v", err)
			}
			if err := otherApp.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1`, otherRefund.AttemptID).Scan(&ledger); err != nil || ledger != 0 {
				t.Fatal("rejected output left a refund", ledger, err)
			}
		})
	}
}

func TestBitcoinClosurePayoutRevalidatesAfterRPCPostgres(t *testing.T) {
	for _, mode := range []string{"prior refund", "admin removed"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			var calls atomic.Int64
			basic := closurePayoutNode(t, nil, &calls)
			app, actor, req, refund := closurePayoutFixture(t, "alerts.repository-slots", basic)
			var once sync.Once
			node := closurePayoutNode(t, func() {
				once.Do(func() {
					if mode == "prior refund" {
						sats := int64(200)
						refund.AmountSatoshis = &sats
						refund.AmountCents = 199
						if err := RecordServiceRefund(ctx, app, refund); err != nil {
							t.Error(err)
						}
					} else {
						if _, err := app.DB.Exec(ctx, `DELETE FROM people_roles WHERE person_id=$1 AND scope='global' AND position='admin'`, actor); err != nil {
							t.Error(err)
						}
					}
				})
			}, &calls)
			_, err := RecordBitcoinClosurePayout(ctx, app, node, actor, req)
			if !errors.Is(err, ErrServiceRefund) && !errors.Is(err, ErrServiceRefundAdmin) {
				t.Fatalf("stale review/authority accepted: %v", err)
			}
			var outputs int
			if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_bitcoin_closure_payouts WHERE review_id=$1`, req.ReviewID).Scan(&outputs); err != nil || outputs != 0 {
				t.Fatal("stale approval recorded output", outputs, err)
			}
		})
	}
}

func TestBitcoinClosurePayoutRollbackPostgres(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int64
	node := closurePayoutNode(t, nil, &calls)
	app, actor, req, refund := closurePayoutFixture(t, "alerts.repository-slots", node)
	// A failure after the ledger and job inserts must not leave either committed.
	name := "closure_payout_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := app.DB.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture payout recording failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `CREATE TRIGGER `+name+` BEFORE INSERT ON service_bitcoin_closure_payouts FOR EACH ROW EXECUTE FUNCTION `+name+`()`); err != nil {
		t.Fatal(err)
	}
	defer app.DB.Exec(ctx, `DROP TRIGGER IF EXISTS `+name+` ON service_bitcoin_closure_payouts; DROP FUNCTION IF EXISTS `+name+`()`)
	if _, err := RecordBitcoinClosurePayout(ctx, app, node, actor, req); err == nil {
		t.Fatal("fixture failure ignored")
	}
	var count int
	var status string
	if err := app.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1)+(SELECT count(*) FROM service_refund_jobs WHERE attempt_id=$1),o.status FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id WHERE a.id=$1`, refund.AttemptID).Scan(&count, &status); err != nil || count != 0 || status != "paid" {
		t.Fatalf("partial commit: %d %s %v", count, status, err)
	}
	if _, err := app.DB.Exec(ctx, `DROP TRIGGER `+name+` ON service_bitcoin_closure_payouts`); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordBitcoinClosurePayout(ctx, app, node, actor, req); err != nil {
		t.Fatal("retry after rollback failed", err)
	}
}

func TestBitcoinClosureConcurrentOutputAllocationPostgres(t *testing.T) {
	for _, distinctOutputs := range []bool{false, true} {
		t.Run(map[bool]string{false: "same output", true: "distinct outputs"}[distinctOutputs], func(t *testing.T) {
			ctx := context.Background()
			var calls atomic.Int64
			node := closurePayoutNode(t, nil, &calls)
			firstApp, firstActor, firstReq, _ := closurePayoutFixture(t, "alerts.repository-slots", node)
			secondApp, secondActor, secondReq, _ := closurePayoutFixture(t, "insider.membership.annual", node)
			secondReq.TxID = firstReq.TxID
			if distinctOutputs {
				secondReq.Vout = 1
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := RecordBitcoinClosurePayout(ctx, firstApp, node, firstActor, firstReq)
				results <- err
			}()
			go func() {
				<-start
				_, err := RecordBitcoinClosurePayout(ctx, secondApp, node, secondActor, secondReq)
				results <- err
			}()
			close(start)
			var successes int
			for i := 0; i < 2; i++ {
				err := <-results
				if err == nil {
					successes++
				} else if !errors.Is(err, ErrServiceRefund) {
					t.Fatal(err)
				}
			}
			want := 1
			if distinctOutputs {
				want = 2
			}
			var payouts, refunds int
			if err := firstApp.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM service_bitcoin_closure_payouts WHERE chain='main' AND txid=$1),(SELECT count(*) FROM service_payment_refunds WHERE refund_reference LIKE $2)`, firstReq.TxID, "onchain:main:"+firstReq.TxID+":%").Scan(&payouts, &refunds); err != nil || successes != want || payouts != want || refunds != want {
				t.Fatalf("allocation: successes=%d payouts=%d refunds=%d want=%d error=%v", successes, payouts, refunds, want, err)
			}
		})
	}
}
