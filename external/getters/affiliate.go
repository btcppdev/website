package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// CreateAffiliateCode mints a new DiscountCode row owned by the
// dashboard user. Caller is responsible for uniqueness; see
// IsCodeNameAvailable.
func CreateAffiliateCode(ctx *config.AppContext, email, codeName string, buyerPct uint, confRefs []string) (string, error) {
	if UsePostgresBackend(ctx) {
		return createAffiliateCodePostgres(ctx, email, codeName, buyerPct, confRefs)
	}
	if email == "" {
		return "", fmt.Errorf("CreateAffiliateCode: empty email")
	}
	if codeName == "" {
		return "", fmt.Errorf("CreateAffiliateCode: empty codeName")
	}
	return createAffiliateCodeNotion(ctx.Notion, email, codeName, buyerPct, confRefs)
}

// UpdateAffiliateCode patches an existing DiscountCode row owned by
// an affiliate.
func UpdateAffiliateCode(ctx *config.AppContext, codeID, codeName string, buyerPct uint, confRefs []string) error {
	if UsePostgresBackend(ctx) {
		return updateAffiliateCodePostgres(ctx, codeID, codeName, buyerPct, confRefs)
	}
	if codeID == "" {
		return fmt.Errorf("UpdateAffiliateCode: empty codeID")
	}
	return updateAffiliateCodeNotion(ctx, codeID, codeName, buyerPct, confRefs)
}

// ArchiveAffiliateCode soft-deletes the DiscountCode row. Past
// AffiliateUsage rows stay put.
func ArchiveAffiliateCode(ctx *config.AppContext, codeID string) error {
	if UsePostgresBackend(ctx) {
		return archiveAffiliateCodePostgres(ctx, codeID)
	}
	return archiveAffiliateCodeNotion(ctx, codeID)
}

// AffiliateUsageInput is the data needed to record one redemption.
type AffiliateUsageInput struct {
	CodeName       string
	AffiliateEmail string
	ConfTag        string
	SavedSats      int64
	EarnedSats     int64
	TicketsCount   uint
}

func RecordAffiliateUsage(ctx *config.AppContext, in AffiliateUsageInput) error {
	if UsePostgresBackend(ctx) {
		return recordAffiliateUsagePostgres(ctx, in)
	}
	return recordAffiliateUsageNotion(ctx, in)
}

// ListAffiliateUsage issues a live paginated query for every AffiliateUsageDb
// row. This is intended for admin/backfill jobs, not request paths.
func ListAffiliateUsage(ctx *config.AppContext) ([]*types.AffiliateUsage, error) {
	if UsePostgresBackend(ctx) {
		return listAffiliateUsagePostgres(ctx)
	}
	return listAffiliateUsageNotion(ctx)
}

// UpdateAffiliateUsageSats rewrites the stored sats split on an existing
// AffiliateUsage row.
func UpdateAffiliateUsageSats(ctx *config.AppContext, usageID string, savedSats, earnedSats int64) error {
	if UsePostgresBackend(ctx) {
		return updateAffiliateUsageSatsPostgres(ctx, usageID, savedSats, earnedSats)
	}
	return updateAffiliateUsageSatsNotion(ctx, usageID, savedSats, earnedSats)
}

// QueryAffiliateUsageByEmail issues a live query against AffiliateUsageDb
// filtering on the AffiliateEmail field.
func QueryAffiliateUsageByEmail(ctx *config.AppContext, email string) ([]*types.AffiliateUsage, error) {
	if UsePostgresBackend(ctx) {
		return queryAffiliateUsageByEmailPostgres(ctx, email)
	}
	return queryAffiliateUsageByEmailNotion(ctx, email)
}

// QueryAffiliateUsageByConf issues a live query against AffiliateUsageDb
// filtering on Conference == confTag.
func QueryAffiliateUsageByConf(ctx *config.AppContext, confTag string) ([]*types.AffiliateUsage, error) {
	if UsePostgresBackend(ctx) {
		return queryAffiliateUsageByConfPostgres(ctx, confTag)
	}
	return queryAffiliateUsageByConfNotion(ctx, confTag)
}

// AffiliateStatsTotals are the aggregate numbers shown on the dashboard's
// affiliate section.
type AffiliateStatsTotals struct {
	TicketsSold int
	SavedSats   int64
	EarnedSats  int64
}

// SumAffiliateStatsByEmail aggregates every AffiliateUsage row for an email.
func SumAffiliateStatsByEmail(ctx *config.AppContext, email string) (AffiliateStatsTotals, error) {
	rows, err := QueryAffiliateUsageByEmail(ctx, email)
	if err != nil {
		return AffiliateStatsTotals{}, err
	}
	var totals AffiliateStatsTotals
	for _, r := range rows {
		if r == nil {
			continue
		}
		totals.TicketsSold += int(r.TicketsCount)
		totals.SavedSats += r.SavedSats
		totals.EarnedSats += r.EarnedSats
	}
	return totals, nil
}
