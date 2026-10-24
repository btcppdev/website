package getters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/jackc/pgx/v5"
)

type ExpeditionLifecycleCandidate struct {
	ID, Buyer, Recipient, Name, Policy, Zone string
	Number                                   int
	Due                                      time.Time
	Cents                                    int64
}

type ExpeditionLifecycleSnapshot struct {
	BookingID, Buyer, Recipient, Name, Policy, Zone string
	Number, Offset, Quantity, PaymentCount          int
	Due, Deadline, ForfeitAt, OccurredAt            time.Time
	Cents, RetainedCents                            int64
	Selections                                      []expedition.CabinSelection
	Payments                                        json.RawMessage
}

// Only paid, confirmed installment bookings with an explicitly approved policy
// are eligible. Unpaid checkout holds never become abandoned paid reservations.
func ExpeditionLifecycleCandidates(ctx context.Context, app *config.AppContext) ([]ExpeditionLifecycleCandidate, error) {
	rows, err := app.DB.Query(ctx, `SELECT r.id::text,r.buyer_id,o.buyer_email::text,COALESCE(pe.name,''),l.version,l.time_zone,i.number,i.due_at,i.cents
 FROM expedition_reservations r JOIN service_purchase_requests p ON p.request_key='expedition:'||r.id::text||':1' AND p.person_id::text=r.buyer_id
 JOIN shop_orders o ON o.id=p.order_id JOIN people pe ON pe.id=p.person_id
 JOIN expedition_lifecycle_policies l ON l.version=p.quote_snapshot->'legal_acceptance'->>'version'
 JOIN LATERAL(SELECT number,due_at,cents FROM expedition_installments WHERE reservation_id=r.id AND paid_order_id IS NULL ORDER BY number LIMIT 1)i ON true
 WHERE r.state='confirmed' AND r.released_at IS NULL AND r.plan='installments' AND i.number>1 AND o.status='paid'
 ORDER BY i.due_at,r.id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ExpeditionLifecycleCandidate
	for rows.Next() {
		var c ExpeditionLifecycleCandidate
		if err = rows.Scan(&c.ID, &c.Buyer, &c.Recipient, &c.Name, &c.Policy, &c.Zone, &c.Number, &c.Due, &c.Cents); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// Refresh only authenticated provider evidence. Ambiguous creation, partial or
// processing payments block forfeiture and replacement checkout, not just emails.
func ReconcileExpeditionPayments(ctx context.Context, app *config.AppContext, booking string) error {
	rows, err := app.DB.Query(ctx, `SELECT a.id::text,a.provider,COALESCE(a.provider_reference,'') FROM service_payment_attempts a
 JOIN service_purchase_requests p ON p.id=a.purchase_id
 WHERE p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2 AND a.settled_at IS NULL`, expedition.ProductCode, booking)
	if err != nil {
		return err
	}
	type attempt struct{ id, provider, ref string }
	var attempts []attempt
	for rows.Next() {
		var a attempt
		if err = rows.Scan(&a.id, &a.provider, &a.ref); err != nil {
			rows.Close()
			return err
		}
		attempts = append(attempts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range attempts {
		if a.provider != "opennode" || a.ref == "" {
			return fmt.Errorf("unresolved payment attempt requires review")
		}
		c, err := LookupOpenNodeServiceCharge(ctx, app, a.ref)
		if err != nil {
			return err
		}
		if c.Status == "paid" {
			if _, err = ReconcileOpenNodeServiceCharge(ctx, app, a.ref); err != nil {
				return err
			}
			continue
		}
		if c.Status == "expired" && ((c.MissingSatoshis != nil && *c.MissingSatoshis < c.PriceSatoshis) || (c.OverpaidSatoshis != nil && *c.OverpaidSatoshis > 0)) {
			c.Status = "expired_with_payment_review"
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err = app.DB.Exec(ctx, `INSERT INTO expedition_payment_checks(attempt_id,checked_at,status,evidence)VALUES($1,now(),$2,$3)
   ON CONFLICT(attempt_id)DO UPDATE SET checked_at=excluded.checked_at,status=excluded.status,evidence=excluded.evidence`, a.id, c.Status, raw); err != nil {
			return err
		}
		if c.Status != "expired" {
			return fmt.Errorf("payment still unresolved: %s", c.Status)
		}
	}
	return nil
}

// ProcessExpeditionLifecycle serializes against order creation, settlement and
// allocation. Event + inventory release + both notification jobs commit together.
func ProcessExpeditionLifecycle(ctx context.Context, app *config.AppContext, c ExpeditionLifecycleCandidate, now time.Time) (bool, error) {
	deadline, err := expedition.InstallmentDeadline(c.Due, c.Zone)
	if err != nil {
		return false, err
	}
	forfeit := deadline.AddDate(0, 0, 30)
	offset, due, err := expedition.LatestReminder(c.Due, now, c.Zone)
	if err != nil || !due {
		return false, err
	}
	kind := "reminder"
	if !now.Before(forfeit) {
		kind = "abandoned"
		offset = 30
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+c.Buyer); err != nil {
		return false, err
	}
	// Same order -> inventory -> reservation lock order as fulfillment.
	if _, err = tx.Exec(ctx, `SELECT o.id FROM shop_orders o JOIN service_purchase_requests p ON p.order_id=o.id WHERE p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2 ORDER BY o.id FOR UPDATE OF o`, expedition.ProductCode, c.ID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `SELECT singleton FROM expedition_settings WHERE singleton FOR UPDATE`); err != nil {
		return false, err
	}
	var state string
	var quantity int
	var schedule expedition.Schedule
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT state,quantity,schedule FROM expedition_reservations WHERE id=$1 AND buyer_id=$2 FOR UPDATE`, c.ID, c.Buyer).Scan(&state, &quantity, &raw); err != nil {
		return false, err
	}
	if state != "confirmed" {
		return false, nil
	}
	if err = json.Unmarshal(raw, &schedule); err != nil {
		return false, err
	}
	var next int
	var actualDue time.Time
	var cents int64
	err = tx.QueryRow(ctx, `SELECT number,due_at,cents FROM expedition_installments WHERE reservation_id=$1 AND paid_order_id IS NULL ORDER BY number LIMIT 1`, c.ID).Scan(&next, &actualDue, &cents)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if next != c.Number || !actualDue.Equal(c.Due) || cents != c.Cents {
		return false, nil
	}
	var blocked bool
	// Any unapplied payment, refund, removed ownership or uncertain provider attempt
	// must be reconciled before sending overdue messages or releasing a paid berth.
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id
 WHERE p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2 AND
 (p.person_id IS NULL OR o.status IN ('refunded','partially_refunded')
 OR (o.status='paid' AND NOT EXISTS(SELECT 1 FROM expedition_installments i WHERE i.reservation_id=$2::uuid AND i.paid_order_id=o.id::text))
 OR EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=p.id AND a.settled_at IS NOT NULL AND o.status<>'paid')
 OR EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=p.id AND a.settled_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM expedition_payment_checks pc WHERE pc.attempt_id=a.id AND pc.status='expired' AND pc.checked_at>$3::timestamptz-interval '5 minutes'))))`, expedition.ProductCode, c.ID, now).Scan(&blocked)
	if err != nil || blocked {
		return false, err
	}
	// Revalidate policy ownership under the locks, never apply today's policy to an old booking.
	var policyOK bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests p JOIN expedition_lifecycle_policies l ON l.version=p.quote_snapshot->'legal_acceptance'->>'version' WHERE p.request_key='expedition:'||$1||':1' AND p.person_id::text=$2 AND l.version=$3 AND l.time_zone=$4)`, c.ID, c.Buyer, c.Policy, c.Zone).Scan(&policyOK)
	if err != nil || !policyOK {
		return false, err
	}
	snapshot := ExpeditionLifecycleSnapshot{BookingID: c.ID, Buyer: c.Buyer, Recipient: c.Recipient, Name: c.Name, Policy: c.Policy, Zone: c.Zone, Number: next, PaymentCount: len(schedule.Installments), Offset: offset, Quantity: quantity, Due: c.Due, Deadline: deadline, ForfeitAt: forfeit, OccurredAt: now, Cents: c.Cents, Selections: schedule.Selections}
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(o.total_cents),0),COALESCE(jsonb_agg(jsonb_build_object('order',o.public_id,'total_cents',o.total_cents,'tax_cents',o.sales_tax_amount_cents,'provider',o.payment_provider,'provider_reference',o.payment_provider_id,'bitcoin',
 (SELECT e.opennode_bitcoin FROM service_payment_events e JOIN service_payment_attempts a ON a.id=e.attempt_id WHERE a.purchase_id=p.id AND a.settled_at IS NOT NULL LIMIT 1))),'[]'::jsonb)
 FROM expedition_installments i JOIN shop_orders o ON o.id::text=i.paid_order_id JOIN service_purchase_requests p ON p.order_id=o.id WHERE i.reservation_id=$1 AND o.status='paid'`, c.ID).Scan(&snapshot.RetainedCents, &snapshot.Payments)
	if err != nil {
		return false, err
	}
	raw, err = json.Marshal(snapshot)
	if err != nil {
		return false, err
	}
	var event string
	err = tx.QueryRow(ctx, `INSERT INTO expedition_lifecycle_events(reservation_id,installment,kind,offset_days,policy_version,occurred_at,snapshot)VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING RETURNING id::text`, c.ID, next, kind, offset, c.Policy, now, raw).Scan(&event)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Suppress older unsent reminders when catching up or abandoning.
	if _, err = tx.Exec(ctx, `UPDATE expedition_lifecycle_mail m SET state='suppressed' FROM expedition_lifecycle_events e WHERE e.id=m.event_id AND e.reservation_id=$1 AND e.kind='reminder' AND m.state='pending'`, c.ID); err != nil {
		return false, err
	}
	if kind == "abandoned" {
		if _, err = tx.Exec(ctx, `UPDATE expedition_reservations SET state='abandoned',released_at=$2 WHERE id=$1`, c.ID, now); err != nil {
			return false, err
		}
		// Existing immutable financial history is retained; only unpaid order entry is closed.
		if _, err = tx.Exec(ctx, `UPDATE shop_orders o SET status='cancelled' FROM service_purchase_requests p WHERE p.order_id=o.id AND p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2 AND o.status='pending'`, expedition.ProductCode, c.ID); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO expedition_lifecycle_mail(event_id,audience,recipient)VALUES($1,'staff','inbox@btcpp.dev')`, event); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO expedition_lifecycle_mail(event_id,audience,recipient)VALUES($1,'customer',$2)`, event, c.Recipient); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

type ExpeditionLifecycleMailJob struct {
	EventID, Audience, Recipient, Kind string
	Attempt                            int
	Snapshot                           ExpeditionLifecycleSnapshot
}

func ClaimExpeditionLifecycleMail(ctx context.Context, app *config.AppContext, now time.Time) (*ExpeditionLifecycleMailJob, error) {
	// Financial/ownership changes suppress unsent notices, including leased retries.
	_, err := app.DB.Exec(ctx, `UPDATE expedition_lifecycle_mail m SET state='suppressed' FROM expedition_lifecycle_events e,expedition_reservations r
 WHERE e.id=m.event_id AND r.id=e.reservation_id AND m.state='pending' AND
 (NOT EXISTS(SELECT 1 FROM service_purchase_requests p WHERE p.request_key='expedition:'||r.id::text||':1' AND p.person_id::text=r.buyer_id)
 OR (e.kind='reminder' AND (r.state<>'confirmed' OR EXISTS(SELECT 1 FROM expedition_installments i WHERE i.reservation_id=r.id AND i.number=e.installment AND i.paid_order_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id WHERE p.quote_snapshot->>'resource_id'=r.id::text AND p.quote_snapshot->>'product_code'=$1 AND o.status IN ('paid','refunded','partially_refunded') AND p.quote_snapshot->'fulfillment'->>'installment'=e.installment::text))))`, expedition.ProductCode)
	if err != nil {
		return nil, err
	}
	_, err = app.DB.Exec(ctx, `UPDATE expedition_lifecycle_mail SET state='review' WHERE state='pending' AND attempts>=12 AND lease_until<=$1`, now)
	if err != nil {
		return nil, err
	}
	var j ExpeditionLifecycleMailJob
	var raw []byte
	err = app.DB.QueryRow(ctx, `WITH due AS(SELECT event_id,audience FROM expedition_lifecycle_mail WHERE state='pending' AND next_attempt_at<=$1 AND lease_until<=$1 AND attempts<12 ORDER BY next_attempt_at,event_id,audience FOR UPDATE SKIP LOCKED LIMIT 1),claimed AS(
 UPDATE expedition_lifecycle_mail m SET attempts=m.attempts+1,lease_until=$1::timestamptz+interval '5 minutes' FROM due WHERE m.event_id=due.event_id AND m.audience=due.audience RETURNING m.*)
 SELECT c.event_id::text,c.audience,c.recipient,c.attempts,e.kind,e.snapshot FROM claimed c JOIN expedition_lifecycle_events e ON e.id=c.event_id`, now).Scan(&j.EventID, &j.Audience, &j.Recipient, &j.Attempt, &j.Kind, &raw)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &j.Snapshot)
	return &j, err
}
func FinishExpeditionLifecycleMail(ctx context.Context, app *config.AppContext, j *ExpeditionLifecycleMailJob, accepted bool, now time.Time) error {
	_, err := app.DB.Exec(ctx, `UPDATE expedition_lifecycle_mail SET state=CASE WHEN $4 THEN 'accepted' ELSE 'pending' END,
 accepted_at=CASE WHEN $4 THEN $5::timestamptz ELSE NULL END,lease_until=$5,next_attempt_at=$5::timestamptz+interval '10 minutes'
 WHERE event_id=$1 AND audience=$2 AND attempts=$3 AND state='pending'`, j.EventID, j.Audience, j.Attempt, accepted, now)
	return err
}

// Resolve existing installment orders before calculating a fresh quote. Unknown
// attempts are never replaced, even after local expiry. An expired, authenticated
// provider charge can be retired without modifying its historical quote.
func ExpeditionInstallmentOrder(ctx context.Context, app *config.AppContext, buyer, booking string, number int) (string, error) {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+buyer); err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `SELECT o.id::text,o.public_id,o.status,o.checkout_expires_at,
 EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=p.id AND (a.settled_at IS NOT NULL OR NOT EXISTS(SELECT 1 FROM expedition_payment_checks c WHERE c.attempt_id=a.id AND c.status='expired' AND c.checked_at>now()-interval '5 minutes')))
 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id WHERE p.person_id::text=$1 AND p.quote_snapshot->>'product_code'=$2 AND p.quote_snapshot->>'resource_id'=$3 AND p.quote_snapshot->'fulfillment'->>'installment'=$4
 ORDER BY o.created_at DESC FOR UPDATE OF o`, buyer, expedition.ProductCode, booking, fmt.Sprint(number))
	if err != nil {
		return "", err
	}
	type entry struct {
		id, public, status string
		expiry             time.Time
		unresolved         bool
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.public, &e.status, &e.expiry, &e.unresolved); err != nil {
			rows.Close()
			return "", err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.status == "paid" || e.unresolved || (e.status == "pending" && e.expiry.After(time.Now())) {
			return e.public, tx.Commit(ctx)
		}
		if e.status == "pending" {
			if _, err = tx.Exec(ctx, `UPDATE shop_orders SET status='cancelled' WHERE id=$1 AND status='pending'`, e.id); err != nil {
				return "", err
			}
		}
	}
	return "", tx.Commit(ctx)
}
