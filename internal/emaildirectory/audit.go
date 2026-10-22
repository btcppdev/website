// Package emaildirectory implements the central address-directory migration audit.
package emaildirectory

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditReport deliberately contains counts only, never addresses or credentials.
type AuditReport struct {
	AccountAddresses                 int64 `json:"account_addresses"`
	SubscriberAddresses              int64 `json:"subscriber_addresses"`
	UniqueContacts                   int64 `json:"unique_contacts"`
	SharedContacts                   int64 `json:"contacts_in_both_sources"`
	AccountNormalizationConflicts    int64 `json:"account_normalization_conflicts"`
	SubscriberNormalizationConflicts int64 `json:"subscriber_normalization_conflicts"`
	EmptyAddresses                   int64 `json:"empty_addresses"`
	Memberships                      int64 `json:"memberships_to_import"`
	IgnoredInsiderMemberships        int64 `json:"ignored_insider_memberships"`
	PreferenceConflicts              int64 `json:"preference_conflicts"`
	CanMigrate                       bool  `json:"can_migrate"`
}

// Audit works before and after migration 129 in a read-only consistent snapshot.
// It covers the central database only; remote Insider contacts need a separate import.
func Audit(ctx context.Context, pool *pgxpool.Pool) (AuditReport, error) {
	var report AuditReport
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	// Production may not yet have the unmerged consent API migration (128).
	var hasConsentHistory bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('newsletter_consent_requests') IS NOT NULL`).Scan(&hasConsentHistory); err != nil {
		return report, err
	}
	consentQuery := `SELECT NULL::text AS email, NULL::text AS newsletter, false AS subscribed, NULL::timestamptz AS recorded_at WHERE false`
	if hasConsentHistory {
		consentQuery = `SELECT email,newsletter,subscribed,recorded_at FROM newsletter_consent_requests`
	}
	err = tx.QueryRow(ctx, `WITH consent_history AS (`+consentQuery+`), accounts AS (
 SELECT lower(btrim(email::text)) AS email FROM person_emails
 ), subscribers_normalized AS (SELECT lower(btrim(email::text)) AS email FROM subscribers),
 all_addresses AS (SELECT email FROM accounts UNION SELECT email FROM subscribers_normalized)
 SELECT
 (SELECT count(*) FROM accounts),
 (SELECT count(*) FROM subscribers_normalized),
 (SELECT count(*) FROM all_addresses),
 (SELECT count(*) FROM (SELECT email FROM accounts INTERSECT SELECT email FROM subscribers_normalized) shared),
 (SELECT count(*) FROM (SELECT email FROM accounts GROUP BY email HAVING count(*)>1) conflicts),
 (SELECT count(*) FROM (SELECT email FROM subscribers_normalized GROUP BY email HAVING count(*)>1) conflicts),
 (SELECT count(*) FROM (SELECT email FROM accounts UNION ALL SELECT email FROM subscribers_normalized) a WHERE email=''),
 (SELECT count(*) FROM subscriber_subscriptions WHERE name<>'insider'),
 (SELECT count(*) FROM subscriber_subscriptions WHERE name='insider'),
 (SELECT count(*) FROM (
 SELECT DISTINCT ON(lower(btrim(email::text)),newsletter) email,newsletter,subscribed
 FROM consent_history WHERE newsletter<>'insider'
 ORDER BY lower(btrim(email::text)),newsletter,recorded_at DESC,subscribed ASC
 ) latest JOIN subscribers s ON lower(btrim(s.email::text))=lower(btrim(latest.email::text))
 JOIN subscriber_subscriptions ss ON ss.subscriber_id=s.id AND ss.name=latest.newsletter
 WHERE NOT latest.subscribed)`).Scan(
		&report.AccountAddresses, &report.SubscriberAddresses, &report.UniqueContacts, &report.SharedContacts,
		&report.AccountNormalizationConflicts, &report.SubscriberNormalizationConflicts, &report.EmptyAddresses,
		&report.Memberships, &report.IgnoredInsiderMemberships, &report.PreferenceConflicts)
	if err != nil {
		return report, err
	}
	report.CanMigrate = report.AccountNormalizationConflicts == 0 && report.SubscriberNormalizationConflicts == 0 && report.EmptyAddresses == 0 && report.PreferenceConflicts == 0
	return report, tx.Commit(ctx)
}
