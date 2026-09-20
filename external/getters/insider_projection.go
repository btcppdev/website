package getters

import (
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"

	"github.com/jackc/pgx/v5"
)

type InsiderSubscriberProjection struct {
	PersonID         string
	SubscriberTier   string
	NewsletterStatus string
	Version          int64
	AsOf             time.Time
}

func ApplyInsiderSubscriberProjection(ctx *config.AppContext, projection InsiderSubscriberProjection) (bool, error) {
	projection.PersonID = strings.TrimSpace(projection.PersonID)
	if ctx == nil || ctx.DB == nil || projection.PersonID == "" || projection.Version <= 0 || projection.AsOf.IsZero() ||
		(projection.SubscriberTier != "none" && projection.SubscriberTier != "free" && projection.SubscriberTier != "paid") ||
		(projection.NewsletterStatus != "subscribed" && projection.NewsletterStatus != "not_subscribed") {
		return false, fmt.Errorf("invalid Insider subscriber projection")
	}
	command, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO insider_subscriber_projections(person_id,subscriber_tier,newsletter_status,version,as_of)
		SELECT person.id,$2,$3,$4,$5 FROM people person
 WHERE person.id=$1::uuid AND NOT person.is_deleted_account
		ON CONFLICT(person_id) DO UPDATE SET
		  subscriber_tier=excluded.subscriber_tier,
		  newsletter_status=excluded.newsletter_status,
		  version=excluded.version,
		  as_of=excluded.as_of
		WHERE insider_subscriber_projections.version < excluded.version
	`, projection.PersonID, projection.SubscriberTier, projection.NewsletterStatus, projection.Version, projection.AsOf.UTC())
	if err != nil {
		return false, fmt.Errorf("apply Insider subscriber projection: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func GetInsiderSubscriberProjection(ctx *config.AppContext, personID string) (*InsiderSubscriberProjection, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	projection := new(InsiderSubscriberProjection)
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT person_id::text,subscriber_tier,newsletter_status,version,as_of
		FROM insider_subscriber_projections WHERE person_id=$1::uuid
 AND EXISTS(SELECT 1 FROM people p WHERE p.id=person_id AND NOT p.is_deleted_account)
	`, strings.TrimSpace(personID)).Scan(&projection.PersonID, &projection.SubscriberTier,
		&projection.NewsletterStatus, &projection.Version, &projection.AsOf)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get Insider subscriber projection: %w", err)
	}
	return projection, nil
}
