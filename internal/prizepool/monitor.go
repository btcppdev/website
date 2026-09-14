package prizepool

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"math/rand/v2"
	"time"
)

func Observe(ctx context.Context, db *pgxpool.Pool, rpc RPC, s Settings) error {
	if err := VerifyNode(ctx, rpc, s); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `INSERT INTO community_payment_cursors(node_id) VALUES($1) ON CONFLICT DO NOTHING`, s.NodeID); err != nil {
		return err
	}
	if err := Reattribute(ctx, db, s.NodeID); err != nil {
		return err
	}
	var cursor int64
	if err := db.QueryRow(ctx, `SELECT pay_index FROM community_payment_cursors WHERE node_id=$1`, s.NodeID).Scan(&cursor); err != nil {
		return err
	}
	var in Invoice
	err := rpc.Call(ctx, "waitanyinvoice", map[string]any{"lastpay_index": cursor, "timeout": 20}, &in)
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == 904 {
		_, err = db.Exec(ctx, `UPDATE community_payment_cursors SET synced_at=now() WHERE node_id=$1`, s.NodeID)
		return err
	}
	if err != nil {
		return err
	}
	// waitanyinvoice does not consistently include local_offer_id; fetch authoritative details.
	var result struct {
		Invoices []Invoice `json:"invoices"`
	}
	if err = rpc.Call(ctx, "listinvoices", map[string]any{"payment_hash": in.Hash}, &result); err != nil {
		return err
	}
	if len(result.Invoices) != 1 || result.Invoices[0].Hash != in.Hash || result.Invoices[0].Index <= cursor {
		return errors.New("unexpected CLN invoice lookup")
	}
	return Credit(ctx, db, s.NodeID, result.Invoices[0])
}
func Run(ctx context.Context, db *pgxpool.Pool, rpc RPC, s Settings, logger *log.Logger) {
	delay := time.Second
	for ctx.Err() == nil {
		iteration, cancel := context.WithTimeout(ctx, 75*time.Second)
		err := Observe(iteration, db, rpc, s)
		cancel()
		if err == nil {
			delay = time.Second
			continue
		}
		logger.Printf("Prize pool monitoring interrupted; retrying: %v", err)
		timer := time.NewTimer(delay + time.Duration(rand.IntN(500))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}
