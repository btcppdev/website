package getters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

type MerchProductInput struct {
	Tag              string
	Slug             string
	Name             string
	Subtitle         string
	Description      string
	Status           string
	ProductType      string
	BasePriceCents   uint
	Currency         string
	Symbol           string
	PostSymbol       string
	StripeTaxCode    string
	EasyshipCategory string
	HSCode           string
	CountryOfOrigin  string
	RequiresShipping bool
	AllowEventPickup bool
}

type MerchVariantInput struct {
	ProductID       string
	SKU             string
	Label           string
	PriceDeltaCents int
	WeightGrams     int
	LengthMM        int
	WidthMM         int
	HeightMM        int
	InventoryPolicy string
	Status          string
}

type ShopOrderInput struct {
	BuyerEmail          string
	BuyerName           string
	Source              string
	CheckoutKind        string
	PaymentProvider     string
	Currency            string
	SubtotalCents       uint
	DiscountAmountCents uint
	ShippingAmountCents uint
	SalesTaxAmountCents uint
	TotalCents          uint
	ShippingAddress     *types.ShopAddress
}

type ShopOrderItemInput struct {
	ProductID            string
	VariantID            string
	Quantity             uint
	UnitPriceCents       uint
	DiscountAmountCents  uint
	TaxAmountCents       uint
	LineTotalCents       uint
	ProductTagSnapshot   string
	ProductNameSnapshot  string
	VariantLabelSnapshot string
	SKUSnapshot          string
	FulfillmentMethod    string
	SaleConferenceID     string
	PickupConferenceID   string
	Status               string
}

type ShippingRateQuoteInput struct {
	OrderID               string
	Provider              string
	ProviderQuoteID       string
	DestinationCountry    string
	DestinationRegion     string
	DestinationPostalCode string
	CourierName           string
	ServiceName           string
	AmountCents           uint
	Currency              string
	EstimatedMinDays      *int
	EstimatedMaxDays      *int
	RawResponse           string
	ExpiresAt             *time.Time
}

type TaxQuoteInput struct {
	OrderID               string
	Provider              string
	ProviderQuoteID       string
	SalesTaxAmountCents   uint
	DestinationCountry    string
	DestinationRegion     string
	DestinationPostalCode string
	RawResponse           string
	ExpiresAt             *time.Time
}

func CreateMerchProduct(ctx *config.AppContext, in MerchProductInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	in = normalizeMerchProductInput(in)
	if in.Tag == "" || in.Slug == "" || in.Name == "" {
		return "", fmt.Errorf("product tag, slug, and name are required")
	}

	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO merch_products (
			tag, slug, name, subtitle, description, status, product_type,
			base_price_cents, currency, symbol, post_symbol, stripe_tax_code,
			easyship_category, hs_code, country_of_origin, requires_shipping,
			allow_event_pickup
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17
		)
		RETURNING id::text
	`, in.Tag, in.Slug, in.Name, in.Subtitle, in.Description, in.Status, in.ProductType,
		int64(in.BasePriceCents), in.Currency, in.Symbol, in.PostSymbol, in.StripeTaxCode,
		in.EasyshipCategory, in.HSCode, in.CountryOfOrigin, in.RequiresShipping, in.AllowEventPickup).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create merch product %q: %w", in.Tag, err)
	}
	return id, nil
}

func UpdateMerchProduct(ctx *config.AppContext, id string, in MerchProductInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("product id is required")
	}
	in = normalizeMerchProductInput(in)
	tag, slug, name := in.Tag, in.Slug, in.Name
	if tag == "" || slug == "" || name == "" {
		return fmt.Errorf("product tag, slug, and name are required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE merch_products
		SET tag = $2, slug = $3, name = $4, subtitle = $5, description = $6,
			status = $7, product_type = $8, base_price_cents = $9, currency = $10,
			symbol = $11, post_symbol = $12, stripe_tax_code = $13,
			easyship_category = $14, hs_code = $15, country_of_origin = $16,
			requires_shipping = $17, allow_event_pickup = $18
		WHERE id = $1::uuid
	`, id, tag, slug, name, in.Subtitle, in.Description, in.Status, in.ProductType,
		int64(in.BasePriceCents), in.Currency, in.Symbol, in.PostSymbol, in.StripeTaxCode,
		in.EasyshipCategory, in.HSCode, in.CountryOfOrigin, in.RequiresShipping, in.AllowEventPickup)
	if err != nil {
		return fmt.Errorf("update merch product %s: %w", id, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("product %s not found", id)
	}
	return nil
}

func ListMerchProducts(ctx *config.AppContext, includeUnpublished bool) ([]*types.MerchProduct, error) {
	where := "WHERE status = 'published'"
	if includeUnpublished {
		where = ""
	}
	return queryMerchProducts(ctx, where, nil)
}

func ListConferenceMerchUpsells(ctx *config.AppContext, confID string) ([]*types.MerchProduct, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	confID = strings.TrimSpace(confID)
	if confID == "" {
		return nil, fmt.Errorf("conference id is required")
	}
	return queryMerchProducts(ctx, `
		JOIN conference_merch_upsells cmu ON cmu.product_id = merch_products.id
		WHERE cmu.conference_id = $1::uuid
			AND merch_products.status = 'published'
		ORDER BY cmu.display_order, cmu.created_at, merch_products.name
	`, []any{confID})
}

func ReplaceConferenceMerchUpsells(ctx *config.AppContext, confID string, productIDs []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	confID = strings.TrimSpace(confID)
	if confID == "" {
		return fmt.Errorf("conference id is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())

	if _, err := tx.Exec(ctx.DatabaseContext(), `DELETE FROM conference_merch_upsells WHERE conference_id = $1::uuid`, confID); err != nil {
		return fmt.Errorf("delete conference merch upsells: %w", err)
	}
	seen := map[string]bool{}
	order := 0
	for _, productID := range productIDs {
		productID = strings.TrimSpace(productID)
		if productID == "" || seen[productID] {
			continue
		}
		if order >= 3 {
			break
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO conference_merch_upsells (conference_id, product_id, display_order)
			VALUES ($1::uuid, $2::uuid, $3)
		`, confID, productID, order); err != nil {
			return fmt.Errorf("insert conference merch upsell: %w", err)
		}
		seen[productID] = true
		order++
	}
	return tx.Commit(ctx.DatabaseContext())
}

func GetMerchProductBySlug(ctx *config.AppContext, slug string, includeUnpublished bool) (*types.MerchProduct, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("product slug is required")
	}
	where := "WHERE slug = $1 AND status = 'published'"
	if includeUnpublished {
		where = "WHERE slug = $1"
	}
	products, err := queryMerchProducts(ctx, where, []any{slug})
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return nil, fmt.Errorf("product %s not found", slug)
	}
	return products[0], nil
}

func GetMerchProductByID(ctx *config.AppContext, id string) (*types.MerchProduct, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("product id is required")
	}
	products, err := queryMerchProducts(ctx, "WHERE id = $1::uuid", []any{id})
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return nil, fmt.Errorf("product %s not found", id)
	}
	return products[0], nil
}

func AddMerchProductImage(ctx *config.AppContext, productID, objectKey, altText string, displayOrder int, primary bool) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	productID = strings.TrimSpace(productID)
	objectKey = strings.TrimSpace(objectKey)
	if productID == "" || objectKey == "" {
		return "", fmt.Errorf("product id and image object key are required")
	}

	var count int
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT count(*) FROM merch_product_images WHERE product_id = $1::uuid
	`, productID).Scan(&count); err != nil {
		return "", fmt.Errorf("count merch images: %w", err)
	}
	if count >= 6 {
		return "", fmt.Errorf("a product can have at most 6 images")
	}

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if primary {
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE merch_product_images SET is_primary = false WHERE product_id = $1::uuid
		`, productID); err != nil {
			return "", fmt.Errorf("clear merch primary image: %w", err)
		}
	}

	var id string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO merch_product_images (product_id, object_key, alt_text, display_order, is_primary)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING id::text
	`, productID, objectKey, strings.TrimSpace(altText), displayOrder, primary).Scan(&id); err != nil {
		return "", fmt.Errorf("add merch image: %w", err)
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return "", err
	}
	return id, nil
}

func DeleteMerchProductImage(ctx *config.AppContext, imageID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return fmt.Errorf("image id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM merch_product_images WHERE id = $1::uuid
	`, imageID)
	if err != nil {
		return fmt.Errorf("delete merch image %s: %w", imageID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("image %s not found", imageID)
	}
	return nil
}

func UpdateMerchProductImage(ctx *config.AppContext, productID, imageID, altText string, displayOrder int, primary bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if primary {
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE merch_product_images SET is_primary = false WHERE product_id = $1::uuid
		`, strings.TrimSpace(productID)); err != nil {
			return fmt.Errorf("clear primary product image: %w", err)
		}
	}
	commandTag, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE merch_product_images
		SET alt_text = $3, display_order = $4, is_primary = $5
		WHERE product_id = $1::uuid AND id = $2::uuid
	`, strings.TrimSpace(productID), strings.TrimSpace(imageID), strings.TrimSpace(altText), displayOrder, primary)
	if err != nil {
		return fmt.Errorf("update product image: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("product image not found")
	}
	return tx.Commit(ctx.DatabaseContext())
}

func SaveMerchProductOption(ctx *config.AppContext, productID, optionID, name string, displayOrder int, required bool, values []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("option name is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if strings.TrimSpace(optionID) == "" {
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO merch_product_options (product_id, name, display_order, required)
			VALUES ($1::uuid, $2, $3, $4) RETURNING id::text
		`, strings.TrimSpace(productID), name, displayOrder, required).Scan(&optionID); err != nil {
			return fmt.Errorf("create product option: %w", err)
		}
	} else {
		commandTag, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE merch_product_options SET name = $3, display_order = $4, required = $5
			WHERE product_id = $1::uuid AND id = $2::uuid
		`, strings.TrimSpace(productID), strings.TrimSpace(optionID), name, displayOrder, required)
		if err != nil {
			return fmt.Errorf("update product option: %w", err)
		}
		if commandTag.RowsAffected() == 0 {
			return fmt.Errorf("product option not found")
		}
	}
	seen := map[string]bool{}
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO merch_product_option_values (option_id, value, display_order)
			VALUES ($1::uuid, $2, $3)
			ON CONFLICT (option_id, value) DO UPDATE SET display_order = excluded.display_order
		`, optionID, value, i); err != nil {
			return fmt.Errorf("save product option value: %w", err)
		}
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		DELETE FROM merch_product_option_values
		WHERE option_id = $1::uuid AND NOT (value = ANY($2::text[]))
	`, optionID, mapKeys(seen)); err != nil {
		return fmt.Errorf("remove product option values: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

func DeleteMerchProductOption(ctx *config.AppContext, productID, optionID string) error {
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM merch_product_options WHERE product_id = $1::uuid AND id = $2::uuid
	`, strings.TrimSpace(productID), strings.TrimSpace(optionID))
	if err != nil {
		return fmt.Errorf("delete product option: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("product option not found")
	}
	return nil
}

func mapKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}

func CreateMerchVariant(ctx *config.AppContext, in MerchVariantInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	in = normalizeMerchVariantInput(in)
	if in.ProductID == "" || in.SKU == "" || in.Label == "" {
		return "", fmt.Errorf("variant product, sku, and label are required")
	}
	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO merch_variants (
			product_id, sku, label, price_delta_cents, weight_grams, length_mm,
			width_mm, height_mm, inventory_policy, status
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id::text
	`, in.ProductID, in.SKU, in.Label, in.PriceDeltaCents, in.WeightGrams, in.LengthMM,
		in.WidthMM, in.HeightMM, in.InventoryPolicy, in.Status).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create merch variant %q: %w", in.SKU, err)
	}
	return id, nil
}

func GetMerchVariant(ctx *config.AppContext, variantID string) (*types.MerchVariant, *types.MerchProduct, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, nil, fmt.Errorf("database is not configured")
	}
	variantID = strings.TrimSpace(variantID)
	if variantID == "" {
		return nil, nil, fmt.Errorf("variant id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT product_id::text
		FROM merch_variants
		WHERE id = $1::uuid
	`, variantID)
	if err != nil {
		return nil, nil, fmt.Errorf("query merch variant product: %w", err)
	}
	var productID string
	if rows.Next() {
		if err := rows.Scan(&productID); err != nil {
			rows.Close()
			return nil, nil, err
		}
	}
	rows.Close()
	if productID == "" {
		return nil, nil, fmt.Errorf("variant %s not found", variantID)
	}
	product, err := GetMerchProductByID(ctx, productID)
	if err != nil {
		return nil, nil, err
	}
	for _, variant := range product.Variants {
		if variant.ID == variantID {
			return variant, product, nil
		}
	}
	return nil, nil, fmt.Errorf("variant %s not found", variantID)
}

func UpdateMerchVariant(ctx *config.AppContext, id string, in MerchVariantInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("variant id is required")
	}
	in = normalizeMerchVariantInput(in)
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE merch_variants
		SET sku = $2, label = $3, price_delta_cents = $4, weight_grams = $5,
			length_mm = $6, width_mm = $7, height_mm = $8,
			inventory_policy = $9, status = $10
		WHERE id = $1::uuid
	`, id, in.SKU, in.Label, in.PriceDeltaCents, in.WeightGrams, in.LengthMM,
		in.WidthMM, in.HeightMM, in.InventoryPolicy, in.Status)
	if err != nil {
		return fmt.Errorf("update merch variant %s: %w", id, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("variant %s not found", id)
	}
	return nil
}

func AdjustMerchInventory(ctx *config.AppContext, variantID, eventType string, delta int, actorEmail, notes string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	variantID = strings.TrimSpace(variantID)
	eventType = strings.TrimSpace(eventType)
	if variantID == "" || eventType == "" || delta == 0 {
		return fmt.Errorf("variant id, event type, and non-zero quantity delta are required")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO merch_inventory_events (
			variant_id, event_type, quantity_delta, actor_email, notes
		) VALUES ($1::uuid, $2, $3, NULLIF($4, '')::citext, $5)
	`, variantID, eventType, delta, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(notes))
	if err != nil {
		return fmt.Errorf("adjust merch inventory: %w", err)
	}
	return nil
}

func CreateShopOrder(ctx *config.AppContext, in ShopOrderInput, items []ShopOrderItemInput) (*types.ShopOrder, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("order needs at least one item")
	}
	in.Source = firstNonEmpty(strings.TrimSpace(in.Source), types.ShopOrderSourceOnline)
	in.CheckoutKind = firstNonEmpty(strings.TrimSpace(in.CheckoutKind), types.ShopCheckoutKindMerch)
	in.Currency = strings.ToUpper(firstNonEmpty(in.Currency, "USD"))
	checkoutExpiresAt := time.Now().UTC().Add(types.ShopCheckoutSessionTTL)
	reservationExpiresAt := time.Now().UTC().Add(types.ShopInventoryReservationTTL)

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx.DatabaseContext())

	var order types.ShopOrder
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO shop_orders (
			public_id, buyer_email, buyer_name, status, source, checkout_kind, payment_provider,
			currency, subtotal_cents, discount_amount_cents, shipping_amount_cents, sales_tax_amount_cents,
			total_cents, checkout_expires_at
		) VALUES (
			gen_random_uuid()::text, NULLIF($1, '')::citext, $2, 'pending', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		RETURNING id::text, public_id::text, coalesce(buyer_email::text, ''), buyer_name,
			status, source, checkout_kind, payment_provider, coalesce(payment_provider_id, ''),
			admin_notes, currency, subtotal_cents, discount_amount_cents, shipping_amount_cents, sales_tax_amount_cents,
			import_duty_amount_cents, import_tax_amount_cents, total_cents, checkout_expires_at,
			created_at, updated_at
	`, strings.ToLower(strings.TrimSpace(in.BuyerEmail)), strings.TrimSpace(in.BuyerName),
		in.Source, in.CheckoutKind, strings.TrimSpace(in.PaymentProvider), in.Currency,
		int64(in.SubtotalCents), int64(in.DiscountAmountCents), int64(in.ShippingAmountCents),
		int64(in.SalesTaxAmountCents), int64(in.TotalCents), checkoutExpiresAt).Scan(
		&order.ID, &order.PublicID, &order.BuyerEmail, &order.BuyerName,
		&order.Status, &order.Source, &order.CheckoutKind, &order.PaymentProvider,
		&order.PaymentProviderID, &order.AdminNotes, &order.Currency, &order.SubtotalCents, &order.DiscountAmountCents,
		&order.ShippingAmountCents, &order.SalesTaxAmountCents, &order.ImportDutyAmountCents,
		&order.ImportTaxAmountCents, &order.TotalCents, &order.CheckoutExpiresAt, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create shop order: %w", err)
	}
	if in.ShippingAddress != nil {
		address := in.ShippingAddress
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO shop_order_addresses (
				order_id, address_type, name, line1, line2, city, region, postal_code, country, phone
			) VALUES ($1::uuid, 'shipping', $2, $3, $4, $5, $6, $7, upper($8), $9)
		`, order.ID, strings.TrimSpace(address.Name), strings.TrimSpace(address.Line1),
			strings.TrimSpace(address.Line2), strings.TrimSpace(address.City), strings.TrimSpace(address.Region),
			strings.TrimSpace(address.PostalCode), strings.TrimSpace(address.Country), strings.TrimSpace(address.Phone)); err != nil {
			return nil, fmt.Errorf("create shop shipping address: %w", err)
		}
	}

	for _, item := range items {
		item.Status = firstNonEmpty(item.Status, types.ShopItemStatusPending)
		if strings.TrimSpace(item.VariantID) != "" {
			var inventoryPolicy, variantStatus string
			var stock int
			if err := tx.QueryRow(ctx.DatabaseContext(), `
				SELECT inventory_policy, status, coalesce((
					SELECT sum(quantity_delta)::int
					FROM merch_inventory_events mie
					WHERE mie.variant_id = merch_variants.id
				), 0) AS stock
				FROM merch_variants
				WHERE id = $1::uuid
				FOR UPDATE
			`, item.VariantID).Scan(&inventoryPolicy, &variantStatus, &stock); err != nil {
				return nil, fmt.Errorf("check merch inventory: %w", err)
			}
			if strings.TrimSpace(variantStatus) != "active" {
				return nil, fmt.Errorf("item is not available")
			}
			if inventoryPolicy != types.MerchInventoryPolicyUnlimited && inventoryPolicy != types.MerchInventoryPolicyAllowBackorder && stock < int(item.Quantity) {
				return nil, fmt.Errorf("not enough inventory for %s: %d left", item.SKUSnapshot, stock)
			}
		}
		var row types.ShopOrderItem
		err := tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO shop_order_items (
				order_id, product_id, variant_id, quantity, unit_price_cents, discount_amount_cents,
				tax_amount_cents, line_total_cents, product_tag_snapshot, product_name_snapshot,
				variant_label_snapshot, sku_snapshot, fulfillment_method,
				sale_conference_id, pickup_conference_id, status
			) VALUES (
				$1::uuid, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9, $10,
				$11, $12, $13, NULLIF($14, '')::uuid, NULLIF($15, '')::uuid, $16
			)
			RETURNING id::text, order_id::text, coalesce(product_id::text, ''), coalesce(variant_id::text, ''),
				quantity, fulfilled_quantity, refunded_quantity, unit_price_cents,
				discount_amount_cents, tax_amount_cents, line_total_cents, product_tag_snapshot,
				product_name_snapshot, variant_label_snapshot, sku_snapshot,
				fulfillment_method, coalesce(sale_conference_id::text, ''),
				coalesce(pickup_conference_id::text, ''), status, created_at, updated_at
		`, order.ID, item.ProductID, item.VariantID, int64(item.Quantity), int64(item.UnitPriceCents),
			int64(item.DiscountAmountCents), int64(item.TaxAmountCents), int64(item.LineTotalCents),
			item.ProductTagSnapshot, item.ProductNameSnapshot, item.VariantLabelSnapshot,
			item.SKUSnapshot, item.FulfillmentMethod, item.SaleConferenceID,
			item.PickupConferenceID, item.Status).Scan(
			&row.ID, &row.OrderID, &row.ProductID, &row.VariantID, &row.Quantity,
			&row.FulfilledQuantity, &row.RefundedQuantity, &row.UnitPriceCents,
			&row.DiscountAmountCents, &row.TaxAmountCents, &row.LineTotalCents, &row.ProductTagSnapshot,
			&row.ProductNameSnapshot, &row.VariantLabelSnapshot, &row.SKUSnapshot,
			&row.FulfillmentMethod, &row.SaleConferenceID, &row.PickupConferenceID,
			&row.Status, &row.CreatedAt, &row.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("create shop order item: %w", err)
		}
		order.Items = append(order.Items, &row)
		if item.FulfillmentMethod == types.ShopFulfillmentEventPickup && item.PickupConferenceID != "" {
			if _, err := tx.Exec(ctx.DatabaseContext(), `
				INSERT INTO shop_item_pickups (order_item_id, conference_id, quantity)
				VALUES ($1::uuid, $2::uuid, $3)
			`, row.ID, item.PickupConferenceID, int64(item.Quantity)); err != nil {
				return nil, fmt.Errorf("create shop pickup: %w", err)
			}
		}
		if strings.TrimSpace(item.VariantID) != "" {
			if _, err := tx.Exec(ctx.DatabaseContext(), `
				INSERT INTO merch_inventory_reservations (
					variant_id, checkout_session_id, order_item_id, quantity, status, expires_at
				) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'active', $5)
			`, item.VariantID, order.ID, row.ID, int64(item.Quantity), reservationExpiresAt); err != nil {
				return nil, fmt.Errorf("create inventory reservation: %w", err)
			}
			if _, err := tx.Exec(ctx.DatabaseContext(), `
				INSERT INTO merch_inventory_events (
					variant_id, event_type, quantity_delta, order_item_id, conference_id,
					actor_email, notes
				) VALUES ($1::uuid, 'reservation', $2, $3::uuid, NULLIF($4, '')::uuid, NULLIF($5, '')::citext, 'shop checkout')
			`, item.VariantID, -int(item.Quantity), row.ID, item.SaleConferenceID, order.BuyerEmail); err != nil {
				return nil, fmt.Errorf("reserve inventory: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (event_type, actor_type, actor_email, entity_type, entity_id, order_id)
		VALUES ('order.created', 'buyer', NULLIF($1, '')::citext, 'shop_order', $2::uuid, $2::uuid)
	`, order.BuyerEmail, order.ID); err != nil {
		return nil, fmt.Errorf("record shop event: %w", err)
	}

	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, err
	}
	return &order, nil
}

func GetShopOrderByPublicID(ctx *config.AppContext, publicID string) (*types.ShopOrder, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return nil, fmt.Errorf("order public id is required")
	}

	var order types.ShopOrder
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, public_id::text, coalesce(buyer_email::text, ''), buyer_name,
			status, source, checkout_kind, payment_provider, coalesce(payment_provider_id, ''),
			admin_notes, currency, subtotal_cents, discount_amount_cents, shipping_amount_cents, sales_tax_amount_cents,
			import_duty_amount_cents, import_tax_amount_cents, total_cents, paid_at, cancelled_at, checkout_expires_at,
			created_at, updated_at
		FROM shop_orders
		WHERE public_id = $1
	`, publicID).Scan(
		&order.ID, &order.PublicID, &order.BuyerEmail, &order.BuyerName,
		&order.Status, &order.Source, &order.CheckoutKind, &order.PaymentProvider,
		&order.PaymentProviderID, &order.AdminNotes, &order.Currency, &order.SubtotalCents, &order.DiscountAmountCents,
		&order.ShippingAmountCents, &order.SalesTaxAmountCents, &order.ImportDutyAmountCents,
		&order.ImportTaxAmountCents, &order.TotalCents, &order.PaidAt, &order.CancelledAt, &order.CheckoutExpiresAt,
		&order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get shop order %s: %w", publicID, err)
	}
	items, err := listShopOrderItems(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.Items = items
	populateShopOrderFulfillmentSummary(&order)
	shipments, err := listShopOrderShipments(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.Shipments = shipments
	order.ShippingAddress, err = getShopOrderAddress(ctx, order.ID, "shipping")
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func GetLatestPendingShopOrderByEmail(ctx *config.AppContext, email string) (*types.ShopOrder, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, nil
	}
	var publicID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT public_id
		FROM shop_orders
		WHERE buyer_email = $1::citext
			AND status = 'pending'
			AND source = 'online'
			AND checkout_kind = 'merch'
			AND coalesce(checkout_expires_at, created_at + interval '30 minutes') > now()
		ORDER BY created_at DESC
		LIMIT 1
	`, email).Scan(&publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find pending shop order: %w", err)
	}
	return GetShopOrderByPublicID(ctx, publicID)
}

func GetShopOrderByID(ctx *config.AppContext, id string) (*types.ShopOrder, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("order id is required")
	}
	var order types.ShopOrder
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, public_id::text, coalesce(buyer_email::text, ''), buyer_name,
			status, source, checkout_kind, payment_provider, coalesce(payment_provider_id, ''),
			admin_notes, currency, subtotal_cents, discount_amount_cents, shipping_amount_cents, sales_tax_amount_cents,
			import_duty_amount_cents, import_tax_amount_cents, total_cents, paid_at, cancelled_at, checkout_expires_at,
			created_at, updated_at
		FROM shop_orders
		WHERE id = $1::uuid
	`, id).Scan(
		&order.ID, &order.PublicID, &order.BuyerEmail, &order.BuyerName,
		&order.Status, &order.Source, &order.CheckoutKind, &order.PaymentProvider,
		&order.PaymentProviderID, &order.AdminNotes, &order.Currency, &order.SubtotalCents, &order.DiscountAmountCents,
		&order.ShippingAmountCents, &order.SalesTaxAmountCents, &order.ImportDutyAmountCents,
		&order.ImportTaxAmountCents, &order.TotalCents, &order.PaidAt, &order.CancelledAt, &order.CheckoutExpiresAt,
		&order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get shop order by id %s: %w", id, err)
	}
	items, err := listShopOrderItems(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.Items = items
	populateShopOrderFulfillmentSummary(&order)
	shipments, err := listShopOrderShipments(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.Shipments = shipments
	order.ShippingAddress, err = getShopOrderAddress(ctx, order.ID, "shipping")
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func GetShopRefundContactByEmail(ctx *config.AppContext, email string) (*types.ShopRefundContact, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return &types.ShopRefundContact{}, nil
	}
	contact := &types.ShopRefundContact{}
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT
			coalesce(max(nullif(signal, '')), ''),
			coalesce(max(nullif(telegram, '')), '')
		FROM people
		WHERE email = $1::citext
	`, email).Scan(&contact.Signal, &contact.Telegram)
	if err != nil {
		return nil, fmt.Errorf("get shop refund contact: %w", err)
	}
	return contact, nil
}

func getShopOrderAddress(ctx *config.AppContext, orderID, addressType string) (*types.ShopAddress, error) {
	var address types.ShopAddress
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT name, line1, line2, city, region, postal_code, country, phone
		FROM shop_order_addresses
		WHERE order_id = $1::uuid AND address_type = $2
	`, orderID, addressType).Scan(&address.Name, &address.Line1, &address.Line2, &address.City,
		&address.Region, &address.PostalCode, &address.Country, &address.Phone)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get shop order %s address: %w", addressType, err)
	}
	return &address, nil
}

func UpsertShopOrderShippingAddress(ctx *config.AppContext, orderID string, address *types.ShopAddress) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(orderID) == "" || address == nil {
		return fmt.Errorf("order id and shipping address are required")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_order_addresses (
			order_id, address_type, name, line1, line2, city, region, postal_code, country, phone
		) VALUES ($1::uuid, 'shipping', $2, $3, $4, $5, $6, $7, upper($8), $9)
		ON CONFLICT (order_id, address_type) DO UPDATE SET
			name = EXCLUDED.name,
			line1 = EXCLUDED.line1,
			line2 = EXCLUDED.line2,
			city = EXCLUDED.city,
			region = EXCLUDED.region,
			postal_code = EXCLUDED.postal_code,
			country = EXCLUDED.country,
			phone = coalesce(NULLIF(EXCLUDED.phone, ''), shop_order_addresses.phone)
	`, strings.TrimSpace(orderID), strings.TrimSpace(address.Name), strings.TrimSpace(address.Line1),
		strings.TrimSpace(address.Line2), strings.TrimSpace(address.City), strings.TrimSpace(address.Region),
		strings.TrimSpace(address.PostalCode), strings.TrimSpace(address.Country), strings.TrimSpace(address.Phone))
	if err != nil {
		return fmt.Errorf("upsert shop shipping address: %w", err)
	}
	return nil
}

func ListShopOrders(ctx *config.AppContext, limit uint) ([]*types.ShopOrder, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if limit == 0 || limit > 200 {
		limit = 100
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT so.id::text, so.public_id::text, coalesce(so.buyer_email::text, ''), so.buyer_name,
			so.status, so.source, so.checkout_kind, so.payment_provider, coalesce(so.payment_provider_id, ''),
			so.admin_notes, so.currency, so.subtotal_cents, so.discount_amount_cents, so.shipping_amount_cents, so.sales_tax_amount_cents,
			so.import_duty_amount_cents, so.import_tax_amount_cents, so.total_cents, so.paid_at, so.cancelled_at, so.checkout_expires_at,
			so.created_at, so.updated_at,
			CASE WHEN so.status IN ('paid', 'partially_refunded') THEN coalesce(f.unfulfilled_quantity, 0) ELSE 0 END,
			CASE WHEN so.status IN ('paid', 'partially_refunded') THEN coalesce(f.item_summary, '') ELSE '' END,
			coalesce(f.event_pickup_quantity, 0), coalesce(f.event_pickup_summary, '')
		FROM shop_orders so
		LEFT JOIN (
			SELECT
				soi.order_id,
				coalesce(sum(greatest(soi.quantity::bigint - soi.fulfilled_quantity::bigint - soi.refunded_quantity::bigint, 0))
					FILTER (WHERE soi.fulfillment_method = 'ship' AND soi.status NOT IN ('cancelled', 'refunded', 'fulfilled')), 0) AS unfulfilled_quantity,
				string_agg(
					concat(soi.product_name_snapshot, ' ×', greatest(soi.quantity::bigint - soi.fulfilled_quantity::bigint - soi.refunded_quantity::bigint, 0)),
					', ' ORDER BY soi.created_at
				) FILTER (WHERE soi.fulfillment_method = 'ship' AND soi.status NOT IN ('cancelled', 'refunded', 'fulfilled')
					AND soi.quantity > soi.fulfilled_quantity + soi.refunded_quantity) AS item_summary,
				coalesce(sum(greatest(soi.quantity::bigint - soi.fulfilled_quantity::bigint - soi.refunded_quantity::bigint, 0))
					FILTER (WHERE soi.fulfillment_method = 'event_pickup' AND soi.status NOT IN ('cancelled', 'refunded', 'fulfilled')), 0) AS event_pickup_quantity,
				string_agg(
					concat(soi.product_name_snapshot, ' ×', greatest(soi.quantity::bigint - soi.fulfilled_quantity::bigint - soi.refunded_quantity::bigint, 0)),
					', ' ORDER BY soi.created_at
				) FILTER (WHERE soi.fulfillment_method = 'event_pickup' AND soi.status NOT IN ('cancelled', 'refunded', 'fulfilled')
					AND soi.quantity > soi.fulfilled_quantity + soi.refunded_quantity) AS event_pickup_summary
			FROM shop_order_items soi
			GROUP BY soi.order_id
		) f ON f.order_id = so.id
		ORDER BY
			(CASE WHEN so.status IN ('paid', 'partially_refunded') THEN coalesce(f.unfulfilled_quantity, 0) ELSE 0 END > 0) DESC,
			(coalesce(f.event_pickup_quantity, 0) > 0) DESC,
			(so.status IN ('paid', 'partially_refunded')) DESC,
			so.created_at DESC
		LIMIT $1
	`, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list shop orders: %w", err)
	}
	defer rows.Close()
	var orders []*types.ShopOrder
	for rows.Next() {
		order, err := scanShopOrderSummary(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func scanShopOrderSummary(row shopOrderScanner) (*types.ShopOrder, error) {
	var order types.ShopOrder
	if err := row.Scan(
		&order.ID, &order.PublicID, &order.BuyerEmail, &order.BuyerName,
		&order.Status, &order.Source, &order.CheckoutKind, &order.PaymentProvider,
		&order.PaymentProviderID, &order.AdminNotes, &order.Currency, &order.SubtotalCents, &order.DiscountAmountCents,
		&order.ShippingAmountCents, &order.SalesTaxAmountCents, &order.ImportDutyAmountCents,
		&order.ImportTaxAmountCents, &order.TotalCents, &order.PaidAt, &order.CancelledAt, &order.CheckoutExpiresAt,
		&order.CreatedAt, &order.UpdatedAt, &order.UnfulfilledShippingQuantity, &order.UnfulfilledShippingSummary,
		&order.EventPickupQuantity, &order.EventPickupSummary,
	); err != nil {
		return nil, fmt.Errorf("scan shop order summary: %w", err)
	}
	return &order, nil
}

func populateShopOrderFulfillmentSummary(order *types.ShopOrder) {
	if order == nil {
		return
	}
	order.UnfulfilledShippingQuantity = 0
	order.UnfulfilledShippingSummary = ""
	order.EventPickupQuantity = 0
	order.EventPickupSummary = ""
	var shippingLines, pickupLines []string
	shippingReady := order.Status == types.ShopOrderStatusPaid || order.Status == types.ShopOrderStatusPartiallyRefunded
	for _, item := range order.Items {
		if item == nil || item.Status == types.ShopItemStatusCancelled || item.Status == types.ShopItemStatusRefunded || item.Status == types.ShopItemStatusFulfilled {
			continue
		}
		used := item.FulfilledQuantity + item.RefundedQuantity
		if used >= item.Quantity {
			continue
		}
		remaining := item.Quantity - used
		line := fmt.Sprintf("%s ×%d", item.ProductNameSnapshot, remaining)
		switch item.FulfillmentMethod {
		case types.ShopFulfillmentShip:
			if shippingReady {
				order.UnfulfilledShippingQuantity += remaining
				shippingLines = append(shippingLines, line)
			}
		case types.ShopFulfillmentEventPickup:
			order.EventPickupQuantity += remaining
			pickupLines = append(pickupLines, line)
		}
	}
	order.UnfulfilledShippingSummary = strings.Join(shippingLines, ", ")
	order.EventPickupSummary = strings.Join(pickupLines, ", ")
}

func GetShopOperationalStats(ctx *config.AppContext) (*types.ShopOperationalStats, error) {
	stats := &types.ShopOperationalStats{}
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'pending' AND checkout_expires_at <= now()),
			count(*) FILTER (WHERE status = 'paid' AND payment_provider_id = '')
		FROM shop_orders
	`).Scan(&stats.PendingOrders, &stats.ExpiredPendingOrders, &stats.PaidMissingProviderID)
	if err != nil {
		return nil, fmt.Errorf("load shop order operational stats: %w", err)
	}
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT count(*) FROM merch_inventory_reservations
		WHERE status = 'active' AND expires_at <= now()
	`).Scan(&stats.ExpiredReservations); err != nil {
		return nil, fmt.Errorf("load reservation operational stats: %w", err)
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT p.name, v.id::text, v.sku, v.label,
			coalesce(sum(e.quantity_delta), 0)::int AS stock
		FROM merch_variants v
		JOIN merch_products p ON p.id = v.product_id
		LEFT JOIN merch_inventory_events e ON e.variant_id = v.id
		WHERE v.status = 'active' AND v.inventory_policy = 'deny'
		GROUP BY p.name, v.id, v.sku, v.label
		HAVING coalesce(sum(e.quantity_delta), 0) <= 5
		ORDER BY stock, p.name, v.label
		LIMIT 50
	`)
	if err != nil {
		return nil, fmt.Errorf("load low stock variants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item := &types.ShopLowStockItem{}
		if err := rows.Scan(&item.ProductName, &item.VariantID, &item.SKU, &item.Label, &item.Stock); err != nil {
			return nil, err
		}
		stats.LowStock = append(stats.LowStock, item)
	}
	return stats, rows.Err()
}

func ListShopOrdersByEmail(ctx *config.AppContext, email string, limit uint) ([]*types.ShopOrder, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if limit == 0 || limit > 100 {
		limit = 20
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, public_id::text, coalesce(buyer_email::text, ''), buyer_name,
			status, source, checkout_kind, payment_provider, coalesce(payment_provider_id, ''),
			admin_notes, currency, subtotal_cents, discount_amount_cents, shipping_amount_cents, sales_tax_amount_cents,
			import_duty_amount_cents, import_tax_amount_cents, total_cents, paid_at, cancelled_at, checkout_expires_at,
			created_at, updated_at
		FROM shop_orders
		WHERE buyer_email = $1::citext
			AND checkout_kind <> 'ticket'
		ORDER BY created_at DESC
		LIMIT $2
	`, strings.ToLower(strings.TrimSpace(email)), int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list shop orders by email: %w", err)
	}
	defer rows.Close()
	var orders []*types.ShopOrder
	for rows.Next() {
		order, err := scanShopOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

type shopOrderScanner interface {
	Scan(dest ...any) error
}

func scanShopOrder(row shopOrderScanner) (*types.ShopOrder, error) {
	var order types.ShopOrder
	if err := row.Scan(
		&order.ID, &order.PublicID, &order.BuyerEmail, &order.BuyerName,
		&order.Status, &order.Source, &order.CheckoutKind, &order.PaymentProvider,
		&order.PaymentProviderID, &order.AdminNotes, &order.Currency, &order.SubtotalCents, &order.DiscountAmountCents,
		&order.ShippingAmountCents, &order.SalesTaxAmountCents, &order.ImportDutyAmountCents,
		&order.ImportTaxAmountCents, &order.TotalCents, &order.PaidAt, &order.CancelledAt, &order.CheckoutExpiresAt,
		&order.CreatedAt, &order.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan shop order: %w", err)
	}
	return &order, nil
}

func listShopOrderShipments(ctx *config.AppContext, orderID string) ([]*types.Shipment, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, order_id::text, provider, provider_shipment_id, provider_label_id,
			courier_service_id, courier_name, service_name, tracking_number, tracking_url, label_url, status,
			label_state, delivery_state, raw_response::text, shipped_at, delivered_at,
			last_webhook_at, last_synced_at, shipping_notified_at,
			create_idempotency_key::text, label_idempotency_key::text, last_error,
			created_at, updated_at
		FROM shipments
		WHERE order_id = $1::uuid
		ORDER BY created_at DESC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list shop order shipments: %w", err)
	}
	defer rows.Close()
	var out []*types.Shipment
	for rows.Next() {
		var shipment types.Shipment
		if err := rows.Scan(shipmentScanTargets(&shipment)...); err != nil {
			return nil, fmt.Errorf("scan shop shipment: %w", err)
		}
		out = append(out, &shipment)
	}
	return out, rows.Err()
}

func ListShopPickupsForConference(ctx *config.AppContext, confID string) ([]*types.ShopOrderItem, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT soi.id::text, soi.order_id::text, coalesce(soi.product_id::text, ''), coalesce(soi.variant_id::text, ''),
			soi.quantity, soi.fulfilled_quantity, soi.refunded_quantity, soi.unit_price_cents,
			soi.discount_amount_cents, soi.tax_amount_cents, soi.line_total_cents, soi.product_tag_snapshot,
			soi.product_name_snapshot, soi.variant_label_snapshot, soi.sku_snapshot,
			'' AS image_object_key,
			soi.fulfillment_method, coalesce(soi.sale_conference_id::text, ''),
			coalesce(sale_conf.tag, ''),
			coalesce(soi.pickup_conference_id::text, ''), soi.status, soi.created_at, soi.updated_at
		FROM shop_order_items soi
		JOIN shop_orders so ON so.id = soi.order_id
		LEFT JOIN conferences sale_conf ON sale_conf.id = soi.sale_conference_id
		WHERE soi.pickup_conference_id = $1::uuid
			AND so.status IN ('pending', 'paid')
			AND soi.status IN ('pending', 'ready')
		ORDER BY so.buyer_name, so.buyer_email, soi.created_at
	`, confID)
	if err != nil {
		return nil, fmt.Errorf("list shop pickups: %w", err)
	}
	defer rows.Close()
	return scanShopOrderItems(rows, "shop pickups")
}

func ListShopPickupsForTicket(ctx *config.AppContext, ticketRef string) ([]*types.ShopOrderItem, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	ticketRef = strings.TrimSpace(ticketRef)
	if ticketRef == "" {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT soi.id::text, soi.order_id::text, coalesce(soi.product_id::text, ''), coalesce(soi.variant_id::text, ''),
			soi.quantity, soi.fulfilled_quantity, soi.refunded_quantity, soi.unit_price_cents,
			soi.discount_amount_cents, soi.tax_amount_cents, soi.line_total_cents, soi.product_tag_snapshot,
			soi.product_name_snapshot, soi.variant_label_snapshot, soi.sku_snapshot,
			'' AS image_object_key,
			soi.fulfillment_method, coalesce(soi.sale_conference_id::text, ''),
			coalesce(sale_conf.tag, ''),
			coalesce(soi.pickup_conference_id::text, ''), soi.status, soi.created_at, soi.updated_at
		FROM registrations r
		JOIN shop_orders so ON lower(so.buyer_email::text) = lower(r.email::text)
		JOIN shop_order_items soi ON soi.order_id = so.id
		LEFT JOIN conferences sale_conf ON sale_conf.id = soi.sale_conference_id
		WHERE r.ref_id = $1
			AND soi.pickup_conference_id = r.conference_id
			AND soi.fulfillment_method = 'event_pickup'
			AND so.status = 'paid'
			AND soi.status IN ('ready', 'fulfilled')
		ORDER BY soi.status, soi.created_at
	`, ticketRef)
	if err != nil {
		return nil, fmt.Errorf("list shop pickups for ticket: %w", err)
	}
	defer rows.Close()
	return scanShopOrderItems(rows, "shop pickups for ticket")
}

func MarkShopOrderItemPickedUp(ctx *config.AppContext, orderItemID, actorEmail, notes string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	orderItemID = strings.TrimSpace(orderItemID)
	if orderItemID == "" {
		return fmt.Errorf("order item id is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())

	commandTag, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_order_items
		SET fulfilled_quantity = quantity,
			status = 'fulfilled'
		WHERE id = $1::uuid
			AND status <> 'fulfilled'
	`, orderItemID)
	if err != nil {
		return fmt.Errorf("mark shop item fulfilled: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("shop order item %s not found or already fulfilled", orderItemID)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_item_pickups
		SET picked_up_at = now(),
			picked_up_by = NULLIF($2, '')::citext,
			notes = $3
		WHERE order_item_id = $1::uuid
			AND picked_up_at IS NULL
	`, orderItemID, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(notes)); err != nil {
		return fmt.Errorf("mark shop pickup: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO merch_inventory_events (
			variant_id, event_type, quantity_delta, order_item_id, actor_email, notes
		)
		SELECT variant_id, 'pickup', 0, id, NULLIF($2, '')::citext, $3
		FROM shop_order_items
		WHERE id = $1::uuid
	`, orderItemID, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(notes)); err != nil {
		return fmt.Errorf("record pickup inventory event: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (
			event_type, actor_type, actor_email, entity_type, entity_id, order_item_id
		) VALUES (
			'pickup.completed', 'volunteer', NULLIF($2, '')::citext, 'shop_order_item', $1::uuid, $1::uuid
		)
	`, orderItemID, strings.ToLower(strings.TrimSpace(actorEmail))); err != nil {
		return fmt.Errorf("record pickup event: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

func UpdateShopOrderAdminNotes(ctx *config.AppContext, orderID, actorEmail, notes string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("order id is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	tag, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_orders
		SET admin_notes = $2
		WHERE id = $1::uuid
	`, orderID, strings.TrimSpace(notes))
	if err != nil {
		return fmt.Errorf("update shop order notes: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("shop order %s not found", orderID)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (
			event_type, actor_type, actor_email, entity_type, entity_id, order_id, metadata
		) VALUES (
			'order.notes_updated', 'admin', NULLIF($2, '')::citext, 'shop_order', $1::uuid, $1::uuid, jsonb_build_object('notes', $3::text)
		)
	`, orderID, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(notes)); err != nil {
		return fmt.Errorf("record shop notes event: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

type shopInventoryReservation struct {
	ID           string
	VariantID    string
	OrderItemID  string
	ConferenceID string
	Quantity     uint
	Status       string
}

func listShopOrderReservationsForUpdate(ctx *config.AppContext, tx pgx.Tx, orderID string, statuses ...string) ([]shopInventoryReservation, error) {
	rows, err := tx.Query(ctx.DatabaseContext(), `
		SELECT r.id::text, r.variant_id::text, coalesce(r.order_item_id::text, ''),
			coalesce(soi.sale_conference_id::text, ''), r.quantity, r.status
		FROM merch_inventory_reservations r
		LEFT JOIN shop_order_items soi ON soi.id = r.order_item_id
		WHERE r.checkout_session_id = $1::uuid
			AND (cardinality($2::text[]) = 0 OR r.status = ANY($2::text[]))
		ORDER BY r.created_at
		FOR UPDATE OF r
	`, orderID, statuses)
	if err != nil {
		return nil, fmt.Errorf("load inventory reservations: %w", err)
	}
	defer rows.Close()
	var reservations []shopInventoryReservation
	for rows.Next() {
		var reservation shopInventoryReservation
		if err := rows.Scan(&reservation.ID, &reservation.VariantID, &reservation.OrderItemID,
			&reservation.ConferenceID, &reservation.Quantity, &reservation.Status); err != nil {
			return nil, fmt.Errorf("scan inventory reservation: %w", err)
		}
		reservations = append(reservations, reservation)
	}
	return reservations, rows.Err()
}

func releaseShopOrderReservations(ctx *config.AppContext, tx pgx.Tx, orderID, status, actorEmail, notes string) error {
	reservations, err := listShopOrderReservationsForUpdate(ctx, tx, orderID, "active")
	if err != nil {
		return err
	}
	for _, reservation := range reservations {
		tag, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE merch_inventory_reservations
			SET status = $2
			WHERE id = $1::uuid AND status = 'active'
		`, reservation.ID, status)
		if err != nil {
			return fmt.Errorf("release inventory reservation: %w", err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO merch_inventory_events (
				variant_id, event_type, quantity_delta, order_item_id, conference_id, actor_email, notes
			) VALUES (
				$1::uuid, 'reservation_release', $2, NULLIF($3, '')::uuid,
				NULLIF($4, '')::uuid, NULLIF($5, '')::citext, $6
			)
		`, reservation.VariantID, int64(reservation.Quantity), reservation.OrderItemID,
			reservation.ConferenceID, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(notes)); err != nil {
			return fmt.Errorf("record inventory reservation release: %w", err)
		}
	}
	return nil
}

// ExpirePendingShopOrders releases stock held by abandoned payment sessions.
// It is safe to run concurrently across app instances because candidate order
// rows are locked with SKIP LOCKED and each reservation transition is guarded.
func ExpirePendingShopOrders(ctx *config.AppContext, limit uint) (uint, error) {
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("database is not configured")
	}
	if limit == 0 || limit > 500 {
		limit = 100
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	rows, err := tx.Query(ctx.DatabaseContext(), `
		SELECT id::text
		FROM shop_orders
		WHERE status = 'pending'
			AND checkout_expires_at IS NOT NULL
			AND checkout_expires_at <= now()
		ORDER BY checkout_expires_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, int64(limit))
	if err != nil {
		return 0, fmt.Errorf("load expired shop orders: %w", err)
	}
	var orderIDs []string
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan expired shop order: %w", err)
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	for _, orderID := range orderIDs {
		if err := releaseShopOrderReservations(ctx, tx, orderID, "expired", "", "checkout expired"); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE shop_order_items
			SET status = 'cancelled'
			WHERE order_id = $1::uuid AND status = 'pending'
		`, orderID); err != nil {
			return 0, fmt.Errorf("cancel expired shop items: %w", err)
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE shop_orders
			SET status = 'cancelled', cancelled_at = coalesce(cancelled_at, now())
			WHERE id = $1::uuid AND status = 'pending'
		`, orderID); err != nil {
			return 0, fmt.Errorf("cancel expired shop order: %w", err)
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO shop_events (event_type, actor_type, entity_type, entity_id, order_id)
			VALUES ('order.expired', 'system', 'shop_order', $1::uuid, $1::uuid)
		`, orderID); err != nil {
			return 0, fmt.Errorf("record expired shop order: %w", err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return 0, err
	}
	return uint(len(orderIDs)), nil
}

func CancelShopOrder(ctx *config.AppContext, orderID, actorEmail, notes string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("order id is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var currentStatus string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT status FROM shop_orders WHERE id = $1::uuid FOR UPDATE
	`, orderID).Scan(&currentStatus); err != nil {
		return fmt.Errorf("load shop order for cancellation: %w", err)
	}
	if currentStatus == types.ShopOrderStatusCancelled {
		return nil
	}
	if currentStatus != types.ShopOrderStatusPending {
		return fmt.Errorf("shop order %s is %s; paid orders must be refunded instead of cancelled", orderID, currentStatus)
	}
	if err := releaseShopOrderReservations(ctx, tx, orderID, "released", actorEmail, firstNonEmpty(strings.TrimSpace(notes), "checkout cancelled")); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_orders
		SET status = 'cancelled',
			cancelled_at = coalesce(cancelled_at, now()),
			admin_notes = CASE WHEN $2 = '' THEN admin_notes ELSE trim(admin_notes || E'\n' || $2) END
		WHERE id = $1::uuid
			AND status = 'pending'
	`, orderID, strings.TrimSpace(notes))
	if err != nil {
		return fmt.Errorf("cancel shop order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("shop order %s cannot be cancelled", orderID)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_order_items
		SET status = 'cancelled'
		WHERE order_id = $1::uuid
			AND status IN ('pending', 'ready')
	`, orderID); err != nil {
		return fmt.Errorf("cancel shop order items: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (
			event_type, actor_type, actor_email, entity_type, entity_id, order_id, metadata
		) VALUES (
			'order.cancelled', 'admin', NULLIF($2, '')::citext, 'shop_order', $1::uuid, $1::uuid, jsonb_build_object('notes', $3::text)
		)
	`, orderID, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(notes)); err != nil {
		return fmt.Errorf("record shop cancel event: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

func MarkShopOrderShipped(ctx *config.AppContext, orderID, actorEmail, courierName, trackingNumber, trackingURL, notes string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("order id is required")
	}
	trackingNumber = strings.TrimSpace(trackingNumber)
	trackingURL = strings.TrimSpace(trackingURL)
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shipments (
			order_id, provider, courier_name, tracking_number, tracking_url, status, shipped_at, raw_response
		) VALUES (
			$1::uuid, 'manual', $2, $3, $4, 'shipped', now(), '{}'::jsonb
		)
	`, orderID, strings.TrimSpace(courierName), trackingNumber, trackingURL); err != nil {
		return fmt.Errorf("record shipment: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_order_items
		SET status = 'fulfilled',
			fulfilled_quantity = quantity
		WHERE order_id = $1::uuid
			AND fulfillment_method = 'ship'
			AND status IN ('pending', 'ready')
	`, orderID); err != nil {
		return fmt.Errorf("mark shipped items fulfilled: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_orders
		SET admin_notes = CASE WHEN $2 = '' THEN admin_notes ELSE trim(admin_notes || E'\n' || $2) END
		WHERE id = $1::uuid
	`, orderID, strings.TrimSpace(notes)); err != nil {
		return fmt.Errorf("update shipped order notes: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (
			event_type, actor_type, actor_email, entity_type, entity_id, order_id, metadata
		) VALUES (
			'order.shipped', 'admin', NULLIF($2, '')::citext, 'shop_order', $1::uuid, $1::uuid,
			jsonb_build_object('courier', $3::text, 'tracking_number', $4::text, 'tracking_url', $5::text, 'notes', $6::text)
		)
	`, orderID, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(courierName), trackingNumber, trackingURL, strings.TrimSpace(notes)); err != nil {
		return fmt.Errorf("record shop shipped event: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

func RecordShopRefund(ctx *config.AppContext, orderID, orderItemID, actorEmail, provider, providerRefundID, reason string, quantity uint, amountCents uint, restock bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	orderID = strings.TrimSpace(orderID)
	orderItemID = strings.TrimSpace(orderItemID)
	if orderID == "" || orderItemID == "" {
		return fmt.Errorf("order id and order item id are required")
	}
	if quantity == 0 || amountCents == 0 {
		return fmt.Errorf("refund quantity and amount are required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	provider = strings.TrimSpace(provider)
	providerRefundID = strings.TrimSpace(providerRefundID)
	if providerRefundID != "" {
		var exists bool
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			SELECT EXISTS (
				SELECT 1 FROM refunds WHERE provider = $1 AND provider_refund_id = $2
			)
		`, provider, providerRefundID).Scan(&exists); err != nil {
			return fmt.Errorf("check refund replay: %w", err)
		}
		if exists {
			return nil
		}
	}

	var existingRefunded, itemQuantity uint
	var variantID string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT refunded_quantity, quantity, coalesce(variant_id::text, '')
		FROM shop_order_items
		WHERE id = $1::uuid
			AND order_id = $2::uuid
		FOR UPDATE
	`, orderItemID, orderID).Scan(&existingRefunded, &itemQuantity, &variantID); err != nil {
		return fmt.Errorf("load shop item for refund: %w", err)
	}
	if existingRefunded+quantity > itemQuantity {
		return fmt.Errorf("refund quantity exceeds remaining refundable quantity")
	}

	var refundID string
	status := "succeeded"
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO refunds (
			order_id, provider, provider_refund_id, amount_cents, currency, reason,
			status, requested_by, completed_at
		)
		SELECT id, $2, $3, $4, currency, $5, $6, $7, now()
		FROM shop_orders
		WHERE id = $1::uuid
		RETURNING id::text
	`, orderID, provider, providerRefundID, int64(amountCents),
		strings.TrimSpace(reason), status, strings.ToLower(strings.TrimSpace(actorEmail))).Scan(&refundID)
	if err != nil {
		return fmt.Errorf("insert refund: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO refund_items (refund_id, order_item_id, quantity, amount_cents, restock)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
	`, refundID, orderItemID, int64(quantity), int64(amountCents), restock); err != nil {
		return fmt.Errorf("insert refund item: %w", err)
	}
	newRefunded := existingRefunded + quantity
	itemStatus := types.ShopItemStatusPartiallyRefunded
	if newRefunded >= itemQuantity {
		itemStatus = types.ShopItemStatusRefunded
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_order_items
		SET refunded_quantity = $2,
			status = $3
		WHERE id = $1::uuid
	`, orderItemID, int64(newRefunded), itemStatus); err != nil {
		return fmt.Errorf("update refunded item: %w", err)
	}
	if restock && variantID != "" {
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO merch_inventory_events (
				variant_id, event_type, quantity_delta, order_item_id, actor_email, notes
			) VALUES (
				$1::uuid, 'refund', $2, $3::uuid, NULLIF($4, '')::citext, $5
			)
		`, variantID, int64(quantity), orderItemID, strings.ToLower(strings.TrimSpace(actorEmail)), strings.TrimSpace(reason)); err != nil {
			return fmt.Errorf("record refund inventory event: %w", err)
		}
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		WITH item_state AS (
			SELECT
				count(*) FILTER (WHERE status <> 'refunded') AS not_refunded,
				count(*) FILTER (WHERE status = 'partially_refunded') AS partial
			FROM shop_order_items
			WHERE order_id = $1::uuid
		)
		UPDATE shop_orders
		SET status = CASE
				WHEN item_state.not_refunded = 0 THEN 'refunded'
				ELSE 'partially_refunded'
			END
		FROM item_state
		WHERE shop_orders.id = $1::uuid
	`, orderID); err != nil {
		return fmt.Errorf("update refunded order status: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (
			event_type, actor_type, actor_email, entity_type, entity_id, order_id, order_item_id, metadata
		) VALUES (
			'order.refunded', 'admin', NULLIF($3, '')::citext, 'refund', $1::uuid, $2::uuid, $4::uuid,
			jsonb_build_object('amount_cents', $5::integer, 'quantity', $6::integer, 'restock', $7::boolean, 'provider', $8::text, 'reason', $9::text)
		)
	`, refundID, orderID, strings.ToLower(strings.TrimSpace(actorEmail)), orderItemID, int64(amountCents), int64(quantity), restock, strings.TrimSpace(provider), strings.TrimSpace(reason)); err != nil {
		return fmt.Errorf("record refund event: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

// MarkShopOrderPaid returns true only for the first successful pending->paid
// transition. Webhook replays return false without duplicating inventory,
// audit events, or downstream receipt work.
func MarkShopOrderPaid(ctx *config.AppContext, orderID, provider, providerID string, salesTaxAmountCents uint, totalCents uint) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("database is not configured")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return false, fmt.Errorf("order id is required")
	}
	provider = strings.TrimSpace(provider)
	providerID = strings.TrimSpace(providerID)
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var currentStatus, currentProvider, currentProviderID string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT status, payment_provider, payment_provider_id
		FROM shop_orders
		WHERE id = $1::uuid
		FOR UPDATE
	`, orderID).Scan(&currentStatus, &currentProvider, &currentProviderID); err != nil {
		return false, fmt.Errorf("load shop order payment state: %w", err)
	}
	if currentStatus == types.ShopOrderStatusPaid {
		if currentProvider != provider || currentProviderID != providerID {
			return false, fmt.Errorf("shop order %s was already paid by %s/%s", orderID, currentProvider, currentProviderID)
		}
		// Payment callbacks are intentionally replay-safe. Use a valid replay
		// to repair orders created before pos_takeaway items were fulfilled as
		// part of the paid transition.
		if err := fulfillPaidPOSTakeawayItems(ctx, tx, orderID); err != nil {
			return false, err
		}
		if err := tx.Commit(ctx.DatabaseContext()); err != nil {
			return false, err
		}
		return false, nil
	}
	if currentStatus != types.ShopOrderStatusPending {
		return false, fmt.Errorf("shop order %s cannot be paid from status %s", orderID, currentStatus)
	}
	commandTag, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_orders
		SET status = 'paid',
			payment_provider = $2,
			payment_provider_id = $3,
			sales_tax_amount_cents = CASE WHEN $4 > 0 THEN $4 ELSE sales_tax_amount_cents END,
			total_cents = CASE WHEN $5 > 0 THEN $5 ELSE total_cents END,
			paid_at = coalesce(paid_at, now())
		WHERE id = $1::uuid
			AND status = 'pending'
	`, orderID, provider, providerID, int64(salesTaxAmountCents), int64(totalCents))
	if err != nil {
		return false, fmt.Errorf("mark shop order paid: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return false, fmt.Errorf("shop order %s payment state changed concurrently", orderID)
	}
	reservations, err := listShopOrderReservationsForUpdate(ctx, tx, orderID, "active", "released", "expired")
	if err != nil {
		return false, err
	}
	for _, reservation := range reservations {
		quantityDelta := int64(0)
		if reservation.Status != "active" {
			// A provider webhook may arrive after cleanup. Honor the paid
			// purchase and make the stock ledger reflect the sale again.
			quantityDelta = -int64(reservation.Quantity)
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE merch_inventory_reservations
			SET status = 'converted'
			WHERE id = $1::uuid AND status = $2
		`, reservation.ID, reservation.Status); err != nil {
			return false, fmt.Errorf("convert inventory reservation: %w", err)
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO merch_inventory_events (
				variant_id, event_type, quantity_delta, order_item_id, conference_id, notes
			) VALUES (
				$1::uuid, 'sale', $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, 'payment completed'
			)
		`, reservation.VariantID, quantityDelta, reservation.OrderItemID, reservation.ConferenceID); err != nil {
			return false, fmt.Errorf("record inventory sale: %w", err)
		}
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_order_items
		SET status = CASE
				WHEN fulfillment_method = 'event_pickup' THEN 'ready'
				WHEN fulfillment_method = 'pos_takeaway' THEN 'fulfilled'
				ELSE 'pending'
			END,
			fulfilled_quantity = CASE
				WHEN fulfillment_method = 'pos_takeaway' THEN quantity
				ELSE fulfilled_quantity
			END
		WHERE order_id = $1::uuid
			AND status = 'pending'
	`, orderID); err != nil {
		return false, fmt.Errorf("mark shop items ready: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO shop_events (event_type, actor_type, entity_type, entity_id, order_id)
		VALUES ('order.paid', 'system', 'shop_order', $1::uuid, $1::uuid)
	`, orderID); err != nil {
		return false, fmt.Errorf("record shop paid event: %w", err)
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return false, err
	}
	return true, nil
}

func fulfillPaidPOSTakeawayItems(ctx *config.AppContext, tx pgx.Tx, orderID string) error {
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE shop_order_items
		SET status = 'fulfilled',
			fulfilled_quantity = quantity
		WHERE order_id = $1::uuid
			AND fulfillment_method = 'pos_takeaway'
			AND status NOT IN ('cancelled', 'refunded', 'partially_refunded')
			AND (status <> 'fulfilled' OR fulfilled_quantity <> quantity)
	`, orderID); err != nil {
		return fmt.Errorf("fulfill paid pos takeaway items: %w", err)
	}
	return nil
}

func CreateShippingRateQuote(ctx *config.AppContext, in ShippingRateQuoteInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(in.OrderID) == "" {
		return fmt.Errorf("order id is required")
	}
	provider := firstNonEmpty(strings.TrimSpace(in.Provider), types.ShippingProviderEasyship)
	currency := strings.ToUpper(firstNonEmpty(in.Currency, "USD"))
	raw := strings.TrimSpace(in.RawResponse)
	if raw == "" {
		raw = "{}"
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO shipping_rate_quotes (
			order_id, provider, provider_quote_id, courier_service_id, destination_country, destination_region,
			destination_postal_code, courier_name, service_name, amount_cents, currency,
			estimated_min_days, estimated_max_days, raw_response, expires_at
		) VALUES (
			$1::uuid, $2, $3, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14
		)
	`, strings.TrimSpace(in.OrderID), provider, strings.TrimSpace(in.ProviderQuoteID),
		strings.ToUpper(strings.TrimSpace(in.DestinationCountry)), strings.TrimSpace(in.DestinationRegion),
		strings.TrimSpace(in.DestinationPostalCode), strings.TrimSpace(in.CourierName),
		strings.TrimSpace(in.ServiceName), int64(in.AmountCents), currency,
		in.EstimatedMinDays, in.EstimatedMaxDays, raw, in.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create shipping rate quote: %w", err)
	}
	return nil
}

func CreateTaxQuote(ctx *config.AppContext, in TaxQuoteInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(in.OrderID) == "" {
		return fmt.Errorf("order id is required")
	}
	raw := strings.TrimSpace(in.RawResponse)
	if raw == "" {
		raw = "{}"
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO tax_quotes (
			order_id, sales_tax_provider, provider_quote_id, sales_tax_amount_cents,
			destination_country, destination_region, destination_postal_code,
			raw_tax_response, expires_at
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
	`, strings.TrimSpace(in.OrderID), firstNonEmpty(strings.TrimSpace(in.Provider), types.TaxProviderStripe),
		strings.TrimSpace(in.ProviderQuoteID), int64(in.SalesTaxAmountCents), strings.TrimSpace(in.DestinationCountry),
		strings.TrimSpace(in.DestinationRegion), strings.TrimSpace(in.DestinationPostalCode), raw, in.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create tax quote: %w", err)
	}
	return nil
}

func GetShopTaxCalculationID(ctx *config.AppContext, orderID string) (string, error) {
	var calculationID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT provider_quote_id
		FROM tax_quotes
		WHERE order_id = $1::uuid
			AND sales_tax_provider = 'stripe'
			AND provider_quote_id <> ''
		ORDER BY created_at DESC
		LIMIT 1
	`, strings.TrimSpace(orderID)).Scan(&calculationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load shop tax calculation: %w", err)
	}
	return calculationID, nil
}

func HasRecordedShopTaxTransaction(ctx *config.AppContext, orderID string) (bool, error) {
	var exists bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1 FROM tax_transactions
			WHERE order_id = $1::uuid AND provider = 'stripe' AND status = 'recorded'
		)
	`, strings.TrimSpace(orderID)).Scan(&exists)
	return exists, err
}

func RecordShopTaxTransaction(ctx *config.AppContext, orderID, providerTransactionID string, amountCents uint, rawResponse string) error {
	rawResponse = strings.TrimSpace(rawResponse)
	if rawResponse == "" {
		rawResponse = "{}"
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO tax_transactions (
			order_id, provider, provider_transaction_id, sales_tax_amount_cents, status, raw_response
		) VALUES ($1::uuid, 'stripe', $2, $3, 'recorded', $4::jsonb)
		ON CONFLICT (order_id, provider) WHERE status = 'recorded' DO NOTHING
	`, strings.TrimSpace(orderID), strings.TrimSpace(providerTransactionID), int64(amountCents), rawResponse)
	if err != nil {
		return fmt.Errorf("record shop tax transaction: %w", err)
	}
	return nil
}

func queryMerchProducts(ctx *config.AppContext, whereSQL string, args []any) ([]*types.MerchProduct, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	orderSQL := "ORDER BY merch_products.status = 'published' DESC, merch_products.created_at DESC, merch_products.name"
	if strings.Contains(strings.ToLower(whereSQL), "order by") {
		orderSQL = ""
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT merch_products.id::text, merch_products.tag, merch_products.slug, merch_products.name,
			merch_products.subtitle, merch_products.description, merch_products.status,
			merch_products.product_type, merch_products.base_price_cents, merch_products.currency,
			merch_products.symbol, merch_products.post_symbol, merch_products.stripe_tax_code,
			merch_products.easyship_category, merch_products.hs_code, merch_products.country_of_origin,
			merch_products.requires_shipping, merch_products.allow_event_pickup,
			merch_products.available_from, merch_products.available_until,
			merch_products.created_at, merch_products.updated_at
		FROM merch_products
		`+whereSQL+`
		`+orderSQL+`
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query merch products: %w", err)
	}
	defer rows.Close()

	var products []*types.MerchProduct
	ids := []string{}
	for rows.Next() {
		var p types.MerchProduct
		if err := rows.Scan(
			&p.ID, &p.Tag, &p.Slug, &p.Name, &p.Subtitle, &p.Description, &p.Status,
			&p.ProductType, &p.BasePriceCents, &p.Currency, &p.Symbol, &p.PostSymbol,
			&p.StripeTaxCode, &p.EasyshipCategory, &p.HSCode, &p.CountryOfOrigin,
			&p.RequiresShipping, &p.AllowEventPickup, &p.AvailableFrom, &p.AvailableUntil,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan merch product: %w", err)
		}
		products = append(products, &p)
		ids = append(ids, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return products, nil
	}
	images, err := listMerchImages(ctx, ids)
	if err != nil {
		return nil, err
	}
	variants, err := listMerchVariants(ctx, ids)
	if err != nil {
		return nil, err
	}
	options, err := listMerchOptions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, p := range products {
		p.Images = images[p.ID]
		p.Variants = variants[p.ID]
		p.Options = options[p.ID]
	}
	return products, nil
}

func listMerchOptions(ctx *config.AppContext, productIDs []string) (map[string][]*types.MerchProductOption, error) {
	out := map[string][]*types.MerchProductOption{}
	byID := map[string]*types.MerchProductOption{}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, product_id::text, name, display_order, required, created_at, updated_at
		FROM merch_product_options WHERE product_id::text = ANY($1)
		ORDER BY product_id, display_order, name
	`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("query merch options: %w", err)
	}
	for rows.Next() {
		option := &types.MerchProductOption{}
		if err := rows.Scan(&option.ID, &option.ProductID, &option.Name, &option.DisplayOrder, &option.Required, &option.CreatedAt, &option.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		byID[option.ID] = option
		out[option.ProductID] = append(out[option.ProductID], option)
	}
	rows.Close()
	valueRows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, option_id::text, value, display_order, created_at
		FROM merch_product_option_values
		WHERE option_id::text = ANY($1)
		ORDER BY option_id, display_order, value
	`, mapKeysOptions(byID))
	if err != nil {
		return nil, fmt.Errorf("query merch option values: %w", err)
	}
	defer valueRows.Close()
	for valueRows.Next() {
		value := &types.MerchProductOptionValue{}
		if err := valueRows.Scan(&value.ID, &value.OptionID, &value.Value, &value.DisplayOrder, &value.CreatedAt); err != nil {
			return nil, err
		}
		if option := byID[value.OptionID]; option != nil {
			option.Values = append(option.Values, value)
		}
	}
	return out, valueRows.Err()
}

func mapKeysOptions(values map[string]*types.MerchProductOption) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}

func listMerchImages(ctx *config.AppContext, productIDs []string) (map[string][]*types.MerchProductImage, error) {
	out := map[string][]*types.MerchProductImage{}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, product_id::text, object_key, alt_text, display_order, is_primary, created_at
		FROM merch_product_images
		WHERE product_id::text = ANY($1)
		ORDER BY product_id, is_primary DESC, display_order, created_at
	`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("query merch images: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var img types.MerchProductImage
		if err := rows.Scan(&img.ID, &img.ProductID, &img.ObjectKey, &img.AltText, &img.DisplayOrder, &img.IsPrimary, &img.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan merch image: %w", err)
		}
		out[img.ProductID] = append(out[img.ProductID], &img)
	}
	return out, rows.Err()
}

func listMerchVariants(ctx *config.AppContext, productIDs []string) (map[string][]*types.MerchVariant, error) {
	out := map[string][]*types.MerchVariant{}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, product_id::text, sku, label, price_delta_cents,
			coalesce((
				SELECT sum(quantity_delta)::int
				FROM merch_inventory_events mie
				WHERE mie.variant_id = merch_variants.id
			), 0) AS stock,
			weight_grams,
			length_mm, width_mm, height_mm, inventory_policy, status, created_at, updated_at
		FROM merch_variants
		WHERE product_id::text = ANY($1)
		ORDER BY product_id, label
	`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("query merch variants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v types.MerchVariant
		if err := rows.Scan(&v.ID, &v.ProductID, &v.SKU, &v.Label, &v.PriceDeltaCents, &v.Stock, &v.WeightGrams, &v.LengthMM, &v.WidthMM, &v.HeightMM, &v.InventoryPolicy, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan merch variant: %w", err)
		}
		out[v.ProductID] = append(out[v.ProductID], &v)
	}
	return out, rows.Err()
}

func listShopOrderItems(ctx *config.AppContext, orderID string) ([]*types.ShopOrderItem, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT soi.id::text, soi.order_id::text, coalesce(soi.product_id::text, ''), coalesce(soi.variant_id::text, ''),
			soi.quantity, soi.fulfilled_quantity, soi.refunded_quantity, soi.unit_price_cents,
			soi.discount_amount_cents, soi.tax_amount_cents, soi.line_total_cents, soi.product_tag_snapshot,
			soi.product_name_snapshot, soi.variant_label_snapshot, soi.sku_snapshot,
			coalesce(img.object_key, '') AS image_object_key,
			soi.fulfillment_method, coalesce(soi.sale_conference_id::text, ''),
			coalesce(sale_conf.tag, ''),
			coalesce(soi.pickup_conference_id::text, ''), soi.status, soi.created_at, soi.updated_at
		FROM shop_order_items soi
		LEFT JOIN conferences sale_conf ON sale_conf.id = soi.sale_conference_id
		LEFT JOIN LATERAL (
			SELECT object_key
			FROM merch_product_images mpi
			WHERE mpi.product_id = soi.product_id
			ORDER BY mpi.is_primary DESC, mpi.display_order, mpi.created_at
			LIMIT 1
		) img ON true
		WHERE soi.order_id = $1::uuid
		ORDER BY soi.created_at
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list shop order items: %w", err)
	}
	defer rows.Close()
	return scanShopOrderItems(rows, "shop order items")
}

type shopOrderItemRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanShopOrderItems(rows shopOrderItemRows, label string) ([]*types.ShopOrderItem, error) {
	var out []*types.ShopOrderItem
	for rows.Next() {
		var item types.ShopOrderItem
		if err := rows.Scan(
			&item.ID, &item.OrderID, &item.ProductID, &item.VariantID,
			&item.Quantity, &item.FulfilledQuantity, &item.RefundedQuantity,
			&item.UnitPriceCents, &item.DiscountAmountCents, &item.TaxAmountCents, &item.LineTotalCents,
			&item.ProductTagSnapshot, &item.ProductNameSnapshot,
			&item.VariantLabelSnapshot, &item.SKUSnapshot, &item.ImageObjectKey, &item.FulfillmentMethod,
			&item.SaleConferenceID, &item.SaleConferenceTag, &item.PickupConferenceID, &item.Status,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		out = append(out, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return out, nil
}

func normalizeMerchProductInput(in MerchProductInput) MerchProductInput {
	in.Tag = normalizeShopSlug(in.Tag)
	in.Slug = normalizeShopSlug(in.Slug)
	in.Name = strings.TrimSpace(in.Name)
	in.Subtitle = strings.TrimSpace(in.Subtitle)
	in.Description = strings.TrimSpace(in.Description)
	in.Status = firstNonEmpty(strings.TrimSpace(in.Status), types.MerchProductStatusDraft)
	in.ProductType = firstNonEmpty(strings.TrimSpace(in.ProductType), "standard")
	in.Currency = strings.ToUpper(firstNonEmpty(strings.TrimSpace(in.Currency), "USD"))
	in.Symbol = firstNonEmpty(strings.TrimSpace(in.Symbol), "$")
	in.PostSymbol = strings.TrimSpace(in.PostSymbol)
	in.StripeTaxCode = strings.TrimSpace(in.StripeTaxCode)
	in.EasyshipCategory = strings.TrimSpace(in.EasyshipCategory)
	in.HSCode = strings.TrimSpace(in.HSCode)
	in.CountryOfOrigin = strings.ToUpper(strings.TrimSpace(in.CountryOfOrigin))
	return in
}

func normalizeMerchVariantInput(in MerchVariantInput) MerchVariantInput {
	in.ProductID = strings.TrimSpace(in.ProductID)
	in.SKU = strings.TrimSpace(in.SKU)
	in.Label = strings.TrimSpace(in.Label)
	in.InventoryPolicy = firstNonEmpty(strings.TrimSpace(in.InventoryPolicy), types.MerchInventoryPolicyDeny)
	in.Status = firstNonEmpty(strings.TrimSpace(in.Status), "active")
	return in
}

func normalizeShopSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
