package bitcoinpayout

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testRequest = Request{TxID: strings.Repeat("a", 64), Vout: 1, BlockHash: strings.Repeat("b", 64), Destination: "bc1qfixture", Satoshis: 500}

func fixtureClient(t *testing.T, mutate func(string, int, map[string]any)) *Client {
	t.Helper()
	chainCalls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "fixture" || p != "fixture-secret" {
			t.Error("missing RPC authentication")
		}
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			t.Error("bad request")
			return
		}
		var result map[string]any
		switch req.Method {
		case "getblockchaininfo":
			chainCalls++
			result = map[string]any{"chain": "main", "blocks": 100, "headers": 100, "bestblockhash": strings.Repeat("c", 64), "initialblockdownload": false, "time": time.Now().Unix()}
		case "validateaddress":
			result = map[string]any{"isvalid": true, "address": testRequest.Destination, "scriptPubKey": "0014" + strings.Repeat("12", 20)}
		case "getrawtransaction":
			if len(req.Params) != 3 || string(req.Params[2]) != `"`+testRequest.BlockHash+`"` {
				t.Error("missing explicit containing block")
			}
			result = map[string]any{"txid": testRequest.TxID, "blockhash": testRequest.BlockHash, "in_active_chain": true, "vin": []any{map[string]any{"txid": strings.Repeat("d", 64)}}, "vout": []any{
				map[string]any{"n": 0, "value": json.Number("0.1"), "scriptPubKey": map[string]any{"hex": "51"}},
				map[string]any{"n": 1, "value": json.Number("0.00000500"), "scriptPubKey": map[string]any{"hex": "0014" + strings.Repeat("12", 20)}},
			}}
		case "getblockheader":
			result = map[string]any{"hash": testRequest.BlockHash, "height": 95, "confirmations": 6}
		default:
			t.Errorf("unexpected RPC %s", req.Method)
		}
		if mutate != nil {
			mutate(req.Method, chainCalls, result)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "payout-verification", "result": result})
	}))
	t.Cleanup(s.Close)
	c, err := New(s.URL, "fixture", "fixture-secret", "main")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestVerifyOutput(t *testing.T) {
	c := fixtureClient(t, nil)
	e, err := c.Verify(context.Background(), testRequest)
	if err != nil || e.Satoshis != 500 || e.Vout != 1 || e.Confirmations != 6 || e.BlockHeight != 95 || e.ObservedTip != strings.Repeat("c", 64) || e.VerifiedAt.IsZero() {
		t.Fatalf("evidence: %+v %v", e, err)
	}
}

func TestVerifyRejectsUnprovenPayout(t *testing.T) {
	tests := map[string]func(string, int, map[string]any){
		"wrong network": func(m string, n int, r map[string]any) {
			if m == "getblockchaininfo" {
				r["chain"] = "regtest"
			}
		},
		"initial sync": func(m string, n int, r map[string]any) {
			if m == "getblockchaininfo" {
				r["initialblockdownload"] = true
			}
		},
		"missing sync field": func(m string, n int, r map[string]any) {
			if m == "getblockchaininfo" {
				delete(r, "initialblockdownload")
			}
		},
		"behind headers": func(m string, n int, r map[string]any) {
			if m == "getblockchaininfo" {
				r["headers"] = 101
			}
		},
		"stale node": func(m string, n int, r map[string]any) {
			if m == "getblockchaininfo" {
				r["time"] = time.Now().Add(-25 * time.Hour).Unix()
			}
		},
		"invalid address": func(m string, n int, r map[string]any) {
			if m == "validateaddress" {
				r["isvalid"] = false
			}
		},
		"wrong txid": func(m string, n int, r map[string]any) {
			if m == "getrawtransaction" {
				r["txid"] = strings.Repeat("e", 64)
			}
		},
		"wrong block": func(m string, n int, r map[string]any) {
			if m == "getrawtransaction" {
				r["blockhash"] = strings.Repeat("e", 64)
			}
		},
		"orphaned": func(m string, n int, r map[string]any) {
			if m == "getrawtransaction" {
				r["in_active_chain"] = false
			}
		},
		"coinbase": func(m string, n int, r map[string]any) {
			if m == "getrawtransaction" {
				r["vin"] = []any{map[string]any{"coinbase": "01"}}
			}
		},
		"wrong output index": func(m string, n int, r map[string]any) {
			if m == "getrawtransaction" {
				r["vout"].([]any)[1].(map[string]any)["n"] = 0
			}
		},
		"wrong amount": func(m string, n int, r map[string]any) {
			if m == "getrawtransaction" {
				r["vout"].([]any)[1].(map[string]any)["value"] = json.Number("0.00000501")
			}
		},
		"wrong script": func(m string, n int, r map[string]any) {
			if m == "getrawtransaction" {
				r["vout"].([]any)[1].(map[string]any)["scriptPubKey"] = map[string]any{"hex": "51"}
			}
		},
		"insufficient confirmations": func(m string, n int, r map[string]any) {
			if m == "getblockheader" {
				r["confirmations"] = 5
			}
		},
		"reorg during lookup": func(m string, n int, r map[string]any) {
			if m == "getblockheader" {
				r["confirmations"] = -1
			}
		},
		"wrong height": func(m string, n int, r map[string]any) {
			if m == "getblockheader" {
				r["height"] = 94
			}
		},
		"tip changed": func(m string, n int, r map[string]any) {
			if m == "getblockchaininfo" && n == 2 {
				r["bestblockhash"] = strings.Repeat("d", 64)
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := fixtureClient(t, mutate)
			if e, err := c.Verify(context.Background(), testRequest); e != nil || !errors.Is(err, ErrUnverified) {
				t.Fatalf("accepted: %+v %v", e, err)
			}
		})
	}
}

func TestExactSatoshis(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
	}{{"0.00000001", 1}, {"1e-8", 1}, {"0.00000500", 500}, {"21000000", maxSatoshis}, {"0", 0}, {"-1", 0}, {"0.000000001", 0}, {"21000000.00000001", 0}, {"1e999999999", 0}, {"1/100000000", 0}} {
		got, err := decimalSatoshis(json.Number(tc.value))
		if tc.want == 0 {
			if err == nil {
				t.Errorf("accepted %s", tc.value)
			}
		} else if err != nil || got != tc.want {
			t.Errorf("%s: %d %v", tc.value, got, err)
		}
	}
}

func TestRPCFailureDoesNotExposeSecrets(t *testing.T) {
	for _, body := range []string{`{"jsonrpc":"2.0","id":"payout-verification","error":{"message":"fixture-secret"}}`, `{"jsonrpc":"2.0","id":"wrong","result":{}}`, `{"jsonrpc":"2.0","id":"payout-verification","result":{}} {}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		c, _ := New(s.URL, "fixture", "fixture-secret", "main")
		_, err := c.Verify(context.Background(), testRequest)
		s.Close()
		if !errors.Is(err, ErrUnverified) || strings.Contains(err.Error(), "fixture-secret") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, _ := New(source.URL, "fixture", "fixture-secret", "main")
	if _, err := c.Verify(context.Background(), testRequest); err == nil || called {
		t.Fatal("RPC redirect followed")
	}
}
