package getters

import (
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func ListPersonBadgePresentations(ctx *config.AppContext, personID string) ([]*types.PersonBadgePresentation, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT badge_ref, hidden, coalesce(featured_position, 0)
		FROM person_badge_presentations
		WHERE person_id=$1::uuid
		ORDER BY featured_position NULLS LAST, badge_ref`, strings.TrimSpace(personID))
	if err != nil {
		return nil, fmt.Errorf("list person badge presentations: %w", err)
	}
	defer rows.Close()
	var items []*types.PersonBadgePresentation
	for rows.Next() {
		item := &types.PersonBadgePresentation{}
		if err := rows.Scan(&item.BadgeRef, &item.Hidden, &item.FeaturedPosition); err != nil {
			return nil, fmt.Errorf("scan person badge presentation: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ReplacePersonBadgePresentations stores one complete presentation snapshot.
// Locking the person row serializes updates from multiple tabs; the final
// submitted snapshot wins without ever mutating the underlying credentials.
func ReplacePersonBadgePresentations(ctx *config.AppContext, personID string, items []*types.PersonBadgePresentation) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	seenRefs := make(map[string]struct{}, len(items))
	seenPositions := make(map[int]struct{}, 6)
	for _, item := range items {
		if item == nil {
			return fmt.Errorf("badge presentation is required")
		}
		item.BadgeRef = strings.TrimSpace(item.BadgeRef)
		if item.BadgeRef == "" || len(item.BadgeRef) > 512 || item.FeaturedPosition < 0 || item.FeaturedPosition > 6 || (item.Hidden && item.FeaturedPosition != 0) {
			return fmt.Errorf("invalid badge presentation")
		}
		if _, exists := seenRefs[item.BadgeRef]; exists {
			return fmt.Errorf("duplicate badge presentation")
		}
		seenRefs[item.BadgeRef] = struct{}{}
		if item.FeaturedPosition > 0 {
			if _, exists := seenPositions[item.FeaturedPosition]; exists {
				return fmt.Errorf("duplicate featured badge position")
			}
			seenPositions[item.FeaturedPosition] = struct{}{}
		}
	}

	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin badge presentation update: %w", err)
	}
	defer tx.Rollback(dbctx)
	var lockedID string
	if err := tx.QueryRow(dbctx, `SELECT id::text FROM people WHERE id=$1::uuid FOR UPDATE`, personID).Scan(&lockedID); err != nil {
		return fmt.Errorf("lock badge presentation owner: %w", err)
	}
	if _, err := tx.Exec(dbctx, `DELETE FROM person_badge_presentations WHERE person_id=$1::uuid`, personID); err != nil {
		return fmt.Errorf("replace badge presentations: %w", err)
	}
	for _, item := range items {
		if _, err := tx.Exec(dbctx, `
			INSERT INTO person_badge_presentations (person_id, badge_ref, hidden, featured_position)
			VALUES ($1::uuid, $2, $3, NULLIF($4, 0))`, personID, item.BadgeRef, item.Hidden, item.FeaturedPosition); err != nil {
			return fmt.Errorf("store badge presentation: %w", err)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit badge presentation update: %w", err)
	}
	return nil
}
