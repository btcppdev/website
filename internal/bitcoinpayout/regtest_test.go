package bitcoinpayout

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Runs a new isolated node with no peers and only regtest coins. Opt in with
// BITCOINPAYOUT_REGTEST_BINARY=/path/to/bitcoind; no production RPC is consulted.
func TestCoreRegtestPayout(t *testing.T) {
	binary := os.Getenv("BITCOINPAYOUT_REGTEST_BINARY")
	if binary == "" {
		t.Skip("set BITCOINPAYOUT_REGTEST_BINARY for real node verification")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "bitcoin.conf"), []byte("regtest=1\nserver=1\nlisten=0\ndiscover=0\ndnsseed=0\nrpcuser=fixture\nrpcpassword=regtest-only\n[regtest]\nconnect=0\nrpcbind=127.0.0.1\nrpcallowip=127.0.0.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "node.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-datadir="+dir, fmt.Sprintf("-rpcport=%d", port), "-printtoconsole=1", "-fallbackfee=0.00001")
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(filepath.Join(dir, "node.log"))
			t.Log(string(raw))
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := New(fmt.Sprintf("http://127.0.0.1:%d", port), "fixture", "regtest-only", "regtest")
	if err != nil {
		t.Fatal(err)
	}
	var info chainInfo
	for {
		if err = c.call(ctx, "getblockchaininfo", []any{}, &info); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("node startup timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	call := func(method string, params []any, out any) {
		t.Helper()
		if err := c.call(ctx, method, params, out); err != nil {
			t.Fatal(method, err)
		}
	}
	var ignored json.RawMessage
	call("createwallet", []any{"fixture"}, &ignored)
	var miningAddress, recipient string
	call("getnewaddress", []any{}, &miningAddress)
	call("getnewaddress", []any{}, &recipient)
	var mined []string
	call("generatetoaddress", []any{101, miningAddress}, &mined)
	var txid string
	call("sendtoaddress", []any{recipient, json.Number("0.00123456")}, &txid)
	call("generatetoaddress", []any{1, miningAddress}, &mined)
	block := mined[0]
	var tx struct {
		Outputs []struct {
			N      uint32 `json:"n"`
			Script struct {
				Address string `json:"address"`
			} `json:"scriptPubKey"`
		} `json:"vout"`
	}
	call("getrawtransaction", []any{txid, 1, block}, &tx)
	var index uint32
	found := false
	for _, output := range tx.Outputs {
		if output.Script.Address == recipient {
			index = output.N
			found = true
		}
	}
	if !found {
		t.Fatal("recipient output absent")
	}
	req := Request{TxID: txid, Vout: index, BlockHash: block, Destination: recipient, Satoshis: 123456}
	if _, err = c.Verify(ctx, req); err == nil {
		t.Fatal("one confirmation accepted")
	}
	call("generatetoaddress", []any{5, miningAddress}, &mined)
	evidence, err := c.Verify(ctx, req)
	if err != nil || evidence.Confirmations != 6 || evidence.Satoshis != 123456 {
		t.Fatalf("real payout: %+v %v", evidence, err)
	}
	wrong := req
	wrong.Satoshis++
	if _, err = c.Verify(ctx, wrong); err == nil {
		t.Fatal("wrong amount accepted")
	}
	wrong = req
	wrong.Destination = miningAddress
	if _, err = c.Verify(ctx, wrong); err == nil {
		t.Fatal("wrong recipient accepted")
	}
	// A recipient spending the payout must not erase proof that we paid them.
	var raw, signed string
	call("createrawtransaction", []any{[]any{map[string]any{"txid": txid, "vout": index}}, map[string]any{miningAddress: json.Number("0.00120000")}}, &raw)
	var signature struct {
		Hex      string `json:"hex"`
		Complete bool   `json:"complete"`
	}
	call("signrawtransactionwithwallet", []any{raw}, &signature)
	if !signature.Complete {
		t.Fatal("fixture signing failed")
	}
	call("sendrawtransaction", []any{signature.Hex}, &signed)
	call("generatetoaddress", []any{1, miningAddress}, &mined)
	if _, err = c.Verify(ctx, req); err != nil {
		t.Fatal("spent payout rejected", err)
	}
	call("invalidateblock", []any{block}, nil)
	if _, err = c.Verify(ctx, req); err == nil {
		t.Fatal("orphaned payout accepted")
	}
	call("reconsiderblock", []any{block}, nil)
	if _, err = c.Verify(ctx, req); err != nil {
		t.Fatal("reconfirmed payout rejected", err)
	}
	c.chain = "main"
	if _, err = c.Verify(ctx, req); err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatal("wrong network accepted", err)
	}
}
