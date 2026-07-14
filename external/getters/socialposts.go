package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"time"
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

func applySocialPostUpdate(post *types.SocialPost, up SocialPostUpdate) *types.SocialPost {
	if post == nil {
		post = &types.SocialPost{}
	}
	cp := *post
	if up.Ref != "" {
		cp.Ref = up.Ref
	}
	if up.Text != nil && *up.Text != "" {
		cp.Text = *up.Text
	}
	if up.PostedTo != "" {
		cp.PostedTo = up.PostedTo
	}
	if up.Kind != "" {
		cp.Kind = up.Kind
	}
	if up.RecordingID != "" {
		cp.RecordingID = up.RecordingID
	}
	if up.ConfTalkID != "" {
		cp.ConfTalkID = up.ConfTalkID
	}
	if up.Status != nil && *up.Status != "" {
		cp.Status = *up.Status
	}
	if up.URL != nil && *up.URL != "" {
		cp.URL = *up.URL
	}
	if up.ReplyURL != nil && *up.ReplyURL != "" {
		cp.ReplyURL = *up.ReplyURL
	}
	if up.Error != nil {
		cp.Error = strings.TrimSpace(*up.Error)
	}
	if up.ErrorFingerprint != nil {
		cp.ErrorFingerprint = strings.TrimSpace(*up.ErrorFingerprint)
	}
	if up.ScheduledAt != nil {
		when := *up.ScheduledAt
		cp.ScheduledAt = &when
	}
	if up.PostedAt != nil {
		when := *up.PostedAt
		cp.PostedAt = &when
	}
	if up.NotifiedAt != nil {
		when := *up.NotifiedAt
		cp.NotifiedAt = &when
	}
	return &cp
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
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO social_posts (ref, text, posted_to, posted_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text
	`, ref, text, platform, postedAt).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert social post: %w", err)
	}
	return nil
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
	existing, err := FindSocialPostByRef(ctx, up.Ref)
	if err != nil {
		return nil, err
	}
	updated := applySocialPostUpdate(existing, up)
	if updated.Ref == "" {
		updated.Ref = up.Ref
	}
	if existing == nil {
		return insertSocialPostPostgres(ctx, updated)
	}
	return updateSocialPostPostgres(ctx, updated)
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
		ORDER BY created_at DESC, id
		LIMIT 1
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

func insertSocialPostPostgres(ctx *config.AppContext, post *types.SocialPost) (*types.SocialPost, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO social_posts (
			ref, text, posted_to, kind, status, recording_id, conf_talk_id,
			url, reply_url, error, error_fingerprint, scheduled_at, posted_at, notified_at
		)
		VALUES (
			$1, $2, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid,
			$8, $9, $10, $11, $12, $13, $14
		)
		RETURNING id::text
	`, post.Ref, post.Text, post.PostedTo, post.Kind, post.Status, post.RecordingID, post.ConfTalkID,
		post.URL, post.ReplyURL, post.Error, post.ErrorFingerprint, post.ScheduledAt, post.PostedAt, post.NotifiedAt).Scan(&post.ID)
	if err != nil {
		return nil, fmt.Errorf("insert social post %s: %w", post.Ref, err)
	}
	return post, nil
}

func updateSocialPostPostgres(ctx *config.AppContext, post *types.SocialPost) (*types.SocialPost, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE social_posts
		SET ref = $2,
			text = $3,
			posted_to = $4,
			kind = $5,
			status = $6,
			recording_id = nullif($7, '')::uuid,
			conf_talk_id = nullif($8, '')::uuid,
			url = $9,
			reply_url = $10,
			error = $11,
			error_fingerprint = $12,
			scheduled_at = $13,
			posted_at = $14,
			notified_at = $15
		WHERE id = $1::uuid
	`, post.ID, post.Ref, post.Text, post.PostedTo, post.Kind, post.Status, post.RecordingID, post.ConfTalkID,
		post.URL, post.ReplyURL, post.Error, post.ErrorFingerprint, post.ScheduledAt, post.PostedAt, post.NotifiedAt)
	if err != nil {
		return nil, fmt.Errorf("update social post %s: %w", post.Ref, err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("social post %s not found", post.ID)
	}
	return post, nil
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
