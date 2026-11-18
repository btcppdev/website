package publishing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"btcpp-web/external/secureblob"
	"btcpp-web/external/xapi"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
)

type Worker struct {
	Store     Store
	OAuth     *oauth2.Config
	Key       []byte
	X         xapi.Client
	ReadImage func(context.Context, string) ([]byte, error)
}

func (s Store) ConnectX(ctx context.Context, id, name string, encrypted []byte) error {
	_, e := s.DB.Exec(ctx, `INSERT INTO publishing_channels(id,platform,name,provider,remote_id,credentials) VALUES($1,'x',$2,'direct',$3,$4) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,credentials=EXCLUDED.credentials,updated_at=now()`, "x:"+id, "@"+name, id, encrypted)
	return e
}
func (w Worker) token(ctx context.Context, channel string) (string, error) {
	// Serialize refresh-token rotation against both workers and reconnects.
	tx, e := w.Store.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var encrypted []byte
	if e = tx.QueryRow(ctx, `SELECT credentials FROM publishing_channels WHERE id=$1 FOR UPDATE`, channel).Scan(&encrypted); e != nil {
		return "", e
	}
	raw, e := secureblob.Decrypt(encrypted, w.Key)
	if e != nil {
		return "", fmt.Errorf("Reconnect this X account")
	}
	var old oauth2.Token
	if json.Unmarshal(raw, &old) != nil {
		return "", fmt.Errorf("Reconnect this X account")
	}
	token, e := w.OAuth.TokenSource(ctx, &old).Token()
	if e != nil {
		return "", fmt.Errorf("X authorization expired; reconnect the account")
	}
	if token.AccessToken != old.AccessToken {
		raw, e = json.Marshal(token)
		if e != nil {
			return "", e
		}
		encrypted, e = secureblob.Encrypt(raw, w.Key)
		if e != nil {
			return "", e
		}
		if _, e = tx.Exec(ctx, `UPDATE publishing_channels SET credentials=$2,updated_at=now() WHERE id=$1`, channel, encrypted); e != nil {
			return "", e
		}
	}
	return token.AccessToken, tx.Commit(ctx)
}

// RunOnce permits one dispatcher across replicas. Persisted leases survive
// restarts. Once a POST begins, recovery requires review instead of guessing.
func (w Worker) RunOnce(ctx context.Context) error {
	conn, e := w.Store.DB.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(7330192403)`).Scan(&locked); e != nil || !locked {
		return e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(7330192403)`); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	if _, e = conn.Exec(ctx, `UPDATE publishing_deliveries SET state=CASE WHEN started_at IS NULL THEN 'scheduled' ELSE 'needs_review' END,error='Worker interrupted. Check X before retrying.',version=version+1,updated_at=now() WHERE state='sending' AND lease_until<now()`); e != nil {
		return e
	}
	for i := 0; i < 10; i++ {
		d, e := scanDelivery(conn.QueryRow(ctx, `UPDATE publishing_deliveries SET state='sending',lease_until=now()+interval '3 minutes',attempts=attempts+1,version=version+1,updated_at=now() WHERE id=(SELECT d.id FROM publishing_deliveries d JOIN publishing_channels c ON c.id=d.channel_id WHERE d.state='scheduled' AND d.scheduled_at<=now() AND NOT c.paused AND c.provider='direct' AND c.platform='x' AND c.credentials IS NOT NULL ORDER BY d.scheduled_at LIMIT 1 FOR UPDATE OF d SKIP LOCKED) RETURNING `+deliveryColumns))
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return nil
			}
			return e
		}
		jobCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
		e = w.deliver(jobCtx, &d)
		cancel()
		state, msg := "published", ""
		retryAt := d.At
		if e != nil {
			state = "failed"
			msg = e.Error()
			var api *xapi.APIError
			if errors.As(e, &api) && api.Ambiguous {
				state = "needs_review"
			} else if errors.As(e, &api) && api.Status == 429 && d.Attempts < 3 {
				state = "scheduled"
				t := time.Now().Add(time.Duration(d.Attempts) * 15 * time.Minute)
				retryAt = &t
			}
			// DB failures after dispatch must never make a POST retryable.
			var uncertain *uncertainError
			if errors.As(e, &uncertain) {
				state = "needs_review"
				msg = "Could not save the publishing result. Check X before retrying."
			}
		}
		if _, e = conn.Exec(ctx, `UPDATE publishing_deliveries SET state=$2,error=$3,published_url=$4,root_id=$5,scheduled_at=$6,lease_until=NULL,version=version+1,updated_at=now() WHERE id=$1 AND state='sending'`, d.ID, state, msg, d.URL, d.RootID, retryAt); e != nil {
			return e
		}
	}
	return nil
}

type uncertainError struct{ error }

func (w Worker) deliver(ctx context.Context, d *Delivery) error {
	token, e := w.token(ctx, d.ChannelID)
	if e != nil {
		return e
	}
	if d.RootID == "" {
		var media []string
		for _, m := range d.Media {
			if w.ReadImage == nil {
				return fmt.Errorf("Image storage unavailable")
			}
			raw, e := w.ReadImage(ctx, m.URL)
			if e != nil {
				return e
			}
			id, e := w.X.UploadImage(ctx, token, raw)
			if e != nil {
				return e
			}
			media = append(media, id)
		}
		if e = w.markStarted(ctx, d.ID); e != nil {
			return e
		}
		id, e := w.X.Post(ctx, token, d.Body, "", media)
		if e != nil {
			return e
		}
		d.RootID = id
		d.URL = xapi.PostURL(id)
		if _, e = w.Store.DB.Exec(ctx, `UPDATE publishing_deliveries SET root_id=$2,published_url=$3 WHERE id=$1 AND state='sending'`, d.ID, id, d.URL); e != nil {
			return &uncertainError{e}
		}
	}
	if d.Reply != "" {
		if e = w.markStarted(ctx, d.ID); e != nil {
			return &uncertainError{e}
		}
		if _, e = w.X.Post(ctx, token, d.Reply, d.RootID, nil); e != nil {
			return e
		}
	}
	return nil
}
func (w Worker) markStarted(ctx context.Context, id string) error {
	tag, e := w.Store.DB.Exec(ctx, `UPDATE publishing_deliveries SET started_at=now() WHERE id=$1 AND state='sending'`, id)
	if e == nil && tag.RowsAffected() != 1 {
		return ErrChanged
	}
	return e
}
