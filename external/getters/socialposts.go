package getters

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
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

var (
	socialPostCacheMu sync.RWMutex
	socialPostByRef   map[string]*types.SocialPost
)

// ListPostedRefs returns a set of all Ref values that have been posted
func ListPostedRefs(ctx *config.AppContext, conf *types.Conf) (map[string]bool, error) {
	if UsePostgresBackend(ctx) {
		return listPostedRefsPostgres(ctx, conf)
	}
	return listPostedRefsNotion(ctx, conf)
}

func RecordSocialPost(ctx *config.AppContext, ref, text, platform string, postedAt time.Time) error {
	if UsePostgresBackend(ctx) {
		return recordSocialPostPostgres(ctx, ref, text, platform, postedAt)
	}
	return recordSocialPostNotion(ctx, ref, text, platform, postedAt)
}

// findCachedSocialPostByRef returns the Notion/fallback cached SocialPost for
// ref. Postgres callers should use GetSocialPostByRef.
func findCachedSocialPostByRef(ref string) *types.SocialPost {
	socialPostCacheMu.RLock()
	defer socialPostCacheMu.RUnlock()
	post := socialPostByRef[ref]
	if post == nil {
		return nil
	}
	cp := *post
	return &cp
}

func ListSocialPosts(ctx *config.AppContext) ([]*types.SocialPost, error) {
	if UsePostgresBackend(ctx) {
		return listSocialPostsPostgres(ctx)
	}
	return listSocialPostsNotion(ctx)
}

func UpsertSocialPost(ctx *config.AppContext, up SocialPostUpdate) (*types.SocialPost, error) {
	if strings.TrimSpace(up.Ref) == "" {
		return nil, fmt.Errorf("social post ref required")
	}
	if UsePostgresBackend(ctx) {
		return upsertSocialPostPostgres(ctx, up)
	}
	return upsertSocialPostNotion(ctx, up)
}

func GetSocialPostByRef(ctx *config.AppContext, ref string) (*types.SocialPost, error) {
	if UsePostgresBackend(ctx) {
		return findSocialPostByRefPostgres(ctx, ref)
	}
	return findSocialPostByRefNotion(ctx, ref)
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

// cacheSocialPost updates the Notion/fallback social-post cache.
func cacheSocialPost(post *types.SocialPost) {
	if post == nil || post.Ref == "" {
		return
	}
	socialPostCacheMu.Lock()
	defer socialPostCacheMu.Unlock()
	if socialPostByRef == nil {
		socialPostByRef = map[string]*types.SocialPost{}
	}
	socialPostByRef[post.Ref] = post
}
