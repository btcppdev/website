package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/publishing"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

// Routing is an explicit account setting. Paused accounts keep queued work locally.
func directEventX(app *config.AppContext) (string, error) {
	if app.DB == nil {
		return "", nil
	}
	var id string
	e := app.DB.QueryRow(app.DatabaseContext(), `SELECT id FROM publishing_channels WHERE platform='x' AND provider='direct' AND route_event_posts AND credentials IS NOT NULL`).Scan(&id)
	if e == pgx.ErrNoRows {
		return "", nil
	}
	return id, e
}
func eventPublishingChannels(app *config.AppContext) ([]publishing.Channel, error) {
	if app.DB == nil {
		return nil, nil
	}
	channels, err := (publishing.Store{DB: app.DB}).Channels(app.DatabaseContext())
	if err != nil {
		return nil, err
	}
	var out []publishing.Channel
	for _, c := range channels {
		if c.Provider == "direct" && c.Platform == "x" && c.RouteEventPosts && c.Connected {
			out = append(out, c)
		}
	}
	return out, nil
}
func createEventSocialPost(app *config.AppContext, conf, ref string, ch publishing.Channel, text string, assets []publishing.Media, reply string) (string, error) {
	// A stale event form must not requeue an item already handed to the previous publisher.
	old, err := getters.FindSocialPostByRef(app, ref)
	if err != nil {
		return "", err
	}
	if legacySocialPostBlocks(old) {
		return "", fmt.Errorf("This item has a previous publishing record; reconcile it before scheduling again")
	}
	return enqueueDirectEventPost(app, conf, ref, ch.ID, text, assets, reply, nil)
}
func enqueueDirectEventPost(app *config.AppContext, conf, ref, channel, text string, assets []publishing.Media, reply string, at *time.Time) (string, error) {
	d := publishing.Delivery{ChannelID: channel, Conference: conf, SourceRef: ref, Body: text, Reply: reply, Mode: "queue", At: at}
	if at != nil {
		d.Mode = "fixed"
	}
	for _, a := range assets {
		if !publishingMediaURL(a.URL) {
			return "", fmt.Errorf("Upload the post image to bitcoin++ storage first")
		}
		d.Media = append(d.Media, publishing.Media{URL: a.URL, Kind: a.Kind})
	}
	// Event forms may be submitted twice. Keep the first durable delivery.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var existingID, state string
	err := app.DB.QueryRow(ctx, `SELECT id::text,state FROM publishing_deliveries WHERE channel_id=$1 AND source_ref=$2`, channel, ref).Scan(&existingID, &state)
	if err == nil {
		if state == "canceled" {
			return "", fmt.Errorf("This post was canceled; create a new draft in Publishing")
		}
		return existingID, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	id, e := (publishing.Store{DB: app.DB}).Save(ctx, d)
	if e != nil {
		return "", e
	}
	return id, nil
}

func scheduleRecordingDirectPosts(app *config.AppContext, talks []*RecordingSpeakerCampaignTalk, channel string) (int, int, error) {
	queued, skipped := 0, 0
	for _, talk := range talks {
		if talk.Row.XURL != "" || socialPostBlocksAnnouncement(talk.Row.XSocialPost) {
			skipped++
			continue
		}
		old, e := getters.FindSocialPostByRef(app, recordingSocialPostRef(talk.Row.Recording, recordingPlatformLegacyX))
		if e != nil {
			return queued, skipped, e
		}
		// Never duplicate a delivery already accepted by Buffer, including an unknown result.
		if legacySocialPostBlocks(old) {
			skipped++
			continue
		}
		var conf string
		e = app.DB.QueryRow(app.DatabaseContext(), `SELECT coalesce(c.tag,'') FROM recordings r LEFT JOIN conf_talks ct ON ct.id=r.conf_talk_id LEFT JOIN conferences c ON c.id=ct.conference_id WHERE r.id=$1`, talk.Row.Recording.ID).Scan(&conf)
		if e != nil {
			return queued, skipped, e
		}
		var assets []publishing.Media
		if talk.CardURL != "" {
			assets = []publishing.Media{{URL: talk.CardURL, Kind: "image"}}
		}
		_, e = enqueueDirectEventPost(app, conf, "recording:"+talk.Row.Recording.ID+":announcement", channel, recordingAnnouncementText(talk), assets, "", &talk.AnnouncementAt)
		if e != nil {
			return queued, skipped, e
		}
		queued++
	}
	return queued, skipped, nil
}

func eventPublishingConfigured(app *config.AppContext) bool {
	id, err := directEventX(app)
	return err == nil && id != ""
}

// Unknown historical outcomes are deliberately retained for cutover review.
func legacySocialPostBlocks(post *types.SocialPost) bool {
	if post == nil || post.PostedTo == "publishing" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(post.Status)) {
	case "canceled", "cancelled":
		return false
	default:
		return true
	}
}
