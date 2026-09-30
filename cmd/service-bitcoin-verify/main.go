// service-bitcoin-verify checks a manual payout against a reviewed amount. It
// produces evidence only: no payment, ledger write, access change, or email.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/bitcoinpayout"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	f := flag.NewFlagSet("service-bitcoin-verify", flag.ContinueOnError)
	reviewID := f.String("review", "", "immutable closure review UUID (required)")
	txid := f.String("txid", "", "paid transaction ID (required)")
	block := f.String("block", "", "containing block hash (required)")
	vout := f.Int64("vout", -1, "recipient output index (required, including zero)")
	chain := f.String("chain", "main", "expected Bitcoin Core network; defaults to main")
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if f.NArg() != 0 || *reviewID == "" || *txid == "" || *block == "" || *vout < 0 || *vout > math.MaxUint32 {
		return fmt.Errorf("-review, -txid, -block and -vout are required; use -help")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	env, err := envconfig.Load(".env")
	if err != nil {
		return fmt.Errorf("could not load environment")
	}
	node, err := bitcoinpayout.New(os.Getenv("BITCOIN_RPC_URL"), os.Getenv("BITCOIN_RPC_USER"), os.Getenv("BITCOIN_RPC_PASSWORD"), *chain)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, env.DatabaseURL)
	if err != nil {
		return fmt.Errorf("could not open commerce database")
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Env: env}
	review, err := getters.InspectBitcoinClosureReview(ctx, app, *reviewID)
	if err != nil {
		return fmt.Errorf("could not read closure review")
	}
	if review.Basis == nil || review.Basis.RemainingRefundSatoshis == nil {
		return fmt.Errorf("review has no executable amount")
	}
	destination, err := getters.InspectBitcoinClosureDestination(ctx, app, *reviewID)
	if err != nil {
		return fmt.Errorf("review requires an approved recipient destination")
	}
	if destination.Destination.Chain != *chain {
		return fmt.Errorf("approved destination network does not match configured network")
	}
	plan, err := getters.PlanBitcoinClosureRefund(ctx, app, review.Basis.AttemptID, review.Basis.ClosureAt)
	if err != nil {
		return fmt.Errorf("could not recheck closure basis")
	}
	before, _ := json.Marshal(review.Basis)
	current, _ := json.Marshal(plan)
	if !bytes.Equal(before, current) {
		return fmt.Errorf("closure review is stale; review current refund basis before proceeding")
	}
	evidence, err := node.Verify(ctx, bitcoinpayout.Request{TxID: *txid, Vout: uint32(*vout), BlockHash: *block, Destination: destination.Destination.Address, Satoshis: *review.Basis.RemainingRefundSatoshis})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if evidence.ScriptPubKey != destination.Destination.ScriptPubKey || evidence.Chain != destination.Destination.Chain {
		return fmt.Errorf("verified output does not match approved destination")
	}
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Review      *getters.BitcoinClosureReview      `json:"review"`
		Evidence    *bitcoinpayout.Evidence            `json:"evidence"`
		Destination *getters.BitcoinClosureDestination `json:"destination_approval"`
		Recorded    bool                               `json:"recorded"`
	}{review, evidence, destination, false})
}
