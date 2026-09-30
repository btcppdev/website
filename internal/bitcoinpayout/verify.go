// Package bitcoinpayout verifies manual on-chain payouts through a trusted
// Bitcoin Core node. It never signs, broadcasts, or changes wallet state.
package bitcoinpayout

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrUnverified = errors.New("Bitcoin payout could not be verified")

// Six confirmations is the initial operational policy, not finality. A later
// reorg still requires reconciliation; evidence is only an observation in time.
const RequiredConfirmations = 6
const maxSatoshis int64 = 21_000_000 * 100_000_000

type Request struct {
	TxID        string `json:"txid"`
	Vout        uint32 `json:"vout"`
	BlockHash   string `json:"block_hash"`
	Destination string `json:"destination"`
	Satoshis    int64  `json:"satoshis"`
}

type Evidence struct {
	Request
	Chain                 string    `json:"chain"`
	ScriptPubKey          string    `json:"script_pub_key"`
	BlockHeight           int64     `json:"block_height"`
	Confirmations         int64     `json:"confirmations"`
	RequiredConfirmations int64     `json:"required_confirmations"`
	ObservedTip           string    `json:"observed_tip"`
	VerifiedAt            time.Time `json:"verified_at"`
}

type Client struct {
	endpoint, username, password, chain string
	http                                *http.Client
	now                                 func() time.Time
}

// New requires explicit network selection and credentials. The endpoint belongs
// to deployment configuration, never an end-user request. Redirects are refused.
func New(endpoint, username, password, chain string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || username == "" || strings.Contains(username, ":") || password == "" {
		return nil, fmt.Errorf("%w: invalid RPC configuration", ErrUnverified)
	}
	switch chain {
	case "main", "regtest", "signet", "test", "testnet4":
	default:
		return nil, fmt.Errorf("%w: explicit network required", ErrUnverified)
	}
	return &Client{endpoint: endpoint, username: username, password: password, chain: chain,
		http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}, nil
}

func (c *Client) call(ctx context.Context, method string, params []any, result any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "payout-verification", "method": method, "params": params})
	if err != nil {
		return ErrUnverified
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return ErrUnverified
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.http.Do(req)
	// Transport errors can contain configured endpoints or credentials. Neither
	// those errors nor remote error messages are safe to include in admin output.
	if err != nil {
		return fmt.Errorf("%w: %s transport failed", ErrUnverified, method)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s HTTP %d", ErrUnverified, method, resp.StatusCode)
	}
	const limit = 16 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(raw) > limit {
		return ErrUnverified
	}
	var reply struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.JSONRPC != "2.0" || reply.ID != "payout-verification" || (len(reply.Error) > 0 && string(reply.Error) != "null") || len(reply.Result) == 0 || (result != nil && string(reply.Result) == "null") {
		return fmt.Errorf("%w: %s invalid RPC response", ErrUnverified, method)
	}
	if result == nil {
		return nil
	}
	if json.Unmarshal(reply.Result, result) != nil {
		return fmt.Errorf("%w: %s invalid result", ErrUnverified, method)
	}
	return nil
}

func validHex(s string, size int) bool {
	if len(s) == 0 || (size > 0 && len(s) != size*2) || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// decimalSatoshis never passes a Bitcoin amount through floating point. JSON
// scientific notation (used by Core for small values) is accepted exactly.
func decimalSatoshis(n json.Number) (int64, error) {
	s := n.String()
	if len(s) == 0 || len(s) > 32 || strings.ContainsAny(s, "/xX") {
		return 0, ErrUnverified
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exp, err := strconv.Atoi(s[i+1:])
		if err != nil || exp < -16 || exp > 16 {
			return 0, ErrUnverified
		}
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, ErrUnverified
	}
	r.Mul(r, big.NewRat(100_000_000, 1))
	if !r.IsInt() || !r.Num().IsInt64() {
		return 0, ErrUnverified
	}
	v := r.Num().Int64()
	if v <= 0 || v > maxSatoshis {
		return 0, ErrUnverified
	}
	return v, nil
}

type chainInfo struct {
	Chain   string `json:"chain"`
	Blocks  *int64 `json:"blocks"`
	Headers *int64 `json:"headers"`
	Tip     string `json:"bestblockhash"`
	Time    *int64 `json:"time"`
	IBD     *bool  `json:"initialblockdownload"`
}

func (c *Client) chainReady(info chainInfo) bool {
	if info.Chain != c.chain || info.IBD == nil || *info.IBD || info.Blocks == nil || info.Headers == nil || *info.Blocks != *info.Headers || *info.Blocks < 0 || !validHex(info.Tip, 32) || info.Time == nil {
		return false
	}
	age := c.now().Sub(time.Unix(*info.Time, 0))
	return age >= -2*time.Hour && age <= 24*time.Hour
}

// Verify proves an exact output match at a stable, sufficiently synced node tip.
// Spent outputs remain valid payouts. It does not prove recipient ownership or
// authorize a refund; those must be bound to an approved order by the caller.
func (c *Client) Verify(ctx context.Context, req Request) (*Evidence, error) {
	if !validHex(req.TxID, 32) || !validHex(req.BlockHash, 32) || req.Satoshis <= 0 || req.Satoshis > maxSatoshis || len(req.Destination) < 1 || len(req.Destination) > 128 || strings.TrimSpace(req.Destination) != req.Destination {
		return nil, ErrUnverified
	}
	var before chainInfo
	if err := c.call(ctx, "getblockchaininfo", []any{}, &before); err != nil {
		return nil, err
	}
	if !c.chainReady(before) {
		return nil, fmt.Errorf("%w: node network or synchronization", ErrUnverified)
	}
	var address struct {
		Valid   bool   `json:"isvalid"`
		Address string `json:"address"`
		Script  string `json:"scriptPubKey"`
	}
	if err := c.call(ctx, "validateaddress", []any{req.Destination}, &address); err != nil {
		return nil, err
	}
	if !address.Valid || address.Address != req.Destination || !validHex(address.Script, 0) {
		return nil, fmt.Errorf("%w: destination", ErrUnverified)
	}
	var tx struct {
		TxID      string `json:"txid"`
		BlockHash string `json:"blockhash"`
		Active    bool   `json:"in_active_chain"`
		Vin       []struct {
			Coinbase string `json:"coinbase"`
		} `json:"vin"`
		Vout []struct {
			N      *uint32     `json:"n"`
			Value  json.Number `json:"value"`
			Script struct {
				Hex string `json:"hex"`
			} `json:"scriptPubKey"`
		} `json:"vout"`
	}
	if err := c.call(ctx, "getrawtransaction", []any{req.TxID, 1, req.BlockHash}, &tx); err != nil {
		return nil, err
	}
	if tx.TxID != req.TxID || tx.BlockHash != req.BlockHash || !tx.Active || len(tx.Vin) == 0 || uint64(req.Vout) >= uint64(len(tx.Vout)) {
		return nil, fmt.Errorf("%w: transaction inclusion", ErrUnverified)
	}
	for _, input := range tx.Vin {
		if input.Coinbase != "" {
			return nil, fmt.Errorf("%w: coinbase is not a refund", ErrUnverified)
		}
	}
	for i, output := range tx.Vout {
		if output.N == nil || uint64(*output.N) != uint64(i) {
			return nil, ErrUnverified
		}
	}
	output := tx.Vout[req.Vout]
	sats, err := decimalSatoshis(output.Value)
	if err != nil || sats != req.Satoshis || output.Script.Hex != address.Script {
		return nil, fmt.Errorf("%w: output amount or destination", ErrUnverified)
	}
	var header struct {
		Hash          string `json:"hash"`
		Height        *int64 `json:"height"`
		Confirmations int64  `json:"confirmations"`
	}
	if err := c.call(ctx, "getblockheader", []any{req.BlockHash, true}, &header); err != nil {
		return nil, err
	}
	if header.Hash != req.BlockHash || header.Height == nil || *header.Height < 0 || *header.Height > *before.Blocks || header.Confirmations < RequiredConfirmations || header.Confirmations != *before.Blocks-*header.Height+1 {
		return nil, fmt.Errorf("%w: confirmations", ErrUnverified)
	}
	var after chainInfo
	if err := c.call(ctx, "getblockchaininfo", []any{}, &after); err != nil {
		return nil, err
	}
	if !c.chainReady(after) || after.Tip != before.Tip || *after.Blocks != *before.Blocks {
		return nil, fmt.Errorf("%w: chain changed; retry verification", ErrUnverified)
	}
	return &Evidence{Request: req, Chain: c.chain, ScriptPubKey: address.Script, BlockHeight: *header.Height, Confirmations: header.Confirmations, RequiredConfirmations: RequiredConfirmations, ObservedTip: after.Tip, VerifiedAt: c.now().UTC()}, nil
}
