package getters

import (
	"btcpp-web/internal/config"
	"context"
	"github.com/jackc/pgx/v5"
	"net/url"
	"time"
)

type AlertsRenewalAccess struct {
	RenewalID  string    `json:"renewalId"`
	State      string    `json:"state"`
	Revision   int64     `json:"revision"`
	DueAt      time.Time `json:"dueAt"`
	PaidSlots  int64     `json:"paidSlots"`
	RenewalURL string    `json:"renewalUrl"`
}
type AccountAccessSnapshot struct {
	AccountID    string                `json:"accountId"`
	AsOf         time.Time             `json:"asOf"`
	Entitlements []ServiceEntitlement  `json:"entitlements"`
	Renewals     []AlertsRenewalAccess `json:"renewals"`
}

// Access and renewal state share a consistent database snapshot, including when
// invoice settlement changes both concurrently. The API supplies owner and time.
func AccountAccess(ctx context.Context, app *config.AppContext, person string, at time.Time) (*AccountAccessSnapshot, error) {
	tx, err := app.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	out := &AccountAccessSnapshot{AccountID: person, AsOf: at, Renewals: []AlertsRenewalAccess{}}
	out.Entitlements, err = queryServiceEntitlements(ctx, tx, person, at)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT r.invoice_id,r.state,r.revision,r.due_at,r.paid_slots FROM alerts_stripe_renewals r JOIN alerts_stripe_checkouts c ON c.id=r.checkout_id WHERE c.person_id=$1 ORDER BY r.due_at,r.invoice_id`, person)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r AlertsRenewalAccess
		if err = rows.Scan(&r.RenewalID, &r.State, &r.Revision, &r.DueAt, &r.PaidSlots); err != nil {
			rows.Close()
			return nil, err
		}
		r.RenewalURL = "https://btcpp.dev/services/alerts/renew/" + url.PathEscape(r.RenewalID)
		out.Renewals = append(out.Renewals, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
