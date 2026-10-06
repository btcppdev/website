package getters

import (
	"btcpp-web/internal/config"
	"fmt"
)

// PublicProjectSitemapURLs uses the same anonymous visibility rules as project
// pages, without doing a separate competition query for every project.
func PublicProjectSitemapURLs(ctx *config.AppContext) (map[string][]string, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
 SELECT c.conference_id::text,p.id::text FROM projects p
 JOIN competitions c ON c.id=p.competition_id
 JOIN conferences event ON event.id=c.conference_id
 WHERE c.visibility='public' AND c.public_gallery_enabled
 AND event.publication_status='published'
 AND p.status NOT IN ('created','hidden')
 ORDER BY c.conference_id,p.id`)
	if err != nil {
		return nil, fmt.Errorf("public project sitemap: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var conference, id string
		if err := rows.Scan(&conference, &id); err != nil {
			return nil, err
		}
		out[conference] = append(out[conference], id)
	}
	return out, rows.Err()
}
