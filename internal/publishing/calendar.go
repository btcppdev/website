package publishing

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func ValidPostURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || (u.Host != "x.com" && u.Host != "twitter.com") {
		return false
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) != 3 || p[1] != "status" || p[0] == "" || p[2] == "" {
		return false
	}
	for _, r := range p[2] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func (s Store) Calendar(ctx context.Context, start, end time.Time) ([]Entry, error) {
	if !end.After(start) || end.Sub(start) > 93*24*time.Hour {
		return nil, fmt.Errorf("Choose a calendar range of at most 93 days")
	}
	rows, e := s.DB.Query(ctx, `
 SELECT 'delivery:'||id::text,channel_id,conference_tag,coalesce(nullif(title,''),left(body,100)),'post',state,scheduled_at,NULL::timestamptz,published_url,'direct'
 FROM publishing_deliveries WHERE scheduled_at BETWEEN $1 AND $2 OR state IN ('draft','failed','needs_review')
 UNION ALL
 SELECT provider||':'||remote_id,channel_id,conference_tag,title,CASE WHEN provider='streamer' THEN 'stream' ELSE 'post' END,state,starts_at,ends_at,url,provider
 FROM publishing_external WHERE provider='streamer' AND (starts_at BETWEEN $1 AND $2 OR starts_at IS NULL)
 UNION ALL
 SELECT 'youtube:'||r.id::text,'youtube',coalesce(c.tag,''),r.talk_name,'recording',coalesce(nullif(sp.status,''),'scheduled'),r.publish_at,NULL::timestamptz,'/'||c.tag||'/admin/recordings/'||r.id::text,'recordings'
 FROM recordings r LEFT JOIN conf_talks ct ON ct.id=r.conf_talk_id LEFT JOIN conferences c ON c.id=ct.conference_id
 LEFT JOIN social_posts sp ON sp.ref='recording:'||r.id::text||':youtube'
 WHERE r.publish_at BETWEEN $1 AND $2
 UNION ALL
 SELECT 'broadcast:'||r.id::text,'broadcasts',coalesce(c.tag,''),r.talk_name,'stream',xb.status,xb.scheduled_at,xb.scheduled_at+(r.duration_seconds+300)*interval '1 second','/'||c.tag||'/admin/recordings/'||r.id::text,'recordings'
 FROM recording_x_broadcasts xb JOIN recordings r ON r.id=xb.recording_id LEFT JOIN conf_talks ct ON ct.id=r.conf_talk_id LEFT JOIN conferences c ON c.id=ct.conference_id
 WHERE xb.scheduled_at BETWEEN $1 AND $2
 ORDER BY 7 NULLS LAST,1 LIMIT 5000`, start, end)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var v Entry
		var link *string
		if e = rows.Scan(&v.ID, &v.ChannelID, &v.Conference, &v.Title, &v.Kind, &v.State, &v.At, &v.End, &link, &v.Provider); e != nil {
			return nil, e
		}
		if link != nil {
			v.URL = *link
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReplaceExternal is called only after fetching a complete snapshot. A failed
// fetch retains the last successful snapshot and its visible freshness time.
func (s Store) ReplaceExternal(ctx context.Context, provider string, channels []Channel, entries []Entry) error {
	if provider != "streamer" {
		return fmt.Errorf("Only streamer snapshots are supported")
	}
	for _, c := range channels {
		if c.Provider != "recordings" || c.Platform != "stream" {
			return fmt.Errorf("Invalid stream channel")
		}
	}

	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7330192402)`); e != nil {
		return e
	}
	for _, c := range channels {
		if c.Slots == nil {
			c.Slots = []Slot{}
		}
		_, e = tx.Exec(ctx, `INSERT INTO publishing_channels(id,platform,name,provider,remote_id,timezone,slots,paused) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,timezone=EXCLUDED.timezone,slots=EXCLUDED.slots,paused=EXCLUDED.paused,updated_at=now()`, c.ID, c.Platform, c.Name, c.Provider, c.RemoteID, c.Timezone, c.Slots, c.Paused)
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `DELETE FROM publishing_external WHERE provider=$1`, provider); e != nil {
		return e
	}
	for _, v := range entries {
		if _, e = tx.Exec(ctx, `INSERT INTO publishing_external(provider,remote_id,channel_id,conference_tag,title,state,starts_at,ends_at,url) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, provider, v.ID, v.ChannelID, v.Conference, v.Title, v.State, v.At, v.End, v.URL); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO publishing_sync(provider,synced_at) VALUES($1,now()) ON CONFLICT(provider) DO UPDATE SET synced_at=now()`, provider); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
