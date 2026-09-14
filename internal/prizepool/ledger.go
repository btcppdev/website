package prizepool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type MSat int64

func (m *MSat) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		*m = MSat(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid msat amount")
	}
	n, err := strconv.ParseInt(strings.TrimSuffix(s, "msat"), 10, 64)
	*m = MSat(n)
	return err
}

type Invoice struct {
	PayerNote    string          `json:"invreq_payer_note"`
	PaidOutpoint json.RawMessage `json:"paid_outpoint"`
	Hash         string          `json:"payment_hash"`
	Label        string          `json:"label"`
	Status       string          `json:"status"`
	Index        int64           `json:"pay_index"`
	Amount       MSat            `json:"amount_received_msat"`
	PaidAt       int64           `json:"paid_at"`
	OfferID      string          `json:"local_offer_id"`
	Description  string          `json:"description"`
}

// Only ordinary LNURL metadata is eligible. Nostr requests and free text are not.
func Identifier(description string) string {
	var entries [][]string
	if len(description) > 16384 || json.Unmarshal([]byte(description), &entries) != nil {
		return ""
	}
	identifier := ""
	for _, e := range entries {
		if len(e) == 2 && e[0] == "text/identifier" {
			if identifier != "" {
				return ""
			}
			identifier = e[1]
		}
	}
	if strings.Count(identifier, "@") != 1 {
		return ""
	}
	p := strings.SplitN(identifier, "@", 2)
	if !slugPattern.MatchString(p[0]) || p[1] == "" || strings.ToLower(identifier) != identifier {
		return ""
	}
	return identifier
}

type Pool struct {
	EventTag                                                                    string // Routing belongs to the event, independently of the Lightning address.
	LinkedPrizeIDs                                                              []string
	ID, ConferenceID, Slug, Domain, NodeID, Description, OfferID, Offer, Status string
	DNSPublished, EndpointCreated                                               bool
	TotalMSat, Count, LateMSat                                                  int64
	SyncedAt                                                                    *time.Time
}

func (p Pool) Address() string { return p.Slug + "@" + p.Domain }
func (p Pool) Sats() string    { return FormatMSat(p.TotalMSat) }
func FormatMSat(n int64) string {
	if n%1000 == 0 {
		return strconv.FormatInt(n/1000, 10)
	}
	return fmt.Sprintf("%d.%03d", n/1000, n%1000)
}
func (p Pool) Stale() bool { return p.SyncedAt == nil || time.Since(*p.SyncedAt) > 90*time.Second }
func Load(ctx context.Context, db *pgxpool.Pool, confID string) (*Pool, error) {
	p := new(Pool)
	err := db.QueryRow(ctx, `SELECT p.id::text,p.conference_id::text,p.slug,p.domain,p.node_id,p.description,p.offer_id,p.offer,p.status,p.dns_published,p.endpoint_created,
 coalesce((SELECT sum(r.received_msat) FROM community_pool_credits c JOIN community_payment_receipts r USING(node_id,payment_hash) WHERE c.pool_id=p.id),0)::bigint,
 (SELECT count(*) FROM community_pool_credits c WHERE c.pool_id=p.id),
 coalesce((SELECT sum(r.received_msat) FROM community_pool_credits c JOIN community_payment_receipts r USING(node_id,payment_hash) WHERE c.pool_id=p.id AND c.late),0)::bigint,
 (SELECT synced_at FROM community_payment_cursors WHERE node_id=p.node_id),
 ARRAY(SELECT id::text FROM prizes WHERE community_pool_id=p.id)
 FROM community_prize_pools p WHERE conference_id=$1`, confID).Scan(&p.ID, &p.ConferenceID, &p.Slug, &p.Domain, &p.NodeID, &p.Description, &p.OfferID, &p.Offer, &p.Status, &p.DNSPublished, &p.EndpointCreated, &p.TotalMSat, &p.Count, &p.LateMSat, &p.SyncedAt, &p.LinkedPrizeIDs)
	if err != nil {
		return nil, err
	}
	return p, nil
}
func Credit(ctx context.Context, db *pgxpool.Pool, node string, in Invoice) error {
	if in.Status != "paid" || !hashPattern.MatchString(in.Hash) || in.Index <= 0 || in.Amount <= 0 || in.PaidAt <= 0 {
		return errors.New("invalid paid invoice")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize cursor/receipt changes even when several app instances observe the node.
	if _, err = tx.Exec(ctx, `INSERT INTO community_payment_cursors(node_id) VALUES($1) ON CONFLICT DO NOTHING`, node); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pay_index FROM community_payment_cursors WHERE node_id=$1 FOR UPDATE`, node); err != nil {
		return err
	}
	identifier := Identifier(in.Description)
	reason := ""
	if len(in.PaidOutpoint) > 0 && string(in.PaidOutpoint) != "null" {
		identifier = ""
		in.OfferID = ""
		reason = "on-chain receipt excluded from Lightning pool"
	}
	tag, err := tx.Exec(ctx, `INSERT INTO community_payment_receipts(node_id,payment_hash,pay_index,received_msat,paid_at,offer_id,identifier,review_reason,description,payer_note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, node, in.Hash, in.Index, int64(in.Amount), time.Unix(in.PaidAt, 0), in.OfferID, identifier, reason, in.Description, in.PayerNote)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var same bool
		err = tx.QueryRow(ctx, `SELECT pay_index=$3 AND received_msat=$4 AND paid_at=$5 AND offer_id=$6 AND identifier=$7 AND review_reason=$8 FROM community_payment_receipts WHERE node_id=$1 AND payment_hash=$2`, node, in.Hash, in.Index, int64(in.Amount), time.Unix(in.PaidAt, 0), in.OfferID, identifier, reason).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return errors.New("conflicting invoice replay; reconciliation required")
		}
	}
	if err = attribute(ctx, tx, node); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE community_payment_cursors SET pay_index=greatest(pay_index,$2) WHERE node_id=$1`, node, in.Index)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Revisit unmatched receipts after provisioning or mapping recovery. Never reassign a credit.
func attribute(ctx context.Context, tx pgx.Tx, node string) error {
	_, err := tx.Exec(ctx, `INSERT INTO community_pool_credits(node_id,payment_hash,pool_id,late)
 SELECT r.node_id,r.payment_hash,p.id,p.closed_at IS NOT NULL AND r.paid_at>=p.closed_at
 FROM community_payment_receipts r JOIN community_prize_pools p ON p.node_id=r.node_id
 AND ((r.offer_id<>'' AND r.offer_id=p.offer_id AND (r.identifier='' OR r.identifier=p.slug||'@'||p.domain))
 OR (r.offer_id='' AND r.identifier=p.slug||'@'||p.domain))
 WHERE r.node_id=$1 AND NOT EXISTS(SELECT 1 FROM community_pool_credits c WHERE c.node_id=r.node_id AND c.payment_hash=r.payment_hash) ON CONFLICT DO NOTHING`, node)
	return err
}
func Reattribute(ctx context.Context, db *pgxpool.Pool, node string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = attribute(ctx, tx, node); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Prize struct {
	ID, Title string
	Linked    bool
}

func ListPrizes(ctx context.Context, db *pgxpool.Pool, confID string) ([]Prize, error) {
	rows, err := db.Query(ctx, `SELECT p.id::text,p.title,p.community_pool_id IS NOT NULL FROM prizes p JOIN awards a ON a.id=p.award_id JOIN competitions c ON c.id=a.competition_id WHERE c.conference_id=$1 AND p.prize_type='pooled' ORDER BY p.title`, confID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var prizes []Prize
	for rows.Next() {
		var p Prize
		if err = rows.Scan(&p.ID, &p.Title, &p.Linked); err != nil {
			return nil, err
		}
		prizes = append(prizes, p)
	}
	return prizes, rows.Err()
}
func LinkPrize(ctx context.Context, db *pgxpool.Pool, confID, prizeID, actor, baseURI string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var poolID string
	err = tx.QueryRow(ctx, `UPDATE prizes p SET community_pool_id=pool.id,pool_url=$3||'/'||conf.tag||'/prize-pool'
 FROM awards a JOIN competitions c ON c.id=a.competition_id JOIN community_prize_pools pool ON pool.conference_id=c.conference_id JOIN conferences conf ON conf.id=c.conference_id
 WHERE p.award_id=a.id AND c.conference_id=$1 AND p.id=$2 AND p.prize_type='pooled'
 RETURNING pool.id::text`, confID, prizeID, strings.TrimRight(baseURI, "/")).Scan(&poolID)
	if err != nil {
		return errors.New("choose a pooled prize belonging to this event")
	}
	_, err = tx.Exec(ctx, `INSERT INTO community_pool_audit(pool_id,actor,action) VALUES($1,$2,$3)`, poolID, actor, "Linked hackathon prize "+prizeID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Goal advances only at confirmed whole-million boundaries, capped at ten million.
func (p Pool) GoalSats() int64 {
	g := (p.TotalMSat/1000000000 + 1) * 1000000
	if g > 10000000 {
		return 10000000
	}
	return g
}
func (p Pool) GoalLabel() string { return strconv.FormatInt(p.GoalSats()/1000000, 10) + "M" }
func (p Pool) Progress() int64 {
	if p.TotalMSat >= p.GoalSats()*1000 {
		return 100
	}
	v := p.TotalMSat * 100 / (p.GoalSats() * 1000)
	if v > 100 {
		return 100
	}
	return v
}
func (p Pool) Milestone() int64 {
	v := p.TotalMSat / 1000000000
	if v > 10 {
		return 10
	}
	return v
}
func (p Pool) SharePath() string {
	return "/" + p.RouteTag() + "/hackathon?prize=community#community-prize"
}

func (p Pool) GoalReached() bool { return p.Milestone() >= 10 }

func (p Pool) RouteTag() string {
	if p.EventTag != "" {
		return p.EventTag
	}
	return p.Slug
}

type Payment struct {
	Hash, Description, Note, OfferID string
	AmountMSat                       int64
	PaidAt                           time.Time
	Late                             bool
}

func (p Payment) Sats() string { return FormatMSat(p.AmountMSat) }
func (p Payment) Method() string {
	if p.OfferID != "" {
		return "BOLT12"
	}
	return "Lightning address"
}
func (p Payment) DescriptionText() string {
	var entries [][]string
	if json.Unmarshal([]byte(p.Description), &entries) == nil {
		for _, e := range entries {
			if len(e) == 2 && e[0] == "text/plain" {
				return e[1]
			}
		}
	}
	return p.Description
}

// A bounded, stable newest-first page. Totals are always computed from the entire ledger.
func ListPayments(ctx context.Context, db *pgxpool.Pool, poolID string, offset int) ([]Payment, error) {
	if offset < 0 {
		offset = 0
	}
	rows, err := db.Query(ctx, `SELECT r.payment_hash,r.description,r.payer_note,r.offer_id,r.received_msat,r.paid_at,c.late FROM community_pool_credits c JOIN community_payment_receipts r USING(node_id,payment_hash) WHERE c.pool_id=$1 ORDER BY r.paid_at DESC,r.payment_hash DESC LIMIT 51 OFFSET $2`, poolID, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var payments []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.Hash, &p.Description, &p.Note, &p.OfferID, &p.AmountMSat, &p.PaidAt, &p.Late); err != nil {
			return nil, err
		}
		payments = append(payments, p)
	}
	return payments, rows.Err()
}
