package getters

import (
	"context"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ServiceClosurePlan fixes the reviewed closure instant independently of sales
// stop, provider cancellation, product enforcement and refund settlement.
type ServiceClosurePlan struct {
	Service    string    `json:"service"`
	ActorID    string    `json:"actor_id"`
	ClosesAt   time.Time `json:"closes_at"`
	Reason     string    `json:"reason"`
	RecordedAt time.Time `json:"recorded_at"`
}

// RecordServiceClosurePlan records intent, not successful product shutdown.
// No provider or product call is made, and purchased periods remain immutable.
func RecordServiceClosurePlan(ctx context.Context, app *config.AppContext, actor, service string, closesAt time.Time, reason string) error {
	reason = strings.TrimSpace(reason)
	if (service != "alerts" && service != "insider") || reason == "" || len(reason) > 1000 || closesAt.IsZero() || closesAt.Nanosecond() != 0 {
		return ErrServiceQuote
	}
	if id, err := uuid.Parse(actor); err != nil || id.String() != actor {
		return ErrServiceQuote
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role int
	err = tx.QueryRow(ctx, `SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account FOR SHARE OF r,p`, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceRefundAdmin
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-sales:"+service); err != nil {
		return err
	}
	var stoppedAt time.Time
	if err = tx.QueryRow(ctx, `SELECT stopped_at FROM service_sales_closures WHERE service=$1`, service).Scan(&stoppedAt); err != nil {
		return err
	}
	// Never backdate closure before sales actually stopped. A past date after
	// sales stop can record an operational closure whose audit was delayed.
	if closesAt.Before(stoppedAt) {
		return ErrServiceQuote
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT actor_id=$2 AND closes_at=$3 AND reason=$4 FROM service_closure_plans WHERE service=$1`, service, actor, closesAt.UTC(), reason).Scan(&same)
	if err == nil {
		if !same {
			return ErrServiceQuote
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO service_closure_plans(service,actor_id,closes_at,reason) VALUES($1,$2,$3,$4)`, service, actor, closesAt.UTC(), reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func GetServiceClosurePlan(ctx context.Context, app *config.AppContext, service string) (*ServiceClosurePlan, error) {
	if service != "alerts" && service != "insider" {
		return nil, ErrServiceQuote
	}
	var out ServiceClosurePlan
	err := app.DB.QueryRow(ctx, `SELECT service,actor_id::text,closes_at,reason,recorded_at FROM service_closure_plans WHERE service=$1`, service).Scan(&out.Service, &out.ActorID, &out.ClosesAt, &out.Reason, &out.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
