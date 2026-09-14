// backfill-organization-welcomes sends launch notices to active organization
// memberships. The default is a read-only dry run; -send queues the notices.
package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/envconfig"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recipient struct{ organizationID, personID, organizationName, name, email, role string }
type totals struct{ eligible, queued, missingEmail, failed int }

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	envPath := flag.String("env", ".env", "environment file; existing process variables take precedence")
	send := flag.Bool("send", false, "queue welcome notices (default: dry run)")
	before := flag.String("before", "", "include memberships created on or before this RFC3339 launch cutoff (default: now)")
	flag.Parse()
	cutoff := time.Now().UTC()
	if *before != "" {
		var err error
		cutoff, err = time.Parse(time.RFC3339Nano, *before)
		if err != nil {
			return fmt.Errorf("invalid -before: %w", err)
		}
	}
	env, err := envconfig.Load(*envPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(env.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if *send && (env.MailOff || env.MailEndpoint == "" || env.MailerSecret == "") {
		return fmt.Errorf("sending requires MAILER_OFF=false, MAILER_ENDPOINT and MAILER_SECRET")
	}
	pool, err := pgxpool.New(context.Background(), env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	source, err := os.ReadFile("templates/emails/rebrand.tmpl")
	if err != nil {
		return err
	}
	templates, err := template.New("emails/rebrand.tmpl").Parse(string(source))
	if err != nil {
		return err
	}
	ctx := &config.AppContext{Env: env, DB: pool, TemplateCache: templates, Infos: log.New(os.Stderr, "", log.LstdFlags), Err: log.New(os.Stderr, "", log.LstdFlags)}
	// Materialize the cohort and release the connection before sending. The shared
	// sender rechecks active membership and current role/email for each notice.
	rows, err := pool.Query(ctx.DatabaseContext(), `
 SELECT m.organization_id::text, m.person_id::text, o.name, p.name,
        coalesce(e.email::text, ''), m.role
 FROM organization_memberships m
 JOIN organizations o ON o.id = m.organization_id
 JOIN people p ON p.id = m.person_id
 LEFT JOIN LATERAL (
   SELECT email FROM person_emails WHERE person_id = m.person_id
   ORDER BY is_primary DESC, created_at, id LIMIT 1
 ) e ON true
 WHERE m.status = 'active' AND m.created_at <= $1
 ORDER BY m.organization_id, m.person_id`, cutoff)
	if err != nil {
		return err
	}
	var recipients []recipient
	for rows.Next() {
		var r recipient
		if err := rows.Scan(&r.organizationID, &r.personID, &r.organizationName, &r.name, &r.email, &r.role); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	fmt.Printf("cutoff=%s dry-run=%t memberships=%d\n", cutoff.Format(time.RFC3339Nano), !*send, len(recipients))
	result := backfill(os.Stdout, recipients, *send, func(r recipient) error {
		return emails.SendOrganizationWelcomeEmail(ctx, r.organizationID, r.personID, r.name)
	})
	fmt.Printf("eligible=%d queued=%d missing-email=%d failed=%d\n", result.eligible, result.queued, result.missingEmail, result.failed)
	if result.failed > 0 || result.missingEmail > 0 {
		return fmt.Errorf("backfill incomplete: resolve reported memberships and rerun with the same -before cutoff")
	}
	return nil
}

func backfill(out io.Writer, recipients []recipient, send bool, deliver func(recipient) error) totals {
	var result totals
	for _, r := range recipients {
		if strings.TrimSpace(r.email) == "" {
			result.missingEmail++
			fmt.Fprintf(out, "MISSING EMAIL org=%s person=%s role=%s\n", r.organizationID, r.personID, r.role)
			continue
		}
		result.eligible++
		if !send {
			fmt.Fprintf(out, "WOULD SEND org=%s (%s) person=%s role=%s email=%s\n", r.organizationID, r.organizationName, r.personID, r.role, r.email)
			continue
		}
		if err := deliver(r); err != nil {
			result.failed++
			fmt.Fprintf(out, "FAILED org=%s person=%s: %v\n", r.organizationID, r.personID, err)
			continue
		}
		result.queued++
		fmt.Fprintf(out, "QUEUED org=%s person=%s role=%s\n", r.organizationID, r.personID, r.role)
	}
	return result
}
