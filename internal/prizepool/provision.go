package prizepool

import (
	bip353 "btcpp-web/internal/bip353"
	"btcpp-web/internal/bip353/cloudflare"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Publisher interface {
	Get(context.Context, string) (bip353.Record, error)
	Put(context.Context, bip353.Entry) (bip353.Result, error)
}

func DNS(s Settings) (Publisher, error) {
	p, err := cloudflare.New(s.CFToken, s.CFZone)
	if err != nil {
		return nil, err
	}
	return bip353.NewManager(p, s.Domain)
}
func Create(ctx context.Context, db *pgxpool.Pool, confID, slug, description, actor string, s Settings) error {
	if err := ValidateSetup(slug, description); err != nil {
		return err
	}
	if s.Domain == "" {
		return errors.New("configure CLN and use a valid event slug first")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO community_prize_pools(conference_id,slug,domain,node_id,description) VALUES($1,$2,$3,$4,$5) ON CONFLICT(conference_id) DO UPDATE SET conference_id=excluded.conference_id WHERE community_prize_pools.slug=excluded.slug AND community_prize_pools.domain=excluded.domain AND community_prize_pools.description=excluded.description RETURNING id::text`, confID, slug, s.Domain, s.NodeID, description).Scan(&id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO community_pool_audit(pool_id,actor,action) VALUES($1,$2,'Create or recover pool')`, id, actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Serialize external provisioning using a dedicated session lock. Each completed step is durable.
func Provision(ctx context.Context, db *pgxpool.Pool, confID, actor string, s Settings, rpc RPC, dns Publisher) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "prize:"+confID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("pool setup is already running")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "prize:"+confID); e != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	p, err := Load(ctx, db, confID)
	if err != nil {
		return err
	}
	if p.Status != "draft" {
		return errors.New("only draft pools can be provisioned")
	}
	if err = VerifyNode(ctx, rpc, s); err != nil {
		return err
	}
	if p.NodeID == "" && s.Enabled() {
		if _, err = conn.Exec(ctx, `UPDATE community_prize_pools SET node_id=$2 WHERE conference_id=$1 AND node_id=''`, confID, s.NodeID); err != nil {
			return err
		}
		p.NodeID = s.NodeID
	}
	if p.NodeID != s.NodeID || p.Domain != s.Domain {
		return errors.New("pool destination differs from configured node or domain")
	}
	if p.OfferID == "" {
		var offer struct {
			ID     string `json:"offer_id"`
			Bolt12 string `json:"bolt12"`
			Active bool   `json:"active"`
		}
		// Offer content includes stable, event-specific description/issuer; identical retry recovers it.
		err = rpc.Call(ctx, "offer", map[string]any{"amount": "any", "description": p.Description, "issuer": p.Domain, "label": "btcpp-prize:" + p.ID}, &offer)
		if err != nil {
			return err
		}
		if !offer.Active || !hashPattern.MatchString(offer.ID) || !strings.HasPrefix(offer.Bolt12, "lno1") {
			return errors.New("invalid CLN offer")
		}
		if _, err = db.Exec(ctx, `UPDATE community_prize_pools SET offer_id=$2,offer=$3 WHERE id=$1`, p.ID, offer.ID, offer.Bolt12); err != nil {
			return err
		}
		p.OfferID = offer.ID
		p.Offer = offer.Bolt12
	}
	var endpoints struct {
		Endpoints []struct{ Name, Description string } `json:"endpoints"`
	}
	if err = rpc.Call(ctx, "clnurl-list", map[string]any{}, &endpoints); err != nil {
		return err
	}
	exists := false
	for _, e := range endpoints.Endpoints {
		if e.Name == p.Slug {
			if e.Description != p.Description {
				return errors.New("existing clnurl address has a different description; resolve before provisioning")
			}
			exists = true
		}
	}
	if !exists {
		var result map[string]any
		if err = rpc.Call(ctx, "clnurl-add", map[string]any{"name": p.Slug, "description": p.Description}, &result); err != nil {
			return err
		}
	}
	if _, err = db.Exec(ctx, `UPDATE community_prize_pools SET endpoint_created=true WHERE id=$1`, p.ID); err != nil {
		return err
	}
	existing, lookupErr := dns.Get(ctx, p.Slug)
	if lookupErr != nil && !errors.Is(lookupErr, bip353.ErrNotFound) {
		return lookupErr
	}
	if lookupErr == nil && existing.Content != "bitcoin:?lno="+p.Offer {
		return errors.New("BIP353 address already points elsewhere; refusing to overwrite")
	}
	if _, err = dns.Put(ctx, bip353.Entry{User: p.Slug, URI: "bitcoin:?lno=" + p.Offer, TTL: 300}); err != nil {
		return err
	}
	if _, err = db.Exec(ctx, `UPDATE community_prize_pools SET dns_published=true WHERE id=$1`, p.ID); err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO community_pool_audit(pool_id,actor,action) VALUES($1,$2,'Provisioned offer, LNURL endpoint, and DNS record')`, p.ID, actor)
	return err
}
func Open(ctx context.Context, db *pgxpool.Pool, confID, actor string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `UPDATE community_prize_pools SET status='open' WHERE conference_id=$1 AND status='draft' AND dns_published AND endpoint_created AND offer_id<>'' RETURNING id::text`, confID).Scan(&id)
	if err != nil {
		return errors.New("complete pool setup before opening")
	}
	_, err = tx.Exec(ctx, `INSERT INTO community_pool_audit(pool_id,actor,action) VALUES($1,$2,'Opened funding after administrator verified DNSSEC and LNURL routing')`, id, actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func ClosePool(ctx context.Context, db *pgxpool.Pool, confID, actor string, rpc RPC) error {
	// Cutoff is fixed before any network calls; retrying must not move it.
	_, err := db.Exec(ctx, `UPDATE community_prize_pools SET status='closing',closed_at=coalesce(closed_at,now()) WHERE conference_id=$1 AND status IN ('open','closing')`, confID)
	if err != nil {
		return err
	}
	p, err := Load(ctx, db, confID)
	if err != nil {
		return err
	}
	if p.Status != "closing" {
		return errors.New("pool is not open or closing")
	}
	var out map[string]any
	if err = rpc.Call(ctx, "disableoffer", map[string]any{"offer_id": p.OfferID}, &out); err != nil {
		return fmt.Errorf("pool closing; retry disabling offer: %w", err)
	}
	var endpoints struct {
		Endpoints []struct{ Name string } `json:"endpoints"`
	}
	if err = rpc.Call(ctx, "clnurl-list", map[string]any{}, &endpoints); err != nil {
		return err
	}
	for _, e := range endpoints.Endpoints {
		if e.Name == p.Slug {
			if err = rpc.Call(ctx, "clnurl-remove", map[string]any{"name": p.Slug}, &out); err != nil {
				return err
			}
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE community_prize_pools SET status='closed' WHERE id=$1`, p.ID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO community_pool_audit(pool_id,actor,action) VALUES($1,$2,'Closed funding; later payments retained separately for review')`, p.ID, actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ValidateSetup(slug, description string) error {
	if !slugPattern.MatchString(slug) {
		return errors.New("address name must be 1–64 lowercase letters, numbers, dots, underscores or hyphens, starting with a letter or number")
	}
	if strings.TrimSpace(description) == "" || len(description) > 500 {
		return errors.New("invoice description must contain 1–500 bytes")
	}
	return nil
}
