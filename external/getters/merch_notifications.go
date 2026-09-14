package getters

import "btcpp-web/internal/config"

// Claim one notice for ten minutes. Crashed workers release it through lease
// expiry; each recipient also gets a stable mailer idempotency key.
func ClaimMerchSaleNotification(ctx *config.AppContext) (string, error) {
	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `WITH candidate AS (
 SELECT order_id FROM merch_sale_notifications WHERE sent_at IS NULL AND next_attempt_at<=now()
 ORDER BY next_attempt_at,order_id LIMIT 1 FOR UPDATE SKIP LOCKED
 ) UPDATE merch_sale_notifications n SET next_attempt_at=now()+interval '10 minutes'
 FROM candidate c WHERE n.order_id=c.order_id RETURNING n.order_id::text`).Scan(&id)
	return id, err
}

func CompleteMerchSaleNotification(ctx *config.AppContext, orderID string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE merch_sale_notifications SET sent_at=now() WHERE order_id=$1`, orderID)
	return err
}

// One verified address per explicitly assigned merch admin. Global access does
// not implicitly subscribe someone to operational email.
func ListMerchAdminEmails(ctx *config.AppContext) ([]string, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT DISTINCT lower(email.email::text)
 FROM people_roles role JOIN LATERAL (
 SELECT email FROM person_emails WHERE person_id=role.person_id AND verified_at IS NOT NULL
 ORDER BY is_primary DESC,created_at,id LIMIT 1
 ) email ON true WHERE role.scope='merch' AND role.position='admin'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}
