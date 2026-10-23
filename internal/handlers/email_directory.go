package handlers

import (
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emaildirectory"
	"btcpp-web/internal/emails"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/gorilla/mux"
	"html"
	"net/http"
	"os"
	"time"
)

func registerEmailDirectory(r *mux.Router, app *config.AppContext) {
	h := &emaildirectory.Handler{Store: emaildirectory.Store{DB: app.DB}, Origin: app.Env.GetURI(), Tokens: map[string]string{"insider": os.Getenv("EMAIL_DIRECTORY_INSIDER_TOKEN"), "alerts": os.Getenv("EMAIL_DIRECTORY_ALERTS_TOKEN")}}
	h.Account = func(r *http.Request) (string, string, error) {
		id, err := auth.Resolve(r, app)
		if err != nil {
			return "", "", err
		}
		if id == nil || id.PersonID == "" || id.Speaker == nil {
			return "", "", nil
		}
		csrf, err := ensureAuthMethodsCSRF(app, r)
		return id.PersonID, csrf, err
	}
	h.SendLink = func(ctx context.Context, address, link string) error {
		return emails.ComposeAndSendMail(app, &emails.Mail{JobKey: fmt.Sprintf("email-preferences-%x", sha256.Sum256([]byte(link))), Email: address, Title: "Manage your bitcoin++ email subscriptions", SendAt: time.Now(), TextBody: []byte("Manage your email subscriptions without signing in:\n\n" + link + "\n\nThis link expires in 30 minutes. If you did not request it, ignore this message."), HTMLBody: []byte("<p>Manage your email subscriptions without signing in.</p><p><a href=\"" + html.EscapeString(link) + "\">Manage email subscriptions</a></p><p>This link expires in 30 minutes.</p>")})
	}
	routes := h.Routes()
	r.PathPrefix("/api/internal/email-directory/").Handler(routes)
	r.PathPrefix("/email-preferences").Handler(routes)
	r.Handle("/dashboard/email-subscriptions", routes)
}

// StartEmailDirectoryCancellation retries cancellation of already queued central
// newsletter mail. Insider and Alerts own cancellation of their product queues.
func StartEmailDirectoryCancellation(app *config.AppContext) {
	if app.DB == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			tx, err := app.DB.Begin(ctx)
			if err != nil {
				cancel()
				continue
			}
			var event int64
			var address, list string
			err = tx.QueryRow(ctx, `SELECT q.event_id,q.recipient,q.newsletter FROM email_preference_cancellations q JOIN email_contacts c ON c.id=q.contact_id WHERE q.completed_at IS NULL AND q.available_at<=now() ORDER BY q.event_id LIMIT 1 FOR UPDATE OF q SKIP LOCKED`).Scan(&event, &address, &list)
			if err == nil {
				var subscribed bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_memberships m JOIN email_contacts c ON c.id=m.contact_id WHERE c.email=$1 AND m.service='btcpp' AND m.newsletter=$2 AND m.status='subscribed')`, address, list).Scan(&subscribed); err != nil {
					tx.Rollback(ctx)
					cancel()
					continue
				}
				if !subscribed {
					err = emails.SendSubDeleteRequest(app, address, list)
				}
				if err == nil {
					_, err = tx.Exec(ctx, `UPDATE email_preference_cancellations SET completed_at=now(),recipient='' WHERE event_id=$1`, event)
				} else {
					_, err = tx.Exec(ctx, `UPDATE email_preference_cancellations SET available_at=now()+interval '1 minute' WHERE event_id=$1`, event)
				}
				if err == nil {
					err = tx.Commit(ctx)
				}
			}
			tx.Rollback(ctx)
			cancel()
		}
	}()
}
