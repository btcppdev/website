package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

// DiscountInput is the normalized write shape for admin-created
// DiscountsDb rows. DiscountExpr uses the existing compact grammar
// parsed by types.DiscountCode.ParseDiscountExpr.
type DiscountInput struct {
	CodeName       string
	DiscountExpr   string
	ConfRef        string
	AffiliateEmail string
}

func listDiscounts(ctx *config.AppContext) ([]*types.DiscountCode, error) {
	return ListDiscounts(ctx)
}

func FindDiscount(ctx *config.AppContext, code string) (*types.DiscountCode, error) {
	return GetDiscountByCode(ctx, code)
}

func FindAffiliateCodeByEmail(ctx *config.AppContext, email string) (*types.DiscountCode, error) {
	return GetDiscountByAffiliateEmail(ctx, email)
}

func findDiscount(ctx *config.AppContext, code string) (*types.DiscountCode, error) {
	discounts, err := listDiscounts(ctx)
	if err != nil {
		return nil, err
	}

	upcode := strings.ToUpper(code)
	for _, discount := range discounts {
		if strings.ToUpper(discount.CodeName) == upcode {
			return discount, nil
		}
	}
	return nil, nil
}

func getDiscountByRef(ctx *config.AppContext, ref string) (*types.DiscountCode, error) {
	if ref == "" {
		return nil, nil
	}
	discounts, err := listDiscounts(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range discounts {
		if d != nil && d.Ref == ref {
			return d, nil
		}
	}
	return nil, nil
}

func findAffiliateCodeByEmail(ctx *config.AppContext, email string) (*types.DiscountCode, error) {
	if email == "" {
		return nil, nil
	}
	discounts, err := listDiscounts(ctx)
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(email)
	for _, d := range discounts {
		if d != nil && strings.ToLower(d.AffiliateEmail) == target {
			return d, nil
		}
	}
	return nil, nil
}

func isCodeNameAvailable(ctx *config.AppContext, codeName string) (bool, error) {
	if codeName == "" {
		return false, nil
	}
	discounts, err := listDiscounts(ctx)
	if err != nil {
		return false, err
	}
	target := strings.ToUpper(codeName)
	for _, d := range discounts {
		if d != nil && strings.ToUpper(d.CodeName) == target {
			return false, nil
		}
	}
	return true, nil
}

func CalcDiscount(ctx *config.AppContext, confRef string, code string, tixPrice uint, count uint) (uint, *types.DiscountCode, error) {
	discount, err := FindDiscount(ctx, code)

	if err != nil {
		return tixPrice * count, nil, err
	}

	/* Discount not found! */
	if discount == nil {
		return tixPrice * count, nil, fmt.Errorf("Discount code \"%s\" not found", code)
	}

	// Empty ConfRef = wildcard. Used by self-service affiliate
	// codes (which mint without any Conference relation) so a
	// single user code applies at every active event without an
	// admin re-attaching it per launch. Admin-created codes that
	// want this universal-redemption behavior can leave the
	// Conference relation empty too.
	if len(discount.ConfRef) > 0 {
		found := false
		for _, discountConfRef := range discount.ConfRef {
			found = found || discountConfRef == confRef
		}
		if !found {
			return tixPrice * count, nil, fmt.Errorf("%s not a valid code for conference (%s)", code, confRef)
		}
	}

	if discount.MaxUses > 0 && discount.UsesCount >= discount.MaxUses {
		return tixPrice * count, nil, fmt.Errorf("Discount code \"%s\" has been fully redeemed", code)
	}
	if discount.IsDateExpired(time.Now().UTC()) {
		return tixPrice * count, nil, fmt.Errorf("Discount code \"%s\" has expired", code)
	}

	if count == 0 {
		count = 1
	}

	total := discount.CalcTotal(tixPrice, count)
	return total, discount, nil
}

// CreateDiscount inserts a DiscountsDb row scoped to a single
// conference. AffiliateEmail is optional; when set, successful
// checkouts using the code will be credited to that affiliate.

// UpdateDiscount patches an existing DiscountsDb row. The admin UI
// always submits the full editable shape, including the event relation,
// so this intentionally rewrites the code, expression, relation, and
// optional affiliate email together.

// ArchiveDiscount soft-deletes a DiscountsDb row in Notion. Past
// purchase rows keep their discount-ref history; future checkout
// lookups stop seeing the archived code after cache refresh.

func CreateDiscount(ctx *config.AppContext, in DiscountInput) (string, error) {
	if in.CodeName == "" {
		return "", fmt.Errorf("CreateDiscount: CodeName is required")
	}
	if in.DiscountExpr == "" {
		return "", fmt.Errorf("CreateDiscount: DiscountExpr is required")
	}
	if in.ConfRef == "" {
		return "", fmt.Errorf("CreateDiscount: ConfRef is required")
	}
	return insertDiscountPostgres(ctx, in.CodeName, in.DiscountExpr, in.AffiliateEmail, []string{in.ConfRef})
}

func ListDiscounts(ctx *config.AppContext) ([]*types.DiscountCode, error) {
	return queryDiscountsPostgres(ctx, "all discounts", `
		discounts.archived_at IS NULL
	`)
}

func ListDiscountsForConf(ctx *config.AppContext, confRef string) ([]*types.DiscountCode, error) {
	confRef = strings.TrimSpace(confRef)
	if confRef == "" {
		return nil, nil
	}
	return queryDiscountsPostgres(ctx, "discounts for conf", `
		discounts.archived_at IS NULL
			AND conferences.id::text = $1
	`, confRef)
}

func GetDiscountByCode(ctx *config.AppContext, code string) (*types.DiscountCode, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	out, err := queryDiscountsPostgres(ctx, "discount by code", `
		discounts.archived_at IS NULL
			AND discounts.code_name = $1
	`, code)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return out[0], nil
}

func GetDiscountByRef(ctx *config.AppContext, ref string) (*types.DiscountCode, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	out, err := queryDiscountsPostgres(ctx, "discount by ref", `
		discounts.archived_at IS NULL
			AND discounts.id::text = $1
	`, ref)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return out[0], nil
}

func GetDiscountByAffiliateEmail(ctx *config.AppContext, email string) (*types.DiscountCode, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}
	out, err := queryDiscountsPostgres(ctx, "discount by affiliate email", `
		discounts.archived_at IS NULL
			AND discounts.affiliate_email = $1
	`, email)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return out[0], nil
}

func IsCodeNameAvailable(ctx *config.AppContext, codeName string) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("database is not configured")
	}
	codeName = strings.TrimSpace(codeName)
	if codeName == "" {
		return false, nil
	}
	var exists bool
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1
			FROM discounts
			WHERE archived_at IS NULL
				AND code_name = $1
		)
	`, codeName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check discount code availability %q: %w", codeName, err)
	}
	return !exists, nil
}

func queryDiscountsPostgres(ctx *config.AppContext, label string, whereSQL string, args ...any) ([]*types.DiscountCode, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT discounts.id::text, discounts.code_name::text, discounts.discount_expr,
			discounts.uses_count, coalesce(discounts.affiliate_email::text, ''),
			coalesce(conferences.id::text, '')
		FROM discounts
		LEFT JOIN discounts_conferences ON discounts_conferences.discount_id = discounts.id
		LEFT JOIN conferences ON conferences.id = discounts_conferences.conference_id
		WHERE `+whereSQL+`
		ORDER BY discounts.code_name, conferences.tag
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	byID := make(map[string]*types.DiscountCode)
	var out []*types.DiscountCode
	for rows.Next() {
		var id string
		var confRef string
		var usesCount int64
		discount := &types.DiscountCode{}
		err := rows.Scan(
			&id,
			&discount.CodeName,
			&discount.Discount,
			&usesCount,
			&discount.AffiliateEmail,
			&confRef,
		)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}

		existing := byID[id]
		if existing == nil {
			discount.Ref = id
			discount.UsesCount = uint(usesCount)
			_ = discount.ParseDiscountExpr()
			byID[id] = discount
			out = append(out, discount)
			existing = discount
		}
		if confRef != "" {
			existing.ConfRef = append(existing.ConfRef, confRef)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return out, nil
}

func IncrementDiscountUses(ctx *config.AppContext, discountRef string, addCount uint) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE discounts
		SET uses_count = uses_count + $2
		WHERE id = $1
	`, discountRef, int64(addCount))
	if err != nil {
		return fmt.Errorf("increment discount uses: %w", err)
	}
	return nil
}

func UpdateDiscount(ctx *config.AppContext, discountID string, in DiscountInput) error {
	if discountID == "" {
		return fmt.Errorf("UpdateDiscount: discountID is required")
	}
	if in.CodeName == "" {
		return fmt.Errorf("UpdateDiscount: CodeName is required")
	}
	if in.DiscountExpr == "" {
		return fmt.Errorf("UpdateDiscount: DiscountExpr is required")
	}
	if in.ConfRef == "" {
		return fmt.Errorf("UpdateDiscount: ConfRef is required")
	}
	return updateDiscountRowPostgres(ctx, discountID, in.CodeName, in.DiscountExpr, &in.AffiliateEmail, []string{in.ConfRef})
}

func ArchiveDiscount(ctx *config.AppContext, discountID string) error {
	if discountID == "" {
		return fmt.Errorf("ArchiveDiscount: discountID is required")
	}
	return archiveDiscountRowPostgres(ctx, discountID)
}

func insertDiscountPostgres(ctx *config.AppContext, codeName, discountExpr, affiliateEmail string, confRefs []string) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	discount, err := discountForWrite(codeName, discountExpr, affiliateEmail)
	if err != nil {
		return "", err
	}

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return "", fmt.Errorf("begin discount insert: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	discType := string(discount.DiscType)
	var discountID string
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO discounts (
			code_name, discount_expr, affiliate_email, disc_type, amount,
			max_uses, extra_qty, valid_from, valid_until
		) VALUES (
			$1, $2, NULLIF($3, '')::citext, $4, $5, $6, $7, $8, $9
		)
		RETURNING id::text
	`, discount.CodeName, discount.Discount, discount.AffiliateEmail, discType,
		nullableUintPtr(discount.Amount), nullableUintPtr(discount.MaxUses),
		int(discount.ExtraQty), discount.ValidFrom, discount.ValidUntil).Scan(&discountID)
	if err != nil {
		return "", fmt.Errorf("insert discount %q: %w", discount.CodeName, err)
	}
	if err := replaceDiscountConferenceLinksPostgres(tx, discountID, confRefs); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return "", fmt.Errorf("commit discount insert: %w", err)
	}

	return discountID, nil
}

func updateDiscountRowPostgres(ctx *config.AppContext, discountID, codeName, discountExpr string, affiliateEmail *string, confRefs []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	email := ""
	if affiliateEmail != nil {
		email = *affiliateEmail
	}
	discount, err := discountForWrite(codeName, discountExpr, email)
	if err != nil {
		return err
	}

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin discount update: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	discType := string(discount.DiscType)
	var rowsAffected int64
	if affiliateEmail == nil {
		commandTag, execErr := tx.Exec(ctx.DatabaseContext(), `
			UPDATE discounts
			SET code_name = $2,
				discount_expr = $3,
				disc_type = $4,
				amount = $5,
				max_uses = $6,
				extra_qty = $7,
				valid_from = $8,
				valid_until = $9
			WHERE id = $1
		`, discountID, discount.CodeName, discount.Discount, discType,
			nullableUintPtr(discount.Amount), nullableUintPtr(discount.MaxUses),
			int(discount.ExtraQty), discount.ValidFrom, discount.ValidUntil)
		err = execErr
		rowsAffected = commandTag.RowsAffected()
	} else {
		commandTag, execErr := tx.Exec(ctx.DatabaseContext(), `
			UPDATE discounts
			SET code_name = $2,
				discount_expr = $3,
				affiliate_email = NULLIF($4, '')::citext,
				disc_type = $5,
				amount = $6,
				max_uses = $7,
				extra_qty = $8,
				valid_from = $9,
				valid_until = $10
			WHERE id = $1
		`, discountID, discount.CodeName, discount.Discount, discount.AffiliateEmail,
			discType, nullableUintPtr(discount.Amount), nullableUintPtr(discount.MaxUses),
			int(discount.ExtraQty), discount.ValidFrom, discount.ValidUntil)
		err = execErr
		rowsAffected = commandTag.RowsAffected()
	}
	if err != nil {
		return fmt.Errorf("update discount %s: %w", discountID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("discount %s not found", discountID)
	}
	if err := replaceDiscountConferenceLinksPostgres(tx, discountID, confRefs); err != nil {
		return err
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit discount update: %w", err)
	}
	return nil
}

func archiveDiscountRowPostgres(ctx *config.AppContext, discountID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE discounts
		SET archived_at = now()
		WHERE id = $1
	`, discountID)
	if err != nil {
		return fmt.Errorf("archive discount %s: %w", discountID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("discount %s not found", discountID)
	}
	return nil
}

func replaceDiscountConferenceLinksPostgres(tx pgx.Tx, discountID string, confRefs []string) error {
	if _, err := tx.Exec(config.DatabaseContext(), `DELETE FROM discounts_conferences WHERE discount_id = $1`, discountID); err != nil {
		return fmt.Errorf("clear discount conference links %s: %w", discountID, err)
	}
	for _, confRef := range confRefs {
		confRef = strings.TrimSpace(confRef)
		if confRef == "" {
			continue
		}
		if _, err := tx.Exec(config.DatabaseContext(), `
			INSERT INTO discounts_conferences (discount_id, conference_id)
			VALUES ($1, $2)
			ON CONFLICT (discount_id, conference_id) DO NOTHING
		`, discountID, confRef); err != nil {
			return fmt.Errorf("insert discount conference link %s/%s: %w", discountID, confRef, err)
		}
	}
	return nil
}

func discountForWrite(codeName, discountExpr, affiliateEmail string) (*types.DiscountCode, error) {
	discount := &types.DiscountCode{
		CodeName:       strings.TrimSpace(codeName),
		Discount:       strings.TrimSpace(discountExpr),
		AffiliateEmail: strings.TrimSpace(affiliateEmail),
	}
	if discount.CodeName == "" {
		return nil, fmt.Errorf("discount code name is required")
	}
	if err := discount.ParseDiscountExpr(); err != nil {
		return nil, err
	}
	return discount, nil
}

func nullableUintPtr(value uint) *int {
	if value == 0 {
		return nil
	}
	out := int(value)
	return &out
}
