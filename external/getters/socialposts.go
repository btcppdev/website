package getters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	SocialPostKindRecording = "recording"
)

type SocialPostUpdate struct {
	Ref              string
	Text             *string
	PostedTo         string
	Kind             string
	RecordingID      string
	ConfTalkID       string
	Status           *string
	URL              *string
	ReplyURL         *string
	Error            *string
	ErrorFingerprint *string
	ScheduledAt      *time.Time
	PostedAt         *time.Time
	NotifiedAt       *time.Time
}

type SocialPostClaim struct {
	Ref   string
	Token string
}

func socialPostFromUpdate(up SocialPostUpdate) *types.SocialPost {
	post := &types.SocialPost{}
	if up.Ref != "" {
		post.Ref = up.Ref
	}
	if up.Text != nil && *up.Text != "" {
		post.Text = *up.Text
	}
	if up.PostedTo != "" {
		post.PostedTo = up.PostedTo
	}
	if up.Kind != "" {
		post.Kind = up.Kind
	}
	if up.RecordingID != "" {
		post.RecordingID = up.RecordingID
	}
	if up.ConfTalkID != "" {
		post.ConfTalkID = up.ConfTalkID
	}
	if up.Status != nil && *up.Status != "" {
		post.Status = *up.Status
	}
	if up.URL != nil && *up.URL != "" {
		post.URL = *up.URL
	}
	if up.ReplyURL != nil && *up.ReplyURL != "" {
		post.ReplyURL = *up.ReplyURL
	}
	if up.Error != nil {
		post.Error = strings.TrimSpace(*up.Error)
	}
	if up.ErrorFingerprint != nil {
		post.ErrorFingerprint = strings.TrimSpace(*up.ErrorFingerprint)
	}
	if up.ScheduledAt != nil {
		when := *up.ScheduledAt
		post.ScheduledAt = &when
	}
	if up.PostedAt != nil {
		when := *up.PostedAt
		post.PostedAt = &when
	}
	if up.NotifiedAt != nil {
		when := *up.NotifiedAt
		post.NotifiedAt = &when
	}
	return post
}

func socialPostSuppressesRef(post *types.SocialPost) bool {
	status := strings.TrimSpace(strings.ToLower(post.Status))
	if status == "" {
		return true
	}
	switch status {
	case "queued", "scheduled", "posted", "uploaded", "published", "succeeded", "success":
		return true
	default:
		return false
	}
}

func ListPostedRefs(ctx *config.AppContext, conf *types.Conf) (map[string]bool, error) {
	posts, err := ListSocialPosts(ctx)
	if err != nil {
		return nil, err
	}
	posted := make(map[string]bool)
	for _, post := range posts {
		if post == nil || post.Ref == "" || !socialPostSuppressesRef(post) {
			continue
		}
		if conf != nil && !strings.Contains(post.Ref, conf.Tag) {
			continue
		}
		posted[post.Ref] = true
	}
	return posted, nil
}

func RecordSocialPost(ctx *config.AppContext, ref, text, platform string, postedAt time.Time) error {
	_, err := UpsertSocialPost(ctx, SocialPostUpdate{
		Ref:      ref,
		Text:     &text,
		PostedTo: platform,
		PostedAt: &postedAt,
	})
	return err
}

func ListSocialPosts(ctx *config.AppContext) ([]*types.SocialPost, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, ref, text, posted_to, kind, status,
			coalesce(recording_id::text, ''), coalesce(conf_talk_id::text, ''),
			url, reply_url, error, error_fingerprint,
			scheduled_at, posted_at, notified_at
		FROM social_posts
		ORDER BY created_at DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query social posts: %w", err)
	}
	defer rows.Close()

	var out []*types.SocialPost
	for rows.Next() {
		post, err := scanSocialPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate social posts: %w", err)
	}
	return out, nil
}

func UpsertSocialPost(ctx *config.AppContext, up SocialPostUpdate) (*types.SocialPost, error) {
	if strings.TrimSpace(up.Ref) == "" {
		return nil, fmt.Errorf("social post ref required")
	}
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	post := socialPostFromUpdate(up)
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO social_posts (
			ref, text, posted_to, kind, status, recording_id, conf_talk_id,
			url, reply_url, error, error_fingerprint, scheduled_at, posted_at, notified_at
		)
		VALUES (
			$1, $2, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid,
			$8, $9, $10, $11, $12, $13, $14
		)
		ON CONFLICT (ref) DO UPDATE SET
			text = coalesce(nullif(EXCLUDED.text, ''), social_posts.text),
			posted_to = coalesce(nullif(EXCLUDED.posted_to, ''), social_posts.posted_to),
			kind = coalesce(nullif(EXCLUDED.kind, ''), social_posts.kind),
			status = coalesce(nullif(EXCLUDED.status, ''), social_posts.status),
			recording_id = coalesce(EXCLUDED.recording_id, social_posts.recording_id),
			conf_talk_id = coalesce(EXCLUDED.conf_talk_id, social_posts.conf_talk_id),
			url = coalesce(nullif(EXCLUDED.url, ''), social_posts.url),
			reply_url = coalesce(nullif(EXCLUDED.reply_url, ''), social_posts.reply_url),
			error = CASE WHEN $15 THEN EXCLUDED.error ELSE social_posts.error END,
			error_fingerprint = CASE WHEN $16 THEN EXCLUDED.error_fingerprint ELSE social_posts.error_fingerprint END,
			scheduled_at = coalesce(EXCLUDED.scheduled_at, social_posts.scheduled_at),
			posted_at = coalesce(EXCLUDED.posted_at, social_posts.posted_at),
			notified_at = coalesce(EXCLUDED.notified_at, social_posts.notified_at)
		WHERE social_posts.publication_claim_token IS NULL
		RETURNING id::text, ref, text, posted_to, kind, status,
			coalesce(recording_id::text, ''), coalesce(conf_talk_id::text, ''),
			url, reply_url, error, error_fingerprint,
			scheduled_at, posted_at, notified_at
	`, post.Ref, post.Text, post.PostedTo, post.Kind, post.Status, post.RecordingID, post.ConfTalkID,
		post.URL, post.ReplyURL, post.Error, post.ErrorFingerprint, post.ScheduledAt, post.PostedAt, post.NotifiedAt,
		up.Error != nil, up.ErrorFingerprint != nil)
	updated, err := scanSocialPost(row)
	if err != nil {
		return nil, fmt.Errorf("upsert social post %s: %w", up.Ref, err)
	}
	return updated, nil
}

// ClaimSocialPost acquires an expiring cross-process lease for an external
// publication operation. The supplied update is applied only when the lease is
// acquired, so a competing worker cannot change the in-progress state.
func ClaimSocialPost(ctx *config.AppContext, up SocialPostUpdate, lease time.Duration) (*SocialPostClaim, bool, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, false, fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(up.Ref) == "" {
		return nil, false, fmt.Errorf("social post ref required")
	}
	if lease <= 0 {
		return nil, false, fmt.Errorf("social post claim lease must be positive")
	}
	post := socialPostFromUpdate(up)
	var token string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO social_posts (
			ref, text, posted_to, kind, status, recording_id, conf_talk_id,
			error, error_fingerprint, scheduled_at,
			publication_claim_token, publication_claim_expires_at
		)
		VALUES (
			$1, $2, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid,
			$8, $9, $10,
			gen_random_uuid(), now() + ($11::double precision * interval '1 second')
		)
		ON CONFLICT (ref) DO UPDATE SET
			text = coalesce(nullif(EXCLUDED.text, ''), social_posts.text),
			posted_to = coalesce(nullif(EXCLUDED.posted_to, ''), social_posts.posted_to),
			kind = coalesce(nullif(EXCLUDED.kind, ''), social_posts.kind),
			status = coalesce(nullif(EXCLUDED.status, ''), social_posts.status),
			recording_id = coalesce(EXCLUDED.recording_id, social_posts.recording_id),
			conf_talk_id = coalesce(EXCLUDED.conf_talk_id, social_posts.conf_talk_id),
			error = CASE WHEN $12 THEN EXCLUDED.error ELSE social_posts.error END,
			error_fingerprint = CASE WHEN $13 THEN EXCLUDED.error_fingerprint ELSE social_posts.error_fingerprint END,
			scheduled_at = coalesce(EXCLUDED.scheduled_at, social_posts.scheduled_at),
			publication_claim_token = EXCLUDED.publication_claim_token,
			publication_claim_expires_at = EXCLUDED.publication_claim_expires_at
		WHERE social_posts.publication_claim_token IS NULL
			OR social_posts.publication_claim_expires_at <= now()
		RETURNING publication_claim_token::text
	`, post.Ref, post.Text, post.PostedTo, post.Kind, post.Status, post.RecordingID, post.ConfTalkID,
		post.Error, post.ErrorFingerprint, post.ScheduledAt, lease.Seconds(),
		up.Error != nil, up.ErrorFingerprint != nil).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("claim social post %s: %w", up.Ref, err)
	}
	return &SocialPostClaim{Ref: up.Ref, Token: token}, true, nil
}

// UpdateClaimedSocialPost applies publication state only while claim owns the
// row. It intentionally leaves the claim active so one operation can persist
// intermediate and terminal state before releasing its lease.
func UpdateClaimedSocialPost(ctx *config.AppContext, claim *SocialPostClaim, up SocialPostUpdate) (*types.SocialPost, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if claim == nil || strings.TrimSpace(claim.Ref) == "" || strings.TrimSpace(claim.Token) == "" {
		return nil, fmt.Errorf("social post claim is required")
	}
	if up.Ref != "" && up.Ref != claim.Ref {
		return nil, fmt.Errorf("social post update ref %s does not match claim %s", up.Ref, claim.Ref)
	}
	up.Ref = claim.Ref
	post := socialPostFromUpdate(up)
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		UPDATE social_posts
		SET text = coalesce(nullif($3, ''), text),
			posted_to = coalesce(nullif($4, ''), posted_to),
			kind = coalesce(nullif($5, ''), kind),
			status = coalesce(nullif($6, ''), status),
			recording_id = coalesce(nullif($7, '')::uuid, recording_id),
			conf_talk_id = coalesce(nullif($8, '')::uuid, conf_talk_id),
			url = coalesce(nullif($9, ''), url),
			reply_url = coalesce(nullif($10, ''), reply_url),
			error = CASE WHEN $11 THEN $12 ELSE error END,
			error_fingerprint = CASE WHEN $13 THEN $14 ELSE error_fingerprint END,
			scheduled_at = coalesce($15, scheduled_at),
			posted_at = coalesce($16, posted_at),
			notified_at = coalesce($17, notified_at)
		WHERE ref = $1
			AND publication_claim_token = $2::uuid
		RETURNING id::text, ref, text, posted_to, kind, status,
			coalesce(recording_id::text, ''), coalesce(conf_talk_id::text, ''),
			url, reply_url, error, error_fingerprint,
			scheduled_at, posted_at, notified_at
	`, claim.Ref, claim.Token, post.Text, post.PostedTo, post.Kind, post.Status, post.RecordingID, post.ConfTalkID,
		post.URL, post.ReplyURL, up.Error != nil, post.Error,
		up.ErrorFingerprint != nil, post.ErrorFingerprint, post.ScheduledAt, post.PostedAt, post.NotifiedAt)
	updated, err := scanSocialPost(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("social post claim %s is no longer owned by %s", claim.Ref, claim.Token)
	}
	if err != nil {
		return nil, fmt.Errorf("update claimed social post %s: %w", claim.Ref, err)
	}
	return updated, nil
}

func RenewSocialPostClaim(ctx *config.AppContext, claim *SocialPostClaim, lease time.Duration) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if claim == nil || strings.TrimSpace(claim.Ref) == "" || strings.TrimSpace(claim.Token) == "" {
		return fmt.Errorf("social post claim is required")
	}
	if lease <= 0 {
		return fmt.Errorf("social post claim lease must be positive")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE social_posts
		SET publication_claim_expires_at = now() + ($3::double precision * interval '1 second')
		WHERE ref = $1
			AND publication_claim_token = $2::uuid
	`, claim.Ref, claim.Token, lease.Seconds())
	if err != nil {
		return fmt.Errorf("renew social post claim %s: %w", claim.Ref, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("social post claim %s is no longer owned by %s", claim.Ref, claim.Token)
	}
	return nil
}

func ReleaseSocialPostClaim(ctx *config.AppContext, claim *SocialPostClaim) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if claim == nil || strings.TrimSpace(claim.Ref) == "" || strings.TrimSpace(claim.Token) == "" {
		return fmt.Errorf("social post claim is required")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE social_posts
		SET publication_claim_token = NULL,
			publication_claim_expires_at = NULL
		WHERE ref = $1
			AND publication_claim_token = $2::uuid
	`, claim.Ref, claim.Token)
	if err != nil {
		return fmt.Errorf("release social post claim %s: %w", claim.Ref, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("social post claim %s is no longer owned by %s", claim.Ref, claim.Token)
	}
	return nil
}

func FindSocialPostByRef(ctx *config.AppContext, ref string) (*types.SocialPost, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, ref, text, posted_to, kind, status,
			coalesce(recording_id::text, ''), coalesce(conf_talk_id::text, ''),
			url, reply_url, error, error_fingerprint,
			scheduled_at, posted_at, notified_at
		FROM social_posts
		WHERE ref = $1
	`, ref)
	post, err := scanSocialPost(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return post, nil
}

func GetSocialPostByRef(ctx *config.AppContext, ref string) (*types.SocialPost, error) {
	return FindSocialPostByRef(ctx, ref)
}

type socialPostScanner interface {
	Scan(dest ...any) error
}

func scanSocialPost(row socialPostScanner) (*types.SocialPost, error) {
	var post types.SocialPost
	var scheduledAt pgtype.Timestamptz
	var postedAt pgtype.Timestamptz
	var notifiedAt pgtype.Timestamptz
	err := row.Scan(
		&post.ID,
		&post.Ref,
		&post.Text,
		&post.PostedTo,
		&post.Kind,
		&post.Status,
		&post.RecordingID,
		&post.ConfTalkID,
		&post.URL,
		&post.ReplyURL,
		&post.Error,
		&post.ErrorFingerprint,
		&scheduledAt,
		&postedAt,
		&notifiedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan social post: %w", err)
	}
	if scheduledAt.Valid {
		post.ScheduledAt = &scheduledAt.Time
	}
	if postedAt.Valid {
		post.PostedAt = &postedAt.Time
	}
	if notifiedAt.Valid {
		post.NotifiedAt = &notifiedAt.Time
	}
	return &post, nil
}
