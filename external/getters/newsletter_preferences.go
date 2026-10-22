package getters

import (
	"errors"
	"strings"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

type NewsletterPreference struct {
	Email      string `json:"email"`
	Newsletter string `json:"newsletter"`
	Subscribed bool   `json:"subscribed"`
}

var ErrNewsletterEmail = errors.New("newsletter email must belong to the verified account")
var ErrNewsletterConflict = errors.New("idempotency key was already used with a different newsletter request")
var ErrNewsletterInput = errors.New("invalid newsletter preference")

func ListNewsletterPreferences(app *config.AppContext, personID string) ([]NewsletterPreference, error) {
	rows, err := app.DB.Query(app.DatabaseContext(), `SELECT c.email::text,n.name,EXISTS(SELECT 1 FROM email_memberships m WHERE m.contact_id=pe.contact_id AND m.service='btcpp' AND m.newsletter=n.name AND m.status='subscribed') FROM person_emails pe JOIN email_contacts c ON c.id=pe.contact_id CROSS JOIN (VALUES ('newsletter')) n(name) WHERE pe.person_id=$1 AND pe.verified_at IS NOT NULL ORDER BY pe.is_primary DESC,pe.email,n.name`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NewsletterPreference{}
	for rows.Next() {
		var p NewsletterPreference
		if err = rows.Scan(&p.Email, &p.Newsletter, &p.Subscribed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func SetNewsletterPreference(app *config.AppContext, personID, clientID, key string, p NewsletterPreference) error {
	p.Email = strings.TrimSpace(p.Email)
	if p.Newsletter != "newsletter" || len(key) < 16 || len(key) > 128 || len(p.Email) > 320 || p.Email == "" || len(clientID) > 200 {
		return ErrNewsletterInput
	}
	tx, err := app.DB.Begin(app.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(app.DatabaseContext())
	// Serialize retries and concurrent choices for one account, including first use.
	if _, err = tx.Exec(app.DatabaseContext(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, personID); err != nil {
		return err
	}
	var verifiedEmail string
	err = tx.QueryRow(app.DatabaseContext(), `SELECT email::text FROM person_emails WHERE person_id=$1 AND email=$2 AND verified_at IS NOT NULL FOR SHARE`, personID, p.Email).Scan(&verifiedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNewsletterEmail
	}
	if err != nil {
		return err
	}
	var prior NewsletterPreference
	var source string
	err = tx.QueryRow(app.DatabaseContext(), `SELECT email::text,newsletter,subscribed,source_client FROM newsletter_consent_requests WHERE person_id=$1 AND request_key=$2`, personID, key).Scan(&prior.Email, &prior.Newsletter, &prior.Subscribed, &source)
	if err == nil {
		if !strings.EqualFold(prior.Email, verifiedEmail) || prior.Newsletter != p.Newsletter || prior.Subscribed != p.Subscribed || source != clientID {
			return ErrNewsletterConflict
		}
		return tx.Commit(app.DatabaseContext()) // A retry must not restore an old choice.
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if p.Subscribed {
		var subscriberID string
		if err = tx.QueryRow(app.DatabaseContext(), `INSERT INTO subscribers(email) VALUES($1) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id::text`, verifiedEmail).Scan(&subscriberID); err != nil {
			return err
		}
		if _, err = tx.Exec(app.DatabaseContext(), `INSERT INTO subscriber_subscriptions(subscriber_id,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, subscriberID, p.Newsletter); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(app.DatabaseContext(), `DELETE FROM subscriber_subscriptions ss USING subscribers s WHERE ss.subscriber_id=s.id AND s.email=$1 AND ss.name=$2`, verifiedEmail, p.Newsletter); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(app.DatabaseContext(), `INSERT INTO newsletter_consent_requests(person_id,request_key,email,newsletter,subscribed,source_client) VALUES($1,$2,$3,$4,$5,$6)`, personID, key, verifiedEmail, p.Newsletter, p.Subscribed, clientID); err != nil {
		return err
	}
	return tx.Commit(app.DatabaseContext())
}
