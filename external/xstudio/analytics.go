package xstudio

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// BroadcastMetric contains only public media metadata and the Studio view count.
// Ingest credentials and unlabeled broadcast counters are deliberately excluded.
type BroadcastMetric struct {
	ID        string `json:"broadcastId"`
	Title     string `json:"title"`
	Thumbnail string `json:"imageUrl"`
	Locked    bool   `json:"isLocked"`
	Started   int64  `json:"startTime"`
	Poster    string `json:"preLiveSlateUrl"`
}

func (c *Client) BroadcastMetrics(ctx context.Context) (map[string]BroadcastMetric, error) {
	result := map[string]BroadcastMetric{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		q := url.Values{"limit": {"100"}, "rwebShell": {"1"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var response envelope[struct {
			Rows    []BroadcastMetric `json:"rows"`
			Cursor  string            `json:"cursor"`
			CanRead bool              `json:"canReadAnalytics"`
		}]
		if _, err := c.do(ctx, http.MethodGet, "/api/live/get-broadcasts?"+q.Encode(), "", "", nil, &response); err != nil {
			return nil, err
		}
		if err := checkEnvelope(response.Error); err != nil {
			return nil, err
		}
		if !response.Value.CanRead {
			return nil, fmt.Errorf("X Studio analytics access unavailable")
		}
		for _, b := range response.Value.Rows {
			if !b.Locked && b.Started > 0 {
				if b.Thumbnail == "" {
					b.Thumbnail = b.Poster
				}
				result[b.ID] = b
			}
		}
		cursor = response.Value.Cursor
		if cursor == "" {
			return result, nil
		}
		if seen[cursor] {
			return nil, fmt.Errorf("X Studio repeated pagination cursor")
		}
		seen[cursor] = true
	}
	return nil, fmt.Errorf("X Studio pagination limit exceeded")
}

func (c *Client) BroadcastViews(ctx context.Context, id string) (*int64, error) {
	var response envelope[map[string]int64]
	q := url.Values{"ids": {id}, "rwebShell": {"1"}}
	if _, err := c.do(ctx, http.MethodGet, "/api/live/get-broadcast-view-counts?"+q.Encode(), "", "", nil, &response); err != nil {
		return nil, err
	}
	if err := checkEnvelope(response.Error); err != nil {
		return nil, err
	}
	v, ok := response.Value[id]
	if !ok {
		return nil, fmt.Errorf("X Studio did not return a view count")
	}
	return &v, nil
}
