package handlers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"errors"
)

// Transactional billing notices use verified central account email addresses,
// independent of release-newsletter subscriptions. Nothing is auto-charged.
func StartAlertsPrepaidReminders(app *config.AppContext) {
	if os.Getenv("SERVICE_RECEIPTS_ENABLED") != "true" || !serviceReceiptMailReady(app) {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := processAlertsPrepaidReminder(ctx, app, time.Now())
			cancel()
			if err != nil {
				app.Err.Printf("Alerts prepaid reminder needs retry")
			}
		}
	}()
}

func processAlertsPrepaidReminder(ctx context.Context, app *config.AppContext, now time.Time) error {
	return processAlertsPrepaidReminderWithSender(ctx, app, now, func(m *emails.Mail) error { return emails.ComposeAndSendMail(app, m) })
}
func processAlertsPrepaidReminderWithSender(ctx context.Context, app *config.AppContext, now time.Time, send func(*emails.Mail) error) error {
	if !serviceReceiptMailReady(app) {
		return nil
	}
	if err := getters.ServiceSalesAvailable(ctx, app, "alerts.repository-slots"); errors.Is(err, getters.ErrServiceSalesClosed) {
		return nil
	} else if err != nil {
		return err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// One sender across app replicas. A failed commit resends the same mailer key.
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('alerts-prepaid-reminders',0))`).Scan(&locked); err != nil || !locked {
		return err
	}
	rows, err := tx.Query(ctx, `WITH candidates AS (
 SELECT c.person_id,b.id::text AS source, to_timestamp(b.period_end) AS due,b.quantity,
 CASE WHEN b.purchase_id IS NULL THEN '/services/alerts/payment-method?switch='||b.id::text ELSE '/services/orders/'||o.public_id END AS path
 FROM alerts_bitcoin_switches b JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id JOIN service_stripe_cancellations x ON x.id=b.id
 LEFT JOIN service_purchase_requests r ON r.id=b.purchase_id LEFT JOIN shop_orders o ON o.id=r.order_id
 WHERE b.state='scheduled' AND x.state='confirmed' AND (b.purchase_id IS NULL OR o.status='pending')
 UNION ALL
 SELECT r.person_id,o.id::text,e.ends_at,(SELECT sum(e2.quantity) FROM service_entitlements e2 WHERE e2.person_id=e.person_id AND e2.product_code=e.product_code AND e2.ends_at=e.ends_at AND e2.revoked_at IS NULL),
 '/services/alerts/prepay?account='||r.person_id::text||'&months=12&renew='||o.id::text
 FROM service_entitlements e JOIN service_purchase_requests r ON r.id=e.purchase_id JOIN shop_orders o ON o.id=r.order_id
 WHERE e.product_code='alerts.repository-slots' AND r.quote_snapshot->>'provider'='opennode' AND coalesce(r.quote_snapshot->>'base_order_id','')='' AND o.status='paid' AND e.revoked_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM service_entitlements future WHERE future.person_id=e.person_id AND future.product_code=e.product_code AND future.revoked_at IS NULL AND future.starts_at>=e.ends_at)
 AND NOT EXISTS(SELECT 1 FROM service_purchase_requests pending JOIN shop_orders po ON po.id=pending.order_id WHERE pending.person_id=r.person_id AND pending.quote_snapshot->>'alerts_renew_order_id'=o.id::text AND (po.status='paid' OR (po.status='pending' AND (po.checkout_expires_at>$1 OR EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=pending.id)))))
 ) SELECT c.person_id::text,c.source,c.due,c.quantity,c.path,p.email::text,
 CASE WHEN c.due<=$1::timestamptz+interval '1 day' THEN 1 ELSE 7 END AS window_days
 FROM candidates c JOIN LATERAL (SELECT email FROM person_emails WHERE person_id=c.person_id AND verified_at IS NOT NULL ORDER BY is_primary DESC,email LIMIT 1) p ON true
 WHERE c.due>$1 AND c.due<=$1::timestamptz+interval '7 days'
 AND NOT EXISTS(SELECT 1 FROM alerts_prepaid_reminders j WHERE j.person_id=c.person_id AND j.source_key=c.source AND j.window_days=CASE WHEN c.due<=$1::timestamptz+interval '1 day' THEN 1 ELSE 7 END AND (j.accepted_at IS NOT NULL OR j.next_attempt_at>$1))
 ORDER BY c.due LIMIT 1`, now)
	if err != nil {
		return err
	}
	var person, source, path, recipient string
	var due time.Time
	var quantity int64
	var window int
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	err = rows.Scan(&person, &source, &due, &quantity, &path, &recipient, &window)
	rows.Close()
	if err != nil {
		return err
	}
	if strings.Contains(path, "&renew=") {
		path += "&slots=" + strconv.FormatInt(quantity, 10)
	}
	link := strings.TrimRight(app.Env.GetURI(), "/") + path
	if _, err = url.ParseRequestURI(link); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alerts_prepaid_reminders(person_id,source_key,window_days,recipient) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, person, source, window, recipient)
	if err != nil {
		return err
	}
	var savedRecipient string
	if err = tx.QueryRow(ctx, `SELECT recipient FROM alerts_prepaid_reminders WHERE person_id=$1 AND source_key=$2 AND window_days=$3`, person, source, window).Scan(&savedRecipient); err != nil {
		return err
	}
	if savedRecipient != recipient {
		return tx.Commit(ctx)
	}
	text := fmt.Sprintf("Your Alerts paid access ends on %s. Bitcoin payments do not renew automatically. Prepay to keep your %d paid projects active. If unpaid, your three earliest-followed projects remain active.\n\nReview and pay: %s\n", due.UTC().Format("January 2, 2006"), quantity, link)
	key := fmt.Sprintf("alerts-prepaid:%x", sha256.Sum256([]byte(person+":"+source+":"+strconv.Itoa(window))))
	sendErr := send(&emails.Mail{JobKey: key, Email: recipient, Title: "Your Alerts Bitcoin renewal is coming up", SendAt: now, TextBody: []byte(text), HTMLBody: []byte("<p>" + strings.ReplaceAll(html.EscapeString(text), "\n", "<br>") + "</p><p><a href=\"" + html.EscapeString(link) + "\">Review and pay</a></p>")})
	if sendErr != nil {
		_, err = tx.Exec(ctx, `UPDATE alerts_prepaid_reminders SET next_attempt_at=$4 WHERE person_id=$1 AND source_key=$2 AND window_days=$3`, person, source, window, now.Add(time.Hour))
	} else {
		_, err = tx.Exec(ctx, `UPDATE alerts_prepaid_reminders SET accepted_at=$4 WHERE person_id=$1 AND source_key=$2 AND window_days=$3`, person, source, window, now)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return sendErr
}
