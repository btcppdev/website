package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

// CreateAffiliateCode mints a new DiscountCode row owned by the
// dashboard user. Caller is responsible for uniqueness; see
// IsCodeNameAvailable.

// UpdateAffiliateCode patches an existing DiscountCode row owned by
// an affiliate.

// ArchiveAffiliateCode soft-deletes the DiscountCode row. Past
// AffiliateUsage rows stay put.

// AffiliateUsageInput is the data needed to record one redemption.
type AffiliateUsageInput struct {
	CodeName       string
	AffiliateEmail string
	ConfTag        string
	SavedSats      int64
	EarnedSats     int64
	TicketsCount   uint
}

// ListAffiliateUsage issues a live paginated query for every AffiliateUsageDb
// row. This is intended for admin/backfill jobs, not request paths.

// UpdateAffiliateUsageSats rewrites the stored sats split on an existing
// AffiliateUsage row.

// QueryAffiliateUsageByEmail issues a live query against AffiliateUsageDb
// filtering on the AffiliateEmail field.

// QueryAffiliateUsageByConf issues a live query against AffiliateUsageDb
// filtering on Conference == confTag.

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

func CreateAffiliateCode(ctx *config.AppContext, email, codeName string, buyerPct uint, confRefs []string) (string, error) {
	if strings.TrimSpace(email) == "" {
		return "", fmt.Errorf("CreateAffiliateCode: empty email")
	}
	if strings.TrimSpace(codeName) == "" {
		return "", fmt.Errorf("CreateAffiliateCode: empty codeName")
	}
	discountExpr := fmt.Sprintf("%%%d", buyerPct)
	if id, restored, err := reactivateArchivedAffiliateCodePostgres(ctx, email, codeName, discountExpr, confRefs); err != nil {
		return "", err
	} else if restored {
		return id, nil
	}
	return insertDiscountPostgres(ctx, codeName, discountExpr, email, confRefs)
}

func reactivateArchivedAffiliateCodePostgres(ctx *config.AppContext, email, codeName, discountExpr string, confRefs []string) (string, bool, error) {
	if ctx == nil || ctx.DB == nil {
		return "", false, fmt.Errorf("database is not configured")
	}
	discount, err := discountForWrite(codeName, discountExpr, email)
	if err != nil {
		return "", false, err
	}

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return "", false, fmt.Errorf("begin affiliate reactivation: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	discType := string(discount.DiscType)
	var discountID string
	err = tx.QueryRow(ctx.DatabaseContext(), `
		UPDATE discounts
		SET archived_at = NULL,
			discount_expr = $3,
			affiliate_email = NULLIF($2, '')::citext,
			disc_type = $4,
			amount = $5,
			max_uses = $6,
			extra_qty = $7,
			valid_from = $8,
			valid_until = $9
		WHERE code_name = $1
			AND affiliate_email = $2
			AND archived_at IS NOT NULL
		RETURNING id::text
	`, discount.CodeName, discount.AffiliateEmail, discount.Discount, discType,
		nullableUintPtr(discount.Amount), nullableUintPtr(discount.MaxUses),
		int(discount.ExtraQty), discount.ValidFrom, discount.ValidUntil).Scan(&discountID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("reactivate affiliate discount %q: %w", discount.CodeName, err)
	}
	if err := replaceDiscountConferenceLinksPostgres(tx, discountID, confRefs); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return "", false, fmt.Errorf("commit affiliate reactivation: %w", err)
	}
	return discountID, true, nil
}

func UpdateAffiliateCode(ctx *config.AppContext, codeID, codeName string, buyerPct uint, confRefs []string) error {
	if strings.TrimSpace(codeID) == "" {
		return fmt.Errorf("UpdateAffiliateCode: empty codeID")
	}
	return updateDiscountRowPostgres(ctx, codeID, codeName, fmt.Sprintf("%%%d", buyerPct), nil, confRefs)
}

func ArchiveAffiliateCode(ctx *config.AppContext, codeID string) error {
	return archiveDiscountRowPostgres(ctx, codeID)
}

func RecordAffiliateUsage(ctx *config.AppContext, in AffiliateUsageInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO affiliate_usages (
			discount_id, conference_id, code_name_snapshot, affiliate_email,
			saved_sats, earned_sats, tickets_count
		)
		VALUES (
			(SELECT id FROM discounts WHERE code_name = $1 LIMIT 1),
			(SELECT id FROM conferences WHERE tag = $2 LIMIT 1),
			$1, $3, $4, $5, $6
		)
	`, in.CodeName, in.ConfTag, in.AffiliateEmail, in.SavedSats, in.EarnedSats, int(in.TicketsCount))
	if err != nil {
		return fmt.Errorf("insert affiliate usage: %w", err)
	}
	return nil
}

func ListAffiliateUsage(ctx *config.AppContext) ([]*types.AffiliateUsage, error) {
	return queryAffiliateUsagePostgres(ctx, "", "")
}

func QueryAffiliateUsageByEmail(ctx *config.AppContext, email string) ([]*types.AffiliateUsage, error) {
	if email == "" {
		return nil, nil
	}
	return queryAffiliateUsagePostgres(ctx, "email", email)
}

func QueryAffiliateUsageByConf(ctx *config.AppContext, confTag string) ([]*types.AffiliateUsage, error) {
	if confTag == "" {
		return nil, nil
	}
	return queryAffiliateUsagePostgres(ctx, "conf", confTag)
}

func queryAffiliateUsagePostgres(ctx *config.AppContext, filter string, value string) ([]*types.AffiliateUsage, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	sql := `
		SELECT au.id::text, au.code_name_snapshot::text, au.affiliate_email::text,
			coalesce(c.tag, ''), au.saved_sats, au.earned_sats,
			au.tickets_count, au.created_at
		FROM affiliate_usages au
		LEFT JOIN conferences c ON c.id = au.conference_id
	`
	args := []any{}
	switch filter {
	case "email":
		sql += " WHERE au.affiliate_email = $1"
		args = append(args, value)
	case "conf":
		sql += " WHERE c.tag = $1"
		args = append(args, value)
	case "":
	default:
		return nil, fmt.Errorf("unknown affiliate usage filter %q", filter)
	}
	sql += " ORDER BY au.created_at DESC, au.id"

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query affiliate usages: %w", err)
	}
	defer rows.Close()

	var out []*types.AffiliateUsage
	for rows.Next() {
		var usage types.AffiliateUsage
		var ticketsCount int
		var created time.Time
		err := rows.Scan(
			&usage.ID,
			&usage.CodeName,
			&usage.AffiliateEmail,
			&usage.ConfTag,
			&usage.SavedSats,
			&usage.EarnedSats,
			&ticketsCount,
			&created,
		)
		if err != nil {
			return nil, fmt.Errorf("scan affiliate usage: %w", err)
		}
		usage.TicketsCount = uint(ticketsCount)
		usage.Created = &created
		out = append(out, &usage)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate affiliate usages: %w", err)
	}
	return out, nil
}

func UpdateAffiliateUsageSats(ctx *config.AppContext, usageID string, savedSats, earnedSats int64) error {
	if usageID == "" {
		return fmt.Errorf("UpdateAffiliateUsageSats: usageID is required")
	}
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE affiliate_usages
		SET saved_sats = $2, earned_sats = $3
		WHERE id = $1
	`, usageID, savedSats, earnedSats)
	if err != nil {
		return fmt.Errorf("update affiliate usage sats: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("affiliate usage %s not found", usageID)
	}
	return nil
}
