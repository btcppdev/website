package getters

import (
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const (
	ConferenceMilestoneTickets = "tickets"
	ConferenceMilestoneTalks   = "talks"
	ConferenceMilestoneEvent   = "event"
	ConferenceMilestoneOther   = "other"
)

type ConferenceMilestoneInput struct {
	ID        string
	Label     string
	Category  string
	OccursAt  time.Time
	URL       string
	Published bool
}

func ListConferenceMilestones(ctx *config.AppContext, confRef string, includeUnpublished bool) ([]*types.ConferenceMilestone, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, conference_id::text, label, category, occurs_at, url, published
		FROM conference_milestones
		WHERE conference_id = $1::uuid
		  AND ($2 OR published)
		ORDER BY occurs_at, created_at, id
	`, confRef, includeUnpublished)
	if err != nil {
		return nil, fmt.Errorf("list conference milestones: %w", err)
	}
	defer rows.Close()

	var milestones []*types.ConferenceMilestone
	for rows.Next() {
		var milestone types.ConferenceMilestone
		if err := rows.Scan(&milestone.ID, &milestone.ConfRef, &milestone.Label, &milestone.Category, &milestone.OccursAt, &milestone.URL, &milestone.Published); err != nil {
			return nil, fmt.Errorf("scan conference milestone: %w", err)
		}
		milestones = append(milestones, &milestone)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conference milestones: %w", err)
	}
	return milestones, nil
}

func UpsertConferenceMilestone(ctx *config.AppContext, confRef string, in ConferenceMilestoneInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	in.Label = strings.TrimSpace(in.Label)
	in.Category = normalizeConferenceMilestoneCategory(in.Category)
	in.URL = strings.TrimSpace(in.URL)
	if in.Label == "" {
		return fmt.Errorf("milestone label is required")
	}
	if in.OccursAt.IsZero() {
		return fmt.Errorf("milestone date is required")
	}

	if strings.TrimSpace(in.ID) == "" {
		_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO conference_milestones (conference_id, label, category, occurs_at, url, published)
			VALUES ($1::uuid, $2, $3, $4, $5, $6)
		`, confRef, in.Label, in.Category, in.OccursAt, in.URL, in.Published)
		if err != nil {
			return fmt.Errorf("create conference milestone: %w", err)
		}
		return nil
	}

	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE conference_milestones
		SET label = $3, category = $4, occurs_at = $5, url = $6, published = $7
		WHERE id = $1::uuid AND conference_id = $2::uuid
	`, in.ID, confRef, in.Label, in.Category, in.OccursAt, in.URL, in.Published)
	if err != nil {
		return fmt.Errorf("update conference milestone: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conference milestone not found")
	}
	return nil
}

func DeleteConferenceMilestone(ctx *config.AppContext, confRef, milestoneID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM conference_milestones
		WHERE id = $1::uuid AND conference_id = $2::uuid
	`, strings.TrimSpace(milestoneID), confRef)
	if err != nil {
		return fmt.Errorf("delete conference milestone: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conference milestone not found")
	}
	return nil
}

func normalizeConferenceMilestoneCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case ConferenceMilestoneTickets:
		return ConferenceMilestoneTickets
	case ConferenceMilestoneTalks:
		return ConferenceMilestoneTalks
	case ConferenceMilestoneOther:
		return ConferenceMilestoneOther
	default:
		return ConferenceMilestoneEvent
	}
}
