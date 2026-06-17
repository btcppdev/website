package getters

import (
	"context"
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func listRunOfShowAdjustmentsPostgres(ctx *config.AppContext, confTag string) ([]*types.RunOfShowAdjustment, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT rsa.id::text, c.tag, rsa.venue, rsa.anchor_kind, rsa.anchor_id,
			rsa.delay_minutes, rsa.propagation_mode, rsa.note, rsa.created_at
		FROM run_of_show_adjustments rsa
		JOIN conferences c ON c.id = rsa.conference_id
		WHERE c.tag = $1
			AND rsa.archived_at IS NULL
		ORDER BY rsa.created_at, rsa.id
	`, confTag)
	if err != nil {
		return nil, fmt.Errorf("query run of show adjustments: %w", err)
	}
	defer rows.Close()

	var out []*types.RunOfShowAdjustment
	for rows.Next() {
		var adj types.RunOfShowAdjustment
		if err := rows.Scan(
			&adj.ID,
			&adj.ConfTag,
			&adj.VenueTag,
			&adj.AnchorKind,
			&adj.AnchorID,
			&adj.DelayMinutes,
			&adj.PropagationMode,
			&adj.Note,
			&adj.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run of show adjustment: %w", err)
		}
		out = append(out, &adj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run of show adjustments: %w", err)
	}
	return out, nil
}

func upsertRunOfShowAdjustmentPostgres(ctx *config.AppContext, in RunOfShowAdjustmentInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	mode := strings.TrimSpace(in.PropagationMode)
	if mode == "" {
		mode = RunOfShowAdjustUntilNextAnchor
	}
	_, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO run_of_show_adjustments (
			conference_id, venue, anchor_kind, anchor_id, delay_minutes, propagation_mode, note
		)
		SELECT c.id, $2, $3, $4, $5, $6, $7
		FROM conferences c
		WHERE c.tag = $1
		ON CONFLICT (conference_id, anchor_kind, anchor_id)
			WHERE archived_at IS NULL
		DO UPDATE SET
			venue = EXCLUDED.venue,
			delay_minutes = EXCLUDED.delay_minutes,
			propagation_mode = EXCLUDED.propagation_mode,
			note = EXCLUDED.note,
			updated_at = now()
	`, strings.TrimSpace(in.ConfTag), strings.TrimSpace(in.VenueTag), strings.TrimSpace(in.AnchorKind), strings.TrimSpace(in.AnchorID), in.DelayMinutes, mode, strings.TrimSpace(in.Note))
	if err != nil {
		return fmt.Errorf("upsert run of show adjustment: %w", err)
	}
	return nil
}

func archiveRunOfShowAdjustmentPostgres(ctx *config.AppContext, confTag, anchorKind, anchorID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	_, err := ctx.DB.Exec(context.Background(), `
		UPDATE run_of_show_adjustments rsa
		SET archived_at = now()
		FROM conferences c
		WHERE c.id = rsa.conference_id
			AND c.tag = $1
			AND rsa.anchor_kind = $2
			AND rsa.anchor_id = $3
			AND rsa.archived_at IS NULL
	`, strings.TrimSpace(confTag), strings.TrimSpace(anchorKind), strings.TrimSpace(anchorID))
	if err != nil {
		return fmt.Errorf("archive run of show adjustment: %w", err)
	}
	return nil
}
