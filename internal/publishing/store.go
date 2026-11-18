package publishing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ DB *pgxpool.Pool }

func (s Store) Channels(ctx context.Context) ([]Channel, error) {
	rows, e := s.DB.Query(ctx, `SELECT id,platform,name,provider,remote_id,timezone,slots,paused,credentials IS NOT NULL,route_event_posts FROM publishing_channels WHERE provider IN ('direct','recordings') ORDER BY platform,name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		var c Channel
		if e = rows.Scan(&c.ID, &c.Platform, &c.Name, &c.Provider, &c.RemoteID, &c.Timezone, &c.Slots, &c.Paused, &c.Connected, &c.RouteEventPosts); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func lockChannel(ctx context.Context, tx pgx.Tx, id string) error {
	var v string
	return tx.QueryRow(ctx, `SELECT id FROM publishing_channels WHERE id=$1 FOR UPDATE`, id).Scan(&v)
}
func (s Store) SaveChannel(ctx context.Context, c Channel) error {
	if e := ValidateSlots(c.Timezone, c.Slots); e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lockChannel(ctx, tx, c.ID); e != nil {
		return e
	}
	slots, _ := json.Marshal(c.Slots)
	tag, e := tx.Exec(ctx, `UPDATE publishing_channels SET timezone=$2,slots=$3,paused=$4,route_event_posts=$5,updated_at=now() WHERE id=$1 AND provider='direct'`, c.ID, c.Timezone, slots, c.Paused, c.RouteEventPosts)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("Manage this account in its publishing service")
	}
	if e = reflow(ctx, tx, c.ID, time.Now().UTC()); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func reflow(ctx context.Context, tx pgx.Tx, id string, now time.Time) error {
	var zone string
	var slots []Slot
	if e := tx.QueryRow(ctx, `SELECT timezone,slots FROM publishing_channels WHERE id=$1`, id).Scan(&zone, &slots); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, `SELECT id::text,mode,scheduled_at,state FROM publishing_deliveries WHERE channel_id=$1 AND state IN ('scheduled','sending') ORDER BY queue_order,id`, id)
	if e != nil {
		return e
	}
	var queued []string
	used := map[int64]bool{}
	for rows.Next() {
		var key, mode, state string
		var at *time.Time
		if e = rows.Scan(&key, &mode, &at, &state); e != nil {
			rows.Close()
			return e
		}
		if mode == "queue" && state == "scheduled" {
			queued = append(queued, key)
		} else if at != nil {
			used[at.Unix()] = true
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(queued) == 0 {
		return nil
	}
	candidates := SlotsBetween(zone, slots, now.Add(time.Minute), now.AddDate(2, 0, 0))
	n := 0
	for _, at := range candidates {
		if used[at.Unix()] {
			continue
		}
		if n == len(queued) {
			break
		}
		_, e = tx.Exec(ctx, `UPDATE publishing_deliveries SET scheduled_at=$2,version=version+1,updated_at=now() WHERE id=$1 AND state='scheduled'`, queued[n], at)
		if e != nil {
			return e
		}
		n++
	}
	if n < len(queued) {
		return fmt.Errorf("Add enough weekly posting slots for this queue")
	}
	return nil
}

const deliveryColumns = `id::text,campaign_id::text,channel_id,conference_tag,coalesce(source_ref,''),title,body,reply,media,mode,state,scheduled_at,root_id,published_url,error,version,attempts`

func scanDelivery(row pgx.Row) (Delivery, error) {
	var d Delivery
	e := row.Scan(&d.ID, &d.CampaignID, &d.ChannelID, &d.Conference, &d.SourceRef, &d.Title, &d.Body, &d.Reply, &d.Media, &d.Mode, &d.State, &d.At, &d.RootID, &d.URL, &d.Error, &d.Version, &d.Attempts)
	return d, e
}
func (s Store) Deliveries(ctx context.Context) ([]Delivery, error) {
	rows, e := s.DB.Query(ctx, `SELECT `+deliveryColumns+` FROM publishing_deliveries WHERE state NOT IN ('published','canceled') OR updated_at>now()-interval '30 days' ORDER BY scheduled_at NULLS LAST,queue_order LIMIT 1000`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		d, e := scanDelivery(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s Store) Save(ctx context.Context, d Delivery) (string, error) {
	if e := ValidateDelivery(d, time.Now()); e != nil {
		return "", e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	id, e := saveDelivery(ctx, tx, d)
	if e != nil {
		return "", e
	}
	return id, tx.Commit(ctx)
}

// saveDelivery shares a transaction when one composer submission has several destinations.
func saveDelivery(ctx context.Context, tx pgx.Tx, d Delivery) (string, error) {
	if e := ValidateDelivery(d, time.Now()); e != nil {
		return "", e
	}
	var e error
	if e = lockChannel(ctx, tx, d.ChannelID); e != nil {
		return "", e
	}
	var supported bool
	if e = tx.QueryRow(ctx, `SELECT provider='direct' AND platform='x' FROM publishing_channels WHERE id=$1`, d.ChannelID).Scan(&supported); e != nil {
		return "", e
	}
	if !supported {
		return "", fmt.Errorf("This account does not support direct social posts; use the recordings workflow for YouTube")
	}
	if d.Conference != "" {
		var ok bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conferences WHERE tag=$1)`, d.Conference).Scan(&ok); e != nil {
			return "", e
		}
		if !ok {
			return "", fmt.Errorf("Unknown event")
		}
	}
	state := "scheduled"
	if d.Mode == "draft" {
		state = "draft"
		d.At = nil
	}
	if d.Mode == "queue" {
		d.At = nil
	}
	media, _ := json.Marshal(d.Media)
	if d.Media == nil {
		media = []byte("[]")
	}
	if d.ID == "" {
		if d.CampaignID == "" {
			d.CampaignID = uuid.NewString()
		}
		e = tx.QueryRow(ctx, `INSERT INTO publishing_deliveries(channel_id,conference_tag,source_ref,title,body,reply,media,mode,state,scheduled_at,campaign_id) VALUES($1,$2,nullif($3,''),$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(channel_id,source_ref) DO NOTHING RETURNING id::text`, d.ChannelID, d.Conference, d.SourceRef, d.Title, d.Body, d.Reply, media, d.Mode, state, d.At, d.CampaignID).Scan(&d.ID)
		if e == pgx.ErrNoRows {
			return "", fmt.Errorf("This item is already in this account's queue")
		}
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, e = tx.Exec(ctx, `UPDATE publishing_deliveries SET conference_tag=$3,title=$4,body=$5,reply=$6,media=$7,mode=$8,state=$9,scheduled_at=$10,version=version+1,updated_at=now(),error='' WHERE id=$1 AND channel_id=$2 AND version=$11 AND state IN ('draft','scheduled','failed') AND root_id=''`, d.ID, d.ChannelID, d.Conference, d.Title, d.Body, d.Reply, media, d.Mode, state, d.At, d.Version)
		if e == nil && tag.RowsAffected() != 1 {
			return "", ErrChanged
		}
	}
	if e != nil {
		return "", e
	}
	if d.Mode == "fixed" {
		var clash bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM publishing_deliveries WHERE channel_id=$1 AND id<>$2 AND state IN ('scheduled','sending') AND mode='fixed' AND scheduled_at=$3)`, d.ChannelID, d.ID, d.At).Scan(&clash)
		if e != nil {
			return "", e
		}
		if clash {
			return "", fmt.Errorf("This account already has a fixed post at that time")
		}
	}
	if e = reflow(ctx, tx, d.ChannelID, time.Now().UTC()); e != nil {
		return "", e
	}
	return d.ID, nil
}
func (s Store) Action(ctx context.Context, id string, version int, action, url string) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var channel string
	if e = tx.QueryRow(ctx, `SELECT channel_id FROM publishing_deliveries WHERE id=$1`, id).Scan(&channel); e != nil {
		return e
	}
	if e = lockChannel(ctx, tx, channel); e != nil {
		return e
	}
	d, e := scanDelivery(tx.QueryRow(ctx, `SELECT `+deliveryColumns+` FROM publishing_deliveries WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return e
	}
	if d.Version != version {
		return ErrChanged
	}
	query := ""
	args := []any{id}
	switch action {
	case "cancel":
		if d.State == "sending" || d.State == "published" || d.State == "needs_review" {
			return ErrChanged
		}
		query = `UPDATE publishing_deliveries SET state='canceled' WHERE id=$1`
	case "top":
		if d.Mode != "queue" || d.State != "scheduled" {
			return ErrChanged
		}
		query = `UPDATE publishing_deliveries SET queue_order=(SELECT coalesce(min(queue_order),0)-1 FROM publishing_deliveries WHERE channel_id=$2) WHERE id=$1`
		args = append(args, channel)
	case "retry", "confirm_not_sent":
		if (action == "retry" && d.State != "failed") || (action == "confirm_not_sent" && d.State != "needs_review") {
			return ErrChanged
		}
		query = `UPDATE publishing_deliveries SET state='scheduled',attempts=0,error='',started_at=NULL,lease_until=NULL,scheduled_at=now()+interval '1 minute' WHERE id=$1`
	case "confirm_sent":
		if d.State != "needs_review" || !ValidPostURL(url) {
			return fmt.Errorf("Enter the published X post URL after checking the account")
		}
		query = `UPDATE publishing_deliveries SET state='published',published_url=$2,error='' WHERE id=$1`
		args = append(args, url)
	default:
		return fmt.Errorf("Unknown action")
	}
	if _, e = tx.Exec(ctx, query, args...); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE publishing_deliveries SET version=version+1,updated_at=now() WHERE id=$1`, id); e != nil {
		return e
	}
	if e = reflow(ctx, tx, channel, time.Now().UTC()); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
