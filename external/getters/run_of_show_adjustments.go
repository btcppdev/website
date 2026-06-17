package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const (
	RunOfShowAdjustUntilNextAnchor = "until_next_anchor"
	RunOfShowAdjustPushFollowing   = "push_following"
	RunOfShowAdjustItemOnly        = "item_only"
)

type RunOfShowAdjustmentInput struct {
	ConfTag         string
	VenueTag        string
	AnchorKind      string
	AnchorID        string
	DelayMinutes    int
	PropagationMode string
	Note            string
}

func ListRunOfShowAdjustments(ctx *config.AppContext, confTag string) ([]*types.RunOfShowAdjustment, error) {
	if UsePostgresBackend(ctx) {
		return listRunOfShowAdjustmentsPostgres(ctx, confTag)
	}
	return nil, nil
}

func UpsertRunOfShowAdjustment(ctx *config.AppContext, in RunOfShowAdjustmentInput) error {
	if UsePostgresBackend(ctx) {
		return upsertRunOfShowAdjustmentPostgres(ctx, in)
	}
	return fmt.Errorf("run of show adjustments require the postgres backend")
}

func ArchiveRunOfShowAdjustment(ctx *config.AppContext, confTag, anchorKind, anchorID string) error {
	if UsePostgresBackend(ctx) {
		return archiveRunOfShowAdjustmentPostgres(ctx, confTag, anchorKind, anchorID)
	}
	return fmt.Errorf("run of show adjustments require the postgres backend")
}
