package getters

import (
	"fmt"
	"strings"

	"btcpp-web/internal/config"
)

// HomepageArchiveAsset is a lightweight image from the published event
// archive. The homepage uses a daily rotating sample for its decorative
// background animation instead of loading every historical image on every visit.
type HomepageArchiveAsset struct {
	Kind  string
	Image string
	Label string
}

func ListHomepageArchiveAssets(ctx *config.AppContext, perKind int) ([]*HomepageArchiveAsset, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if perKind < 1 {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH past_talk_art AS (
			SELECT DISTINCT ON (ct.clipart_path)
				ct.id::text AS source_id,
				ct.clipart_path AS image,
				proposal.title AS label
			FROM conf_talks ct
			JOIN proposals proposal ON proposal.id = ct.proposal_id
			JOIN conferences conf ON conf.id = ct.conference_id
			WHERE ct.archived_at IS NULL
			  AND btrim(ct.clipart_path) <> ''
			  AND proposal.status IN ('', 'Accepted', 'Scheduled')
			  AND conf.publication_status = 'published'
			  AND coalesce(conf.end_date, conf.start_date) < now()
			ORDER BY ct.clipart_path, conf.end_date DESC NULLS LAST, ct.id
		), talk_sample AS (
			SELECT 'talk'::text AS kind, source_id, image, label
			FROM past_talk_art
			ORDER BY md5(source_id || current_date::text)
			LIMIT $1
		), past_speakers AS (
			SELECT DISTINCT ON (person.id)
				person.id::text AS source_id,
				person.norm_photo_path AS image,
				person.name AS label
			FROM conf_talks ct
			JOIN proposals proposal ON proposal.id = ct.proposal_id
			JOIN proposals_speaker_confs psc ON psc.proposal_id = proposal.id
			JOIN speaker_confs sc ON sc.id = psc.speaker_conf_id
			JOIN people person ON person.id = sc.speaker_id
			JOIN conferences conf ON conf.id = ct.conference_id
			WHERE ct.archived_at IS NULL
			  AND btrim(person.norm_photo_path) <> ''
			  AND proposal.status IN ('', 'Accepted', 'Scheduled')
			  AND conf.publication_status = 'published'
			  AND coalesce(conf.end_date, conf.start_date) < now()
			ORDER BY person.id, conf.end_date DESC NULLS LAST, ct.id
		), speaker_sample AS (
			SELECT 'speaker'::text AS kind, source_id, image, label
			FROM past_speakers
			ORDER BY md5(source_id || current_date::text)
			LIMIT $1
		), sample AS (
			SELECT * FROM talk_sample
			UNION ALL
			SELECT * FROM speaker_sample
		)
		SELECT kind, image, label
		FROM sample
		ORDER BY md5(kind || source_id || current_date::text)
	`, perKind)
	if err != nil {
		return nil, fmt.Errorf("query homepage archive assets: %w", err)
	}
	defer rows.Close()
	assets := make([]*HomepageArchiveAsset, 0, perKind*2)
	for rows.Next() {
		asset := &HomepageArchiveAsset{}
		if err := rows.Scan(&asset.Kind, &asset.Image, &asset.Label); err != nil {
			return nil, fmt.Errorf("scan homepage archive asset: %w", err)
		}
		asset.Kind = strings.TrimSpace(asset.Kind)
		asset.Image = strings.TrimSpace(asset.Image)
		asset.Label = strings.TrimSpace(asset.Label)
		if asset.Image != "" {
			assets = append(assets, asset)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate homepage archive assets: %w", err)
	}
	return assets, nil
}
