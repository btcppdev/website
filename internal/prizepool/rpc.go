package prizepool

import (
	"btcpp-web/internal/lnsocket"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Settings struct{ Host, NodeID, Rune, ProvisionRune, Domain, Network, CFToken, CFZone string }

func Environment() Settings {
	s := Settings{Host: os.Getenv("PRIZE_CLN_HOST"), NodeID: strings.ToLower(os.Getenv("PRIZE_CLN_NODE_ID")), Rune: os.Getenv("PRIZE_CLN_MONITOR_RUNE"), ProvisionRune: os.Getenv("PRIZE_CLN_PROVISION_RUNE"), Domain: os.Getenv("PRIZE_ADDRESS_DOMAIN"), Network: os.Getenv("PRIZE_CLN_NETWORK"), CFToken: os.Getenv("PRIZE_CLOUDFLARE_TOKEN"), CFZone: os.Getenv("PRIZE_CLOUDFLARE_ZONE")}
	if s.Domain == "" {
		s.Domain = "btcplusplus.dev"
	}
	if s.Network == "" {
		s.Network = "bitcoin"
	}
	return s
}
func (s Settings) Enabled() bool { return s.Host != "" && s.NodeID != "" && s.Rune != "" }

type RPC interface {
	Call(context.Context, string, any, any) error
}
type Commando struct{ Host, NodeID, Rune string }
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return fmt.Sprintf("CLN RPC rejected request (code %d)", e.Code) }
func (c Commando) Call(ctx context.Context, method string, params, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := &lnsocket.LNSocket{}
	if err := client.ConnectContext(ctx, c.Host, c.NodeID); err != nil {
		return fmt.Errorf("CLN connection failed: %w", err)
	}
	// Close the dedicated socket on cancellation, including stalled init and RPC writes.
	conn := client.Conn
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer client.Close()
	if err := client.PerformInitContext(ctx); err != nil {
		return err
	}
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	raw, err := client.RpcContext(ctx, c.Rune, method, string(b))
	if err != nil {
		return err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err = json.Unmarshal([]byte(raw), &envelope); err != nil {
		return errors.New("invalid CLN response")
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	if len(envelope.Result) == 0 {
		return errors.New("missing CLN result")
	}
	return json.Unmarshal(envelope.Result, out)
}
func VerifyNode(ctx context.Context, rpc RPC, s Settings) error {
	var info struct{ ID, Network string }
	if err := rpc.Call(ctx, "getinfo", map[string]any{}, &info); err != nil {
		return err
	}
	if info.ID != s.NodeID || info.Network != s.Network {
		return errors.New("CLN node identity or network mismatch")
	}
	return nil
}
