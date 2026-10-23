package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/types"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAlertsPrepaidReminderPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{MailEndpoint: "https://mailer.example.test"}}
	var person string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Prepaid reminders') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
	recipient := person + "@example.test"
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, recipient); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(6 * 24 * time.Hour)
	start := end.AddDate(-1, 0, 0)
	purchase, err := getters.CreateServicePurchase(app, person, uuid.NewString(), getters.ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Prepaid Alerts", Provider: "opennode", Quantity: 2, UnitCents: 2400, TotalCents: 4800, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: now.Add(time.Hour), BillingMonths: 12})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE shop_orders SET status='paid' WHERE id=$1`, purchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO service_entitlements(purchase_id,person_id,product_code,quantity,starts_at,ends_at) VALUES($1,$2,'alerts.repository-slots',2,$3,$4)`, purchase.PurchaseID, person, start, end); err != nil {
		t.Fatal(err)
	}
	var sent []*emails.Mail
	send := func(m *emails.Mail) error { sent = append(sent, m); return nil }
	for _, at := range []time.Time{now, now, end.Add(-12 * time.Hour), end.Add(-time.Hour), end.Add(time.Second)} {
		if err = processAlertsPrepaidReminderWithSender(ctx, app, at, send); err != nil {
			t.Fatal(err)
		}
	}
	if len(sent) != 2 || sent[0].Email != recipient || sent[0].JobKey == sent[1].JobKey || !strings.Contains(string(sent[0].TextBody), "renew="+purchase.OrderID) || !strings.Contains(string(sent[0].TextBody), "slots=2") {
		t.Fatalf("incorrect reminders: %+v", sent)
	}
}
