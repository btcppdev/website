package youtube

import (
	"context"
	"google.golang.org/api/option"
	youtubeapi "google.golang.org/api/youtube/v3"
)

type VideoMetric struct {
	ID, Title, Thumbnail string
	Views                uint64
}

// VideoMetrics reads public lifetime counters using the existing readonly scope.
// Private and unlisted videos are excluded from sponsor-facing reports.
func VideoMetrics(ctx context.Context, ids []string) ([]VideoMetric, error) {
	client, err := httpClient(ctx)
	if err != nil {
		return nil, err
	}
	svc, err := youtubeapi.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	var result []VideoMetric
	for offset := 0; offset < len(ids); offset += 50 {
		end := offset + 50
		if end > len(ids) {
			end = len(ids)
		}
		response, err := svc.Videos.List([]string{"snippet", "statistics", "status"}).Id(ids[offset:end]...).Do()
		if err != nil {
			return nil, err
		}
		for _, v := range response.Items {
			if v.Status == nil || v.Status.PrivacyStatus != "public" || v.Snippet == nil || v.Statistics == nil {
				continue
			}
			m := VideoMetric{ID: v.Id, Title: v.Snippet.Title, Views: v.Statistics.ViewCount}
			if v.Snippet.Thumbnails != nil {
				if v.Snippet.Thumbnails.High != nil {
					m.Thumbnail = v.Snippet.Thumbnails.High.Url
				} else if v.Snippet.Thumbnails.Default != nil {
					m.Thumbnail = v.Snippet.Thumbnails.Default.Url
				}
			}
			result = append(result, m)
		}
	}
	return result, nil
}
