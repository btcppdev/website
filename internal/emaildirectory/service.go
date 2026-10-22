package emaildirectory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/mail"
	"strings"
	"time"
)

var ErrInput = errors.New("invalid directory request")
var ErrConflict = errors.New("preference changed; reload and try again")

type Store struct{ DB *pgxpool.Pool }
type Membership struct {
	Service      string     `json:"service"`
	Newsletter   string     `json:"newsletter"`
	Status       string     `json:"status"`
	Version      int64      `json:"version"`
	DeliveryMode string     `json:"delivery_mode"`
	PausedUntil  *time.Time `json:"paused_until,omitempty"`
}
type Contact struct {
	ID          string       `json:"id"`
	Email       string       `json:"email"`
	Memberships []Membership `json:"memberships"`
}
type Change struct {
	Newsletter      string     `json:"newsletter"`
	Subscribed      bool       `json:"subscribed"`
	ExpectedVersion int64      `json:"expected_version"`
	DeliveryMode    string     `json:"delivery_mode"`
	PausedUntil     *time.Time `json:"paused_until,omitempty"`
}

func ValidScope(service, list string) bool {
	switch service {
	case "alerts":
		return list == "releases" || list == "checkout-reminders"
	case "insider":
		switch list {
		case "insider.last-week-in-bitcoin", "insider.bitcoin-core", "insider.deep-dives", "insider.interviews", "insider.podcasts", "insider.friday-show", "insider.editorials":
			return true
		}
	case "btcpp":
		return list != "" && list != "insider" && len(list) <= 200 && !strings.ContainsAny(list, "!\r\n\t")
	}
	return false
}
func NormalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 320 {
		return "", ErrInput
	}
	return value, nil
}

// Resolve only establishes address identity. It never grants consent or account ownership.
func (s Store) Resolve(ctx context.Context, email, service string) (*Contact, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	var id string
	err = s.DB.QueryRow(ctx, `INSERT INTO email_contacts(email) VALUES($1) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id::text`, email).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id, service)
}
func (s Store) Get(ctx context.Context, id, service string) (*Contact, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrInput
	}
	c := &Contact{ID: id, Memberships: []Membership{}}
	if err := s.DB.QueryRow(ctx, `SELECT email::text FROM email_contacts WHERE id=$1 AND erased_at IS NULL`, id).Scan(&c.Email); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT service,newsletter,status,version,delivery_mode,paused_until FROM email_memberships WHERE contact_id=$1 AND ($2='' OR service=$2) ORDER BY service,newsletter`, id, service)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Membership
		if err = rows.Scan(&m.Service, &m.Newsletter, &m.Status, &m.Version, &m.DeliveryMode, &m.PausedUntil); err != nil {
			return nil, err
		}
		c.Memberships = append(c.Memberships, m)
	}
	return c, rows.Err()
}

// Change is a compare-and-swap batch. A replay never restores an older preference.
// importOnly seeds missing memberships; existing central decisions always win.
func (s Store) Change(ctx context.Context, id, service, key string, changes []Change, importOnly bool) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrInput
	}
	if len(key) < 16 || len(key) > 128 || len(changes) == 0 || len(changes) > 50 {
		return ErrInput
	}
	seen := map[string]bool{}
	for i := range changes {
		c := &changes[i]
		if !ValidScope(service, c.Newsletter) || seen[c.Newsletter] || c.ExpectedVersion < 0 {
			return ErrInput
		}
		seen[c.Newsletter] = true
		if c.DeliveryMode == "" {
			c.DeliveryMode = "as_published"
		}
		if c.DeliveryMode != "as_published" && c.DeliveryMode != "weekly_digest" {
			return ErrInput
		}
		if c.DeliveryMode == "weekly_digest" && (service != "insider" || (c.Newsletter != "insider.deep-dives" && c.Newsletter != "insider.interviews" && c.Newsletter != "insider.podcasts")) {
			return ErrInput
		}
	}
	payload, _ := json.Marshal(struct {
		ID      string
		Changes []Change
		Import  bool
	}{id, changes, importOnly})
	digest := sha256.Sum256(payload)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize both central requests and legacy subscriber updates through the contact row.
	var email string
	if err = tx.QueryRow(ctx, `SELECT email::text FROM email_contacts WHERE id=$1 AND erased_at IS NULL FOR UPDATE`, id).Scan(&email); err != nil {
		return err
	}
	var prior []byte
	err = tx.QueryRow(ctx, `SELECT request_hash FROM email_preference_requests WHERE service=$1 AND request_key=$2`, service, key).Scan(&prior)
	if err == nil {
		if string(prior) != string(digest[:]) {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('btcpp.directory_write','yes',true)`); err != nil {
		return err
	}
	for _, c := range changes {
		var version int64
		err = tx.QueryRow(ctx, `SELECT version FROM email_memberships WHERE contact_id=$1 AND service=$2 AND newsletter=$3 FOR UPDATE`, id, service, c.Newsletter).Scan(&version)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if importOnly && version > 0 {
			continue
		}
		if !importOnly && version != c.ExpectedVersion {
			return ErrConflict
		}
		status := "unsubscribed"
		if c.Subscribed {
			status = "subscribed"
		}
		source := "directory"
		if importOnly {
			source = "product-import"
		}
		_, err = tx.Exec(ctx, `INSERT INTO email_memberships(contact_id,service,newsletter,status,source,consented_at,unsubscribed_at,delivery_mode,paused_until)
 VALUES($1,$2,$3,$4,$5,CASE WHEN $4='subscribed' AND $5<>'product-import' THEN now() END,CASE WHEN $4='unsubscribed' THEN now() END,$6,$7)
 ON CONFLICT(contact_id,service,newsletter) DO UPDATE SET status=excluded.status,source=excluded.source,
 consented_at=CASE WHEN excluded.status='subscribed' AND email_memberships.status<>'subscribed' THEN now() ELSE email_memberships.consented_at END,
 unsubscribed_at=CASE WHEN excluded.status='unsubscribed' THEN coalesce(email_memberships.unsubscribed_at,now()) END,
 delivery_mode=excluded.delivery_mode,paused_until=excluded.paused_until,version=email_memberships.version+1,updated_at=now()
 WHERE (email_memberships.status,email_memberships.delivery_mode,email_memberships.paused_until) IS DISTINCT FROM (excluded.status,excluded.delivery_mode,excluded.paused_until)`, id, service, c.Newsletter, status, source, c.DeliveryMode, c.PausedUntil)
		if err != nil {
			return err
		}
		if service == "btcpp" {
			if c.Subscribed {
				_, err = tx.Exec(ctx, `WITH s AS (INSERT INTO subscribers(email) VALUES($1) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id) INSERT INTO subscriber_subscriptions(subscriber_id,name) SELECT id,$2 FROM s ON CONFLICT DO NOTHING`, email, c.Newsletter)
			} else {
				_, err = tx.Exec(ctx, `DELETE FROM subscriber_subscriptions WHERE contact_id=$1 AND name=$2`, id, c.Newsletter)
			}
			if err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO email_preference_requests(service,request_key,request_hash,contact_id) VALUES($1,$2,$3,$4)`, service, key, digest[:], id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Account struct {
	PersonID  string `json:"person_id"`
	ContactID string `json:"contact_id"`
	Email     string `json:"email"`
	Created   bool   `json:"created"`
}

// EnsureVerifiedAccount trusts only a service that has verified the mailbox using
// its own single-use confirmation challenge. It never returns a central login token.
func (s Store) EnsureVerifiedAccount(ctx context.Context, email, service, proof string) (*Account, error) {
	if service != "insider" || len(proof) < 32 || len(proof) > 128 {
		return nil, ErrInput
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	// A used proof remains consumed after account deletion. An old worker cannot
	// recreate an erased account by retrying a previous confirmation.
	var prior Account
	err = s.DB.QueryRow(ctx, `SELECT coalesce(p.person_id::text,''),p.contact_id::text,c.email::text FROM email_account_proofs p JOIN email_contacts c ON c.id=p.contact_id WHERE p.service=$1 AND p.proof_id=$2`, service, proof).Scan(&prior.PersonID, &prior.ContactID, &prior.Email)
	if err == nil {
		if prior.PersonID == "" || prior.Email != email {
			return nil, ErrConflict
		}
		return &prior, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	c, err := s.Resolve(ctx, email, service)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM email_contacts WHERE id=$1 FOR UPDATE`, c.ID); err != nil {
		return nil, err
	}
	out := &Account{ContactID: c.ID, Email: c.Email}
	var priorContact string
	err = tx.QueryRow(ctx, `SELECT contact_id::text,coalesce(person_id::text,'') FROM email_account_proofs WHERE service=$1 AND proof_id=$2`, service, proof).Scan(&priorContact, &out.PersonID)
	if err == nil {
		if priorContact != c.ID || out.PersonID == "" {
			return nil, ErrConflict
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var conflicts bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM person_email_conflicts WHERE email=$1)`, c.Email).Scan(&conflicts); err != nil {
		return nil, err
	}
	if conflicts {
		return nil, ErrConflict
	}
	err = tx.QueryRow(ctx, `SELECT pe.person_id::text FROM person_emails pe JOIN people p ON p.id=pe.person_id WHERE pe.contact_id=$1 AND NOT p.is_deleted_account`, c.ID).Scan(&out.PersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.QueryRow(ctx, `INSERT INTO people(name,norm_photo_path) VALUES('bitcoin++ member','default.avif') RETURNING id::text`).Scan(&out.PersonID); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at,contact_id) VALUES($1,$2,true,now(),$3)`, out.PersonID, c.Email, c.ID)
		out.Created = true
	} else if err == nil {
		_, err = tx.Exec(ctx, `UPDATE person_emails SET verified_at=coalesce(verified_at,now()) WHERE contact_id=$1`, c.ID)
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO email_account_proofs(service,proof_id,contact_id,person_id) VALUES($1,$2,$3,$4)`, service, proof, c.ID, out.PersonID); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
