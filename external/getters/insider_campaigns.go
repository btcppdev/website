package getters

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

type InsiderCampaignInput struct {
	PublicationID    string
	IdempotencyKey   string
	Series           string
	Access           string
	Subject          string
	PreviewText      string
	BodyMarkdown     string
	CanonicalURL     string
	Streams          []string
	DeliveryMode     string
	SendAt           time.Time
	AudienceFrozenAt time.Time
	SuppressedCount  int
	Recipients       []InsiderCampaignRecipientInput
}

type InsiderCampaignRecipientInput struct {
	SubscriberID    string
	Email           string
	SharedContactID string
	ArticleURL      string
	PreferencesURL  string
	OpenTrackingURL string
}

type InsiderCampaign struct {
	ID             string
	PublicationID  string
	IdempotencyKey string
	Status         string
	MissiveUID     uint64
	RecipientCount int
	SendAt         time.Time
	CreatedAt      time.Time
}

type InsiderCampaignStats struct {
	CampaignID          string
	PublicationID       string
	Status              string
	RecipientCount      int
	ScheduledCount      int
	SentCount           int
	DeliveredCount      int
	BouncedCount        int
	ComplainedCount     int
	EstimatedUniqueOpen int
	EstimatedTotalOpen  int
	LastError           string
}

func AcceptInsiderCampaign(ctx *config.AppContext, input InsiderCampaignInput) (*InsiderCampaign, bool, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, false, fmt.Errorf("database is not configured")
	}
	if input.DeliveryMode == "" {
		input.DeliveryMode = types.InsiderDeliveryAsPublished
	}
	if err := validateInsiderCampaignInput(input); err != nil {
		return nil, false, err
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, false, fmt.Errorf("begin Insider campaign: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if existing, err := getInsiderCampaignTx(ctx, tx, input.IdempotencyKey); err != nil {
		return nil, false, err
	} else if existing != nil {
		return existing, false, nil
	}

	// missives.public_uid is a legacy monotonic identifier. Serialize only
	// this short allocation so concurrent editorial campaigns cannot collide.
	if _, err := tx.Exec(ctx.DatabaseContext(), `SELECT pg_advisory_xact_lock(hashtext('missives-public-uid'))`); err != nil {
		return nil, false, fmt.Errorf("lock missive identifier allocation: %w", err)
	}
	var missiveID string
	var missiveUID uint64
	expiry := input.SendAt.AddDate(0, 1, 0)
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO missives (
			public_uid, title, newsletters, only_for, markdown, send_at_expr, expiry, dedupe_key
		) VALUES (
			(SELECT COALESCE(max(public_uid), 0) + 1 FROM missives),
			$1, $2, $3, $4, $5, $6, $7
		)
		RETURNING id::text, public_uid
	`, input.Subject, input.Streams, mtypes.OnlyForTemplated, input.BodyMarkdown, input.SendAt.UTC().Format(time.RFC3339), expiry, "insider:"+input.IdempotencyKey).Scan(&missiveID, &missiveUID); err != nil {
		return nil, false, fmt.Errorf("create Insider missive: %w", err)
	}
	campaign := &InsiderCampaign{}
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO insider_campaigns (
			publication_id, idempotency_key, series, access, subject, preview_text,
			body_markdown, canonical_url, streams, delivery_mode, audience_frozen_at,
			suppressed_count, send_at, missive_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::uuid)
		RETURNING id::text, publication_id, idempotency_key, status, recipient_count, send_at, created_at
	`, input.PublicationID, input.IdempotencyKey, input.Series, input.Access, input.Subject,
		input.PreviewText, input.BodyMarkdown, input.CanonicalURL, input.Streams, input.DeliveryMode,
		input.AudienceFrozenAt, input.SuppressedCount, input.SendAt, missiveID).Scan(&campaign.ID, &campaign.PublicationID, &campaign.IdempotencyKey,
		&campaign.Status, &campaign.RecipientCount, &campaign.SendAt, &campaign.CreatedAt); err != nil {
		return nil, false, fmt.Errorf("create Insider campaign: %w", err)
	}
	campaign.MissiveUID = missiveUID
	for _, recipient := range input.Recipients {
		var sharedContactID any
		if recipient.SharedContactID != "" {
			sharedContactID = recipient.SharedContactID
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO insider_campaign_recipients(
			  campaign_id,subscriber_id,email,shared_contact_id,article_url,preferences_url,open_tracking_url
			) VALUES($1::uuid,$2,$3,$4::uuid,$5,$6,$7)
		`, campaign.ID, recipient.SubscriberID, recipient.Email, sharedContactID,
			recipient.ArticleURL, recipient.PreferencesURL, recipient.OpenTrackingURL); err != nil {
			return nil, false, fmt.Errorf("store frozen Insider campaign recipient: %w", err)
		}
	}
	campaign.RecipientCount = len(input.Recipients)
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE insider_campaigns SET recipient_count = $2 WHERE id = $1::uuid
	`, campaign.ID, campaign.RecipientCount); err != nil {
		return nil, false, fmt.Errorf("update Insider campaign recipient count: %w", err)
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, false, fmt.Errorf("commit Insider campaign: %w", err)
	}
	return campaign, true, nil
}

func ListInsiderCampaignSubscribers(ctx *config.AppContext, campaignID string) ([]*mtypes.Subscriber, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT recipient.subscriber_id, recipient.email::text, campaign.streams,
		  recipient.article_url,campaign.canonical_url,recipient.preferences_url,recipient.open_tracking_url,campaign.preview_text
		FROM insider_campaign_recipients recipient
		JOIN insider_campaigns campaign ON campaign.id=recipient.campaign_id
		WHERE recipient.campaign_id = $1::uuid
		  AND recipient.status = 'accepted'
		ORDER BY recipient.email
	`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("query Insider campaign recipients: %w", err)
	}
	defer rows.Close()
	var subscribers []*mtypes.Subscriber
	for rows.Next() {
		var subscriberID, email, articleURL, canonicalURL, preferencesURL, openTrackingURL, previewText string
		var names []string
		if err := rows.Scan(&subscriberID, &email, &names, &articleURL, &canonicalURL, &preferencesURL, &openTrackingURL, &previewText); err != nil {
			return nil, fmt.Errorf("scan Insider campaign recipient: %w", err)
		}
		subscriber := &mtypes.Subscriber{
			Email: email, Pages: []string{subscriberID}, InsiderArticleURL: articleURL,
			InsiderCanonicalURL:   canonicalURL,
			InsiderPreferencesURL: preferencesURL, InsiderOpenTrackingURL: openTrackingURL,
			InsiderPreviewText: previewText,
		}
		for _, name := range names {
			subscriber.AddSubscription(name)
		}
		subscribers = append(subscribers, subscriber)
	}
	return subscribers, rows.Err()
}

func MarkInsiderCampaignQueued(ctx *config.AppContext, campaignID string, successful bool, message string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	status := "failed"
	if successful {
		status = "queued"
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin Insider campaign status update: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	_, err = tx.Exec(ctx.DatabaseContext(), `
		UPDATE insider_campaigns
		SET status = $2,
		    queued_at = CASE WHEN $2 = 'queued' THEN now() ELSE queued_at END,
		    last_error = $3
		WHERE id = $1::uuid
	`, campaignID, status, strings.TrimSpace(message))
	if err != nil {
		return fmt.Errorf("mark Insider campaign %s (%s): %w", status, strings.TrimSpace(message), err)
	}
	if successful {
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE insider_campaign_recipients
			SET status='scheduled'
			WHERE campaign_id=$1::uuid AND status='accepted'
		`, campaignID); err != nil {
			return fmt.Errorf("mark Insider campaign recipients scheduled: %w", err)
		}
	}
	return tx.Commit(ctx.DatabaseContext())
}

func GetInsiderCampaignStats(ctx *config.AppContext, campaignID string) (*InsiderCampaignStats, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	stats := &InsiderCampaignStats{}
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT campaign.id::text,campaign.publication_id,campaign.status,campaign.recipient_count,
		  count(DISTINCT recipient.subscriber_id) FILTER (WHERE recipient.status IN ('scheduled','sent','delivered'))::int,
		  count(DISTINCT recipient.subscriber_id) FILTER (WHERE recipient.status IN ('sent','delivered'))::int,
		  count(DISTINCT recipient.subscriber_id) FILTER (WHERE recipient.status='delivered')::int,
		  count(DISTINCT recipient.subscriber_id) FILTER (WHERE recipient.status='bounced')::int,
		  count(DISTINCT recipient.subscriber_id) FILTER (WHERE recipient.status='complained')::int,
		  0::int,
		  0::int,
		  campaign.last_error
		FROM insider_campaigns campaign
		LEFT JOIN insider_campaign_recipients recipient ON recipient.campaign_id=campaign.id
		WHERE campaign.id=$1::uuid
		GROUP BY campaign.id
	`, campaignID).Scan(&stats.CampaignID, &stats.PublicationID, &stats.Status, &stats.RecipientCount,
		&stats.ScheduledCount, &stats.SentCount, &stats.DeliveredCount, &stats.BouncedCount,
		&stats.ComplainedCount, &stats.EstimatedUniqueOpen, &stats.EstimatedTotalOpen, &stats.LastError)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("Insider campaign was not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query Insider campaign stats: %w", err)
	}
	return stats, nil
}

func getInsiderCampaignTx(ctx *config.AppContext, tx pgx.Tx, idempotencyKey string) (*InsiderCampaign, error) {
	campaign := &InsiderCampaign{}
	err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT campaign.id::text, campaign.publication_id, campaign.idempotency_key,
		  campaign.status, missive.public_uid, campaign.recipient_count,
		  campaign.send_at, campaign.created_at
		FROM insider_campaigns campaign
		JOIN missives missive ON missive.id = campaign.missive_id
		WHERE campaign.idempotency_key = $1
	`, idempotencyKey).Scan(&campaign.ID, &campaign.PublicationID, &campaign.IdempotencyKey,
		&campaign.Status, &campaign.MissiveUID, &campaign.RecipientCount, &campaign.SendAt, &campaign.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query Insider campaign: %w", err)
	}
	return campaign, nil
}

func validateInsiderCampaignInput(input InsiderCampaignInput) error {
	if strings.TrimSpace(input.PublicationID) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return fmt.Errorf("publication_id and idempotency_key are required")
	}
	if len(input.IdempotencyKey) > 200 || strings.TrimSpace(input.Subject) == "" || len([]rune(input.Subject)) > 200 {
		return fmt.Errorf("campaign subject or idempotency key is invalid")
	}
	if len(input.BodyMarkdown) == 0 || len(input.BodyMarkdown) > 1<<20 {
		return fmt.Errorf("campaign body_markdown is invalid")
	}
	if input.Access != "public" && input.Access != "subscribers" && input.Access != "members" {
		return fmt.Errorf("campaign access is invalid")
	}
	seriesValid := false
	for _, stream := range types.InsiderStreams() {
		if input.Series == strings.TrimPrefix(stream.ID, "insider.") {
			seriesValid = true
		}
	}
	if !seriesValid || len(input.Streams) == 0 {
		return fmt.Errorf("campaign series and at least one stream are required")
	}
	seen := map[string]bool{}
	for _, stream := range input.Streams {
		if !types.IsInsiderStream(stream) || seen[stream] {
			return fmt.Errorf("campaign stream %q is invalid or duplicated", stream)
		}
		seen[stream] = true
	}
	if input.DeliveryMode == "" {
		input.DeliveryMode = types.InsiderDeliveryAsPublished
	}
	if input.DeliveryMode != types.InsiderDeliveryAsPublished && input.DeliveryMode != types.InsiderDeliveryWeeklyDigest {
		return fmt.Errorf("campaign delivery mode is invalid")
	}
	if input.SendAt.IsZero() {
		return fmt.Errorf("campaign send_at is required")
	}
	if input.AudienceFrozenAt.IsZero() || input.AudienceFrozenAt.After(time.Now().UTC().Add(5*time.Minute)) || input.SuppressedCount < 0 {
		return fmt.Errorf("campaign audience snapshot is invalid")
	}
	if !strings.HasPrefix(input.CanonicalURL, "https://") && !strings.HasPrefix(input.CanonicalURL, "http://") {
		return fmt.Errorf("campaign canonical_url is invalid")
	}
	seenSubscribers, seenEmails := map[string]bool{}, map[string]bool{}
	for _, recipient := range input.Recipients {
		emailAddress, err := mail.ParseAddress(strings.TrimSpace(recipient.Email))
		if err != nil || strings.ToLower(emailAddress.Address) != strings.ToLower(strings.TrimSpace(recipient.Email)) || strings.TrimSpace(recipient.SubscriberID) == "" || seenSubscribers[recipient.SubscriberID] || seenEmails[strings.ToLower(recipient.Email)] {
			return fmt.Errorf("campaign recipient identity is invalid or duplicated")
		}
		seenSubscribers[recipient.SubscriberID], seenEmails[strings.ToLower(recipient.Email)] = true, true
		for _, raw := range []string{recipient.ArticleURL, recipient.PreferencesURL, recipient.OpenTrackingURL} {
			parsed, err := url.Parse(strings.TrimSpace(raw))
			if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
				return fmt.Errorf("campaign recipient URL is invalid")
			}
		}
	}
	return nil
}
