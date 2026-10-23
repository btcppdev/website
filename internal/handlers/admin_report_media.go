package handlers

import (
	"btcpp-web/external/xstudio"
	"btcpp-web/external/youtube"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type reportMedia struct {
	Platform, ID, Title, URL, Thumbnail, Collected, Basis, MomentURL, Event, SocialCard string
	Views                                                                               int64
}

func (p sponsorReportPage) Media() []reportMedia {
	var media []reportMedia
	_ = json.Unmarshal([]byte(p.Saved["media_snapshot"]), &media)
	attachReportSocialCards(media, p.Sessions)
	sort.SliceStable(media, func(i, j int) bool {
		if media[i].Platform != media[j].Platform {
			return media[i].Platform < media[j].Platform
		}
		return media[i].Views > media[j].Views
	})
	return media
}

var reportYouTubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var reportBroadcastID = regexp.MustCompile(`^[A-Za-z0-9]+$`)

func reportMediaIdentity(raw string) (platform, id string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		id = u.Query().Get("v")
		for _, prefix := range []string{"/live/", "/shorts/", "/embed/"} {
			if strings.HasPrefix(u.Path, prefix) {
				id = strings.TrimPrefix(u.Path, prefix)
			}
		}
		if reportYouTubeID.MatchString(id) {
			return "YouTube", id
		}
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
		if reportYouTubeID.MatchString(id) {
			return "YouTube", id
		}
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
		if strings.HasPrefix(u.Path, "/i/broadcasts/") {
			id = strings.TrimPrefix(u.Path, "/i/broadcasts/")
			if reportBroadcastID.MatchString(id) {
				return "X", id
			}
		}
	}
	return "", ""
}

func saveReportMediaFields(form url.Values, saved, previous map[string]string) error {
	saved["media_snapshot"] = previous["media_snapshot"]
	raw := strings.TrimSpace(form.Get("media_links"))
	if len(raw) > 12000 {
		return fmt.Errorf("media links exceed 12,000 bytes")
	}
	count := 0
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		count++
		platform, _ := reportMediaIdentity(fields[0])
		if platform == "" || len(fields) > 2 {
			return fmt.Errorf("use a YouTube video or X broadcast URL, optionally followed by start seconds")
		}
		if len(fields) == 2 {
			seconds, err := strconv.Atoi(fields[1])
			if err != nil || seconds < 0 || seconds > 86400 || platform != "YouTube" {
				return fmt.Errorf("moment links require a YouTube URL and start seconds between 0 and 86400")
			}
		}
	}
	if count > 100 {
		return fmt.Errorf("at most 100 additional media links per event")
	}
	saved["media_links"] = raw
	return nil
}

// Refresh is explicit, outside database transactions. A failed platform retains its
// previous snapshot; successful platforms replace theirs, removing unavailable media.
func refreshReportMedia(ctx context.Context, app *config.AppContext, p *sponsorReportPage, saved map[string]string) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	wanted := map[string]map[string]int{"YouTube": {}, "X": {}}
	add := func(raw string, seconds int) {
		platform, id := reportMediaIdentity(raw)
		if platform != "" {
			wanted[platform][id] = seconds
		}
	}
	for _, s := range p.Sessions {
		add(s.YouTube, 0)
		add(s.X, 0)
	}
	for _, line := range strings.Split(saved["media_links"], "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			seconds := 0
			if len(fields) > 1 {
				seconds, _ = strconv.Atoi(fields[1])
			}
			add(fields[0], seconds)
		}
	}
	var previous []reportMedia
	_ = json.Unmarshal([]byte(saved["media_snapshot"]), &previous)
	var result []reportMedia
	var notices []string
	collected := time.Now().UTC().Format(time.RFC3339)
	for _, platform := range []string{"YouTube", "X"} {
		var next []reportMedia
		var err error
		ids := make([]string, 0, len(wanted[platform]))
		for id := range wanted[platform] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) > 0 && platform == "YouTube" {
			var videos []youtube.VideoMetric
			videos, err = youtube.VideoMetrics(ctx, ids)
			for _, v := range videos {
				m := reportMedia{Platform: platform, ID: v.ID, Title: v.Title, URL: "https://www.youtube.com/watch?v=" + v.ID, Thumbnail: v.Thumbnail, Views: int64(v.Views), Collected: collected, Basis: "YouTube Data API · lifetime public video views"}
				if seconds := wanted[platform][v.ID]; seconds > 0 {
					m.MomentURL = m.URL + "&t=" + strconv.Itoa(seconds) + "s"
				}
				next = append(next, m)
			}
		} else if len(ids) > 0 {
			var client *xstudio.Client
			client, err = xstudio.New(xstudio.Config{Cookie: app.Env.Recordings.X.Cookie, UserAgent: app.Env.Recordings.X.UserAgent, IngestID: app.Env.Recordings.X.IngestID})
			if err == nil {
				var broadcasts map[string]xstudio.BroadcastMetric
				broadcasts, err = client.BroadcastMetrics(ctx)
				if err == nil {
					for _, id := range ids {
						b, ok := broadcasts[id]
						if !ok {
							continue
						}
						var views *int64
						views, err = client.BroadcastViews(ctx, id)
						if err != nil {
							break
						}
						next = append(next, reportMedia{Platform: platform, ID: id, Title: b.Title, URL: "https://x.com/i/broadcasts/" + id, Thumbnail: b.Thumbnail, Views: *views, Collected: collected, Basis: "X Media Studio · lifetime broadcast views (live + replay)"})
					}
				}
			}
		}
		if err != nil {
			for _, m := range previous {
				if m.Platform == platform {
					result = append(result, m)
				}
			}
			notices = append(notices, platform+": refresh unavailable; previous snapshot retained. Check the account connection and retry.")
		} else {
			for i := range next {
				next[i].Thumbnail = reportThumbnail(ctx, next[i].Thumbnail)
				if next[i].Thumbnail == "" {
					for _, old := range previous {
						if old.Platform == next[i].Platform && old.ID == next[i].ID {
							next[i].Thumbnail = old.Thumbnail
							break
						}
					}
				}
			}
			result = append(result, next...)
			notices = append(notices, fmt.Sprintf("%s: refreshed %d public recordings.", platform, len(next)))
		}
	}
	attachReportSocialCards(result, p.Sessions)
	for i := range result {
		result[i].Event = p.Event.Location
	}
	raw, _ := json.Marshal(result)
	saved["media_snapshot"] = string(raw)
	saved["media_status"] = strings.Join(notices, " ")
}

func reportMediaPages(media []reportMedia) [][]reportMedia {
	var pages [][]reportMedia
	for len(media) > 0 {
		n := 4
		if n > len(media) {
			n = len(media)
		}
		pages = append(pages, media[:n])
		media = media[n:]
	}
	return pages
}
func (p sponsorReportPage) MediaPages() [][]reportMedia {
	return reportMediaPages(reportMediaHighlights(p.Media()))
}
func (e annualEvent) MediaPages() [][]reportMedia { return reportMediaPages(e.Media) }

func (m reportMedia) CollectionDate() string {
	if t, err := time.Parse(time.RFC3339Nano, m.Collected); err == nil {
		return t.Format("2 Jan 2006 · 15:04 UTC")
	}
	return m.Collected
}

// Match on platform IDs, never titles: a broadcast may have been renamed.
func attachReportSocialCards(media []reportMedia, sessions []reportSession) {
	cards := map[string]string{}
	for _, s := range sessions {
		if s.SocialCard == "" {
			continue
		}
		for _, raw := range []string{s.YouTube, s.X} {
			platform, id := reportMediaIdentity(raw)
			if platform != "" {
				cards[platform+":"+id] = s.SocialCard
			}
		}
	}
	for i := range media {
		if card := cards[media[i].Platform+":"+media[i].ID]; card != "" {
			media[i].SocialCard = card
		}
	}
}
