package getters

import (
	"context"
	"errors"
	"strings"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrServiceSalesClosed = errors.New("this service is no longer accepting purchases")

// StopServiceSales is the first permanent-closure step. It does not cancel
// provider sessions/renewals or end access. Callers must authenticate the actor.
// The immutable record deliberately has no implicit reopen operation.
func StopServiceSales(ctx context.Context, app *config.AppContext, actor, service, reason string) error {
	reason = strings.TrimSpace(reason)
	if (service != "alerts" && service != "insider") || reason == "" || len(reason) > 1000 {
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
	var matches bool
	err = tx.QueryRow(ctx, `SELECT actor_id=$2 AND reason=$3 FROM service_sales_closures WHERE service=$1`, service, actor, reason).Scan(&matches)
	if err == nil {
		if !matches {
			return ErrServiceQuote
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO service_sales_closures(service,actor_id,reason) VALUES($1,$2,$3)`, service, actor, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// requireServiceSales serializes new reservations with the closure operation.
// Existing settlement/webhook recording must not use this guard.
func requireServiceSales(ctx context.Context, tx pgx.Tx, product string) error {
	service := strings.SplitN(product, ".", 2)[0]
	if service != "alerts" && service != "insider" {
		return nil // Other product families retain their own purchase validation.
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended($1,0))`, "service-sales:"+service); err != nil {
		return err
	}
	return checkServiceSales(ctx, tx, service)
}

func checkServiceSales(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, product string) error {
	service := strings.SplitN(product, ".", 2)[0]
	if service != "alerts" && service != "insider" {
		return nil // Other product families retain their own purchase validation.
	}
	var stopped bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_sales_closures WHERE service=$1)`, service).Scan(&stopped); err != nil {
		return err
	}
	if stopped {
		return ErrServiceSalesClosed
	}
	return nil
}

// ServiceSalesAvailable checks local sales status without changing provider state.
func ServiceSalesAvailable(ctx context.Context, app *config.AppContext, product string) error {
	return checkServiceSales(ctx, app.DB, product)
}
