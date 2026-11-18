package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrShopEditConflict = errors.New("This shop was updated by someone else. Reload before saving.")
var shopCountryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
var shopSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,79}$`)
var shopTaxCodePattern = regexp.MustCompile(`^txcd_[0-9]{8}$`)

func GetOrganizationShopProfile(app *config.AppContext, orgID string) (*types.OrganizationShopProfile, error) {
	out := &types.OrganizationShopProfile{OrganizationID: orgID}
	var raw []byte
	e := app.DB.QueryRow(app.DatabaseContext(), `SELECT data,revision FROM organization_shop_profiles WHERE organization_id=$1`, orgID).Scan(&raw, &out.Revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	revision := out.Revision
	if e = json.Unmarshal(raw, out); e != nil {
		return nil, e
	}
	out.OrganizationID = orgID
	out.Revision = revision
	return out, nil
}
func ValidateOrganizationShopProfile(p *types.OrganizationShopProfile) error {
	if p == nil {
		return fmt.Errorf("shop profile is required")
	}
	o := &p.ShipFrom
	o.ContactName = strings.TrimSpace(o.ContactName)
	o.CountryAlpha2 = strings.ToUpper(strings.TrimSpace(o.CountryAlpha2))
	p.Billing.Country = strings.ToUpper(strings.TrimSpace(p.Billing.Country))
	if !o.IsConfigured() || strings.TrimSpace(o.Email) == "" || strings.TrimSpace(o.Phone) == "" {
		return fmt.Errorf("complete the shipping origin, including contact, email, phone and address")
	}
	if a, e := mail.ParseAddress(o.Email); e != nil || a.Address != o.Email {
		return fmt.Errorf("enter a valid ship-from contact email")
	}
	if strings.TrimSpace(p.BillingName) == "" || strings.TrimSpace(p.Billing.Line1) == "" || strings.TrimSpace(p.Billing.City) == "" || strings.TrimSpace(p.Billing.PostalCode) == "" || !shopCountryPattern.MatchString(p.Billing.Country) {
		return fmt.Errorf("complete the organization’s legal name and billing address")
	}
	return nil
}
func SaveOrganizationShopProfile(app *config.AppContext, orgID, actor string, p types.OrganizationShopProfile) (int64, error) {
	if e := ValidateOrganizationShopProfile(&p); e != nil {
		return 0, e
	}
	p.OrganizationID = orgID
	c := app.DatabaseContext()
	tx, e := app.DB.Begin(c)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(c)
	if _, e = tx.Exec(c, `INSERT INTO organization_shop_profiles(organization_id) VALUES($1) ON CONFLICT DO NOTHING`, orgID); e != nil {
		return 0, e
	}
	var revision int64
	if e = tx.QueryRow(c, `SELECT revision FROM organization_shop_profiles WHERE organization_id=$1 FOR UPDATE`, orgID).Scan(&revision); e != nil {
		return 0, e
	}
	if revision != p.Revision {
		return 0, ErrShopEditConflict
	}
	p.Revision = revision + 1
	raw, e := json.Marshal(p)
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(c, `UPDATE organization_shop_profiles SET data=$2,revision=revision+1,updated_by=$3,updated_at=now() WHERE organization_id=$1`, orgID, raw, nullableUUID(actor)); e != nil {
		return 0, e
	}
	return p.Revision, tx.Commit(c)
}
func ListOrganizationMerch(app *config.AppContext, orgID string) ([]*types.MerchProduct, error) {
	return queryMerchProducts(app, `WHERE fulfillment_organization_id=$1`, []any{orgID})
}
func OrganizationOwnsProduct(app *config.AppContext, orgID, productID string) bool {
	var yes bool
	e := app.DB.QueryRow(app.DatabaseContext(), `SELECT EXISTS(SELECT 1 FROM merch_products WHERE id=$1 AND fulfillment_organization_id=$2)`, productID, orgID).Scan(&yes)
	return e == nil && yes
}

// OrganizationProductInput keeps commercial fields in the shared catalog.
type OrganizationProductInput struct {
	ID        string
	UpdatedAt time.Time
	MerchProductInput
	OnlineEnabled bool
	EventIDs      []string
}

func SaveOrganizationProduct(app *config.AppContext, orgID string, in OrganizationProductInput) (string, error) {
	in.MerchProductInput = normalizeMerchProductInput(in.MerchProductInput)
	if !shopSlugPattern.MatchString(in.Slug) || strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
		return "", fmt.Errorf("enter a product name and a URL with lowercase letters, numbers and hyphens")
	}
	if in.BasePriceCents < 1 || in.BasePriceCents > 100000000 || in.Currency != "USD" {
		return "", fmt.Errorf("use a positive USD price up to $1,000,000")
	}
	switch in.Status {
	case "draft", "published", "archived":
	default:
		return "", fmt.Errorf("invalid product status")
	}
	switch in.ProductType {
	case "apparel", "accessories", "prints", "stickers", "exclusive", "standard", "other":
	default:
		return "", fmt.Errorf("choose a physical product category")
	}
	if !shopTaxCodePattern.MatchString(in.StripeTaxCode) {
		return "", fmt.Errorf("choose a Stripe tax classification")
	}
	if !shopCountryPattern.MatchString(in.CountryOfOrigin) {
		return "", fmt.Errorf("enter the two-letter country where the item was manufactured")
	}
	if len(in.EventIDs) > 100 {
		return "", fmt.Errorf("choose at most 100 events")
	}
	if in.Status == "published" {
		profile, e := GetOrganizationShopProfile(app, orgID)
		if e != nil {
			return "", e
		}
		if e = ValidateOrganizationShopProfile(profile); e != nil {
			return "", e
		}
		if !in.OnlineEnabled && len(in.EventIDs) == 0 {
			return "", fmt.Errorf("choose online sales or at least one event")
		}
	}
	c := app.DatabaseContext()
	tx, e := app.DB.Begin(c)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(c)
	if in.ID != "" {
		var updated time.Time
		if e = tx.QueryRow(c, `SELECT updated_at FROM merch_products WHERE id=$1 AND fulfillment_organization_id=$2 FOR UPDATE`, in.ID, orgID).Scan(&updated); e != nil {
			return "", fmt.Errorf("product not found in this organization")
		}
		var valid bool
		if e = tx.QueryRow(c, `SELECT NOT EXISTS(SELECT 1 FROM merch_variants WHERE product_id=$1 AND ($2::bigint+price_delta_cents<1 OR $2::bigint+price_delta_cents>100000000))`, in.ID, in.BasePriceCents).Scan(&valid); e != nil {
			return "", e
		}
		if !valid {
			return "", fmt.Errorf("the base price must leave every variant with a valid positive price")
		}
		if !updated.Equal(in.UpdatedAt) {
			return "", ErrShopEditConflict
		}
	}
	if in.ID == "" {
		in.ID = uuid.NewString()
		_, e = tx.Exec(c, `INSERT INTO merch_products(id,fulfillment_organization_id,tag,slug,name,subtitle,description,status,product_type,base_price_cents,currency,symbol,stripe_tax_code,easyship_category,hs_code,country_of_origin,requires_shipping,allow_event_pickup,online_enabled,published_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'USD','$',$11,$12,$13,$14,true,$15,$16,CASE WHEN $8='published' THEN now() END)`, in.ID, orgID, "org-"+in.ID, in.Slug, in.Name, in.Subtitle, in.Description, in.Status, in.ProductType, in.BasePriceCents, in.StripeTaxCode, in.EasyshipCategory, in.HSCode, in.CountryOfOrigin, len(in.EventIDs) > 0, in.OnlineEnabled)
	} else {
		_, e = tx.Exec(c, `UPDATE merch_products SET slug=$3,name=$4,subtitle=$5,description=$6,status=$7,product_type=$8,base_price_cents=$9,stripe_tax_code=$10,easyship_category=$11,hs_code=$12,country_of_origin=$13,allow_event_pickup=$14,online_enabled=$15,published_at=CASE WHEN $7='published' THEN coalesce(published_at,now()) ELSE published_at END WHERE id=$1 AND fulfillment_organization_id=$2`, in.ID, orgID, in.Slug, in.Name, in.Subtitle, in.Description, in.Status, in.ProductType, in.BasePriceCents, in.StripeTaxCode, in.EasyshipCategory, in.HSCode, in.CountryOfOrigin, len(in.EventIDs) > 0, in.OnlineEnabled)
	}
	if e != nil {
		return "", e
	}
	if _, e = tx.Exec(c, `DELETE FROM merch_product_events WHERE product_id=$1`, in.ID); e != nil {
		return "", e
	}
	seen := map[string]bool{}
	for _, confID := range in.EventIDs {
		if seen[confID] {
			continue
		}
		seen[confID] = true
		var exists bool
		if e = tx.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM conferences WHERE id=$1 AND publication_status='published' AND end_date>now())`, confID).Scan(&exists); e != nil || !exists {
			return "", fmt.Errorf("select upcoming published events")
		}
		if _, e = tx.Exec(c, `INSERT INTO merch_product_events(product_id,conference_id) VALUES($1,$2)`, in.ID, confID); e != nil {
			return "", e
		}
	}
	return in.ID, tx.Commit(c)
}
func OrganizationProductEvents(app *config.AppContext, productID string) ([]string, error) {
	rows, e := app.DB.Query(app.DatabaseContext(), `SELECT conference_id::text FROM merch_product_events WHERE product_id=$1 ORDER BY conference_id`, productID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Lock the variant before adjusting its ledger, matching the checkout reservation lock.
func SaveOrganizationVariant(app *config.AppContext, orgID, productID, variantID, actor, operation string, in MerchVariantInput, delta int) (string, error) {
	if _, e := uuid.Parse(operation); e != nil {
		return "", fmt.Errorf("invalid stock operation")
	}
	in = normalizeMerchVariantInput(in)
	if in.SKU == "" || in.Label == "" || in.WeightGrams < 0 || in.LengthMM < 0 || in.WidthMM < 0 || in.HeightMM < 0 || delta < -1000000 || delta > 1000000 {
		return "", fmt.Errorf("check variant name, SKU, parcel dimensions, and quantity")
	}
	if in.Status != "active" && in.Status != "archived" {
		return "", fmt.Errorf("invalid variant status")
	}
	c := app.DatabaseContext()
	tx, e := app.DB.Begin(c)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(c)
	var price int
	if e = tx.QueryRow(c, `SELECT base_price_cents FROM merch_products WHERE id=$1 AND fulfillment_organization_id=$2 FOR UPDATE`, productID, orgID).Scan(&price); e != nil {
		return "", fmt.Errorf("product not found in this organization")
	}
	if price+in.PriceDeltaCents < 1 || price+in.PriceDeltaCents > 100000000 {
		return "", fmt.Errorf("the resulting variant price must be positive")
	}
	if variantID == "" {
		variantID = operation
		_, e = tx.Exec(c, `INSERT INTO merch_variants(id,product_id,sku,label,price_delta_cents,weight_grams,length_mm,width_mm,height_mm,inventory_policy,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'deny',$10)`, variantID, productID, in.SKU, in.Label, in.PriceDeltaCents, in.WeightGrams, in.LengthMM, in.WidthMM, in.HeightMM, in.Status)
	} else {
		var id string
		if e = tx.QueryRow(c, `SELECT id::text FROM merch_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variantID, productID).Scan(&id); e != nil {
			return "", fmt.Errorf("variant not found in this product")
		}
		_, e = tx.Exec(c, `UPDATE merch_variants SET sku=$2,label=$3,price_delta_cents=$4,weight_grams=$5,length_mm=$6,width_mm=$7,height_mm=$8,status=$9,inventory_policy='deny' WHERE id=$1`, variantID, in.SKU, in.Label, in.PriceDeltaCents, in.WeightGrams, in.LengthMM, in.WidthMM, in.HeightMM, in.Status)
	}
	if e != nil {
		return "", e
	}
	if delta != 0 {
		var applied bool
		if e = tx.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM merch_inventory_events WHERE variant_id=$1 AND operation_key=$2)`, variantID, operation).Scan(&applied); e != nil {
			return "", e
		}
		if !applied {
			var stock int
			if e = tx.QueryRow(c, `SELECT coalesce(sum(quantity_delta),0) FROM merch_inventory_events WHERE variant_id=$1`, variantID).Scan(&stock); e != nil {
				return "", e
			}
			if stock+delta < 0 {
				return "", fmt.Errorf("stock adjustment exceeds available inventory")
			}
			if _, e = tx.Exec(c, `INSERT INTO merch_inventory_events(variant_id,event_type,quantity_delta,actor_email,notes,operation_key) VALUES($1,'adjustment',$2,$3,'Organization stock adjustment',$4)`, variantID, delta, actor, operation); e != nil {
				return "", e
			}
		}
	}
	return variantID, tx.Commit(c)
}

// Used by checkout to freeze fulfillment data alongside each purchased line.
func organizationShipFromTx(c context.Context, tx pgx.Tx, orgID string) ([]byte, error) {
	var raw []byte
	if e := tx.QueryRow(c, `SELECT data->'ship_from' FROM organization_shop_profiles WHERE organization_id=$1 FOR SHARE`, orgID).Scan(&raw); e != nil {
		return nil, e
	}
	var origin types.EasyshipSettings
	if e := json.Unmarshal(raw, &origin); e != nil {
		return nil, e
	}
	if !origin.IsConfigured() {
		return nil, fmt.Errorf("organization shipping origin is incomplete")
	}
	return raw, nil
}

func ListOrganizationDiscounts(app *config.AppContext, orgID string) ([]types.OrganizationShopDiscount, error) {
	rows, e := app.DB.Query(app.DatabaseContext(), `SELECT d.id::text,d.organization_id::text,d.code::text,d.percent_off,d.active,d.starts_at,d.ends_at,d.max_uses,(SELECT count(*) FROM organization_shop_discount_uses u JOIN shop_orders o ON o.id=u.order_id WHERE u.discount_id=d.id AND o.status NOT IN ('cancelled','expired')) FROM organization_shop_discounts d WHERE d.organization_id=$1 ORDER BY d.created_at DESC`, orgID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []types.OrganizationShopDiscount{}
	for rows.Next() {
		var d types.OrganizationShopDiscount
		if e = rows.Scan(&d.ID, &d.OrganizationID, &d.Code, &d.PercentOff, &d.Active, &d.StartsAt, &d.EndsAt, &d.MaxUses, &d.Uses); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

var shopDiscountPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{2,39}$`)

func SaveOrganizationDiscount(app *config.AppContext, orgID string, d types.OrganizationShopDiscount) error {
	d.Code = strings.ToUpper(strings.TrimSpace(d.Code))
	if !shopDiscountPattern.MatchString(d.Code) || d.PercentOff < 1 || d.PercentOff > 99 || d.MaxUses < 0 || d.MaxUses > 1000000 {
		return fmt.Errorf("use a 3–40 character code, 1–99 percent discount, and a nonnegative use limit")
	}
	if d.StartsAt != nil && d.EndsAt != nil && !d.EndsAt.After(*d.StartsAt) {
		return fmt.Errorf("discount end must be after its start")
	}
	if d.ID == "" {
		_, e := app.DB.Exec(app.DatabaseContext(), `INSERT INTO organization_shop_discounts(organization_id,code,percent_off,active,starts_at,ends_at,max_uses) VALUES($1,$2,$3,$4,$5,$6,$7)`, orgID, d.Code, d.PercentOff, d.Active, d.StartsAt, d.EndsAt, d.MaxUses)
		return e
	}
	result, e := app.DB.Exec(app.DatabaseContext(), `UPDATE organization_shop_discounts SET code=$3,percent_off=$4,active=$5,starts_at=$6,ends_at=$7,max_uses=$8,updated_at=now() WHERE id=$1 AND organization_id=$2`, d.ID, orgID, d.Code, d.PercentOff, d.Active, d.StartsAt, d.EndsAt, d.MaxUses)
	if e != nil {
		return e
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("discount not found in this organization")
	}
	return nil
}
func FindOrganizationDiscount(app *config.AppContext, code string) (*types.OrganizationShopDiscount, error) {
	var d types.OrganizationShopDiscount
	e := app.DB.QueryRow(app.DatabaseContext(), `SELECT d.id::text,d.organization_id::text,d.code::text,d.percent_off,d.active,d.starts_at,d.ends_at,d.max_uses,(SELECT count(*) FROM organization_shop_discount_uses u JOIN shop_orders o ON o.id=u.order_id WHERE u.discount_id=d.id AND o.status NOT IN ('cancelled','expired')) FROM organization_shop_discounts d WHERE d.code=$1 AND d.active AND (d.starts_at IS NULL OR d.starts_at<=now()) AND (d.ends_at IS NULL OR d.ends_at>now())`, strings.TrimSpace(code)).Scan(&d.ID, &d.OrganizationID, &d.Code, &d.PercentOff, &d.Active, &d.StartsAt, &d.EndsAt, &d.MaxUses, &d.Uses)
	if e != nil || d.MaxUses > 0 && d.Uses >= d.MaxUses {
		return nil, fmt.Errorf("this discount is unavailable or has reached its use limit")
	}
	return &d, nil
}

// Reserve discount use in the same transaction as stock; expired/cancelled orders release usage.
func reserveOrganizationDiscount(c context.Context, tx pgx.Tx, orderID, code string, items []ShopOrderItemInput, expected uint) error {
	if code == "" {
		return nil
	}
	var d types.OrganizationShopDiscount
	var available bool
	e := tx.QueryRow(c, `SELECT id::text,organization_id::text,percent_off,max_uses,active AND (starts_at IS NULL OR starts_at<=now()) AND (ends_at IS NULL OR ends_at>now()) FROM organization_shop_discounts WHERE code=$1 FOR UPDATE`, code).Scan(&d.ID, &d.OrganizationID, &d.PercentOff, &d.MaxUses, &available)
	if e != nil || !available {
		return fmt.Errorf("discount expired; review the cart again")
	}
	if e = tx.QueryRow(c, `SELECT count(*) FROM organization_shop_discount_uses u JOIN shop_orders o ON o.id=u.order_id WHERE u.discount_id=$1 AND o.status NOT IN ('cancelled','expired')`, d.ID).Scan(&d.Uses); e != nil {
		return e
	}
	if d.MaxUses > 0 && d.Uses >= d.MaxUses {
		return fmt.Errorf("discount reached its use limit")
	}
	var amount uint
	for _, item := range items {
		var org string
		if item.ProductID == "" {
			continue
		}
		if e = tx.QueryRow(c, `SELECT coalesce(fulfillment_organization_id::text,'') FROM merch_products WHERE id=$1`, item.ProductID).Scan(&org); e != nil {
			return e
		}
		discount := uint(0)
		if org == d.OrganizationID {
			discount = item.UnitPriceCents * uint(d.PercentOff) / 100 * item.Quantity
		}
		if item.DiscountAmountCents != discount || item.LineTotalCents != item.UnitPriceCents*item.Quantity-discount {
			return fmt.Errorf("discount changed; review the cart again")
		}
		amount += discount
	}
	if amount == 0 || amount != expected {
		return fmt.Errorf("discount does not apply to these items")
	}
	_, e = tx.Exec(c, `INSERT INTO organization_shop_discount_uses(discount_id,order_id,amount_cents) VALUES($1,$2,$3)`, d.ID, orderID, amount)
	return e
}

type OrganizationShopOrder struct {
	ID              string
	PublicID        string
	BuyerName       string
	BuyerEmail      string
	Status          string
	CreatedAt       time.Time
	Items           []*types.ShopOrderItem
	Address         *types.ShopAddress
	ShipFrom        *types.EasyshipSettings
	CanShip         bool
	Tracking        []string
	ShippingService string
	ShippingCents   uint
}

func ListOrganizationShopOrders(app *config.AppContext, orgID string) ([]*OrganizationShopOrder, error) {
	c := app.DatabaseContext()
	rows, e := app.DB.Query(c, `SELECT o.id::text,o.public_id,o.buyer_name,coalesce(o.buyer_email::text,''),o.status,o.created_at FROM shop_orders o WHERE o.status IN ('paid','partially_refunded','refunded') AND EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id AND i.fulfillment_organization_id=$1) ORDER BY o.created_at DESC LIMIT 100`, orgID)
	if e != nil {
		return nil, e
	}
	out := []*OrganizationShopOrder{}
	for rows.Next() {
		o := &OrganizationShopOrder{}
		if e = rows.Scan(&o.ID, &o.PublicID, &o.BuyerName, &o.BuyerEmail, &o.Status, &o.CreatedAt); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, o := range out {
		items, e := app.DB.Query(c, `SELECT id::text,quantity,fulfilled_quantity,refunded_quantity,product_name_snapshot,variant_label_snapshot,sku_snapshot,fulfillment_method,line_total_cents,status,ship_from_snapshot FROM shop_order_items WHERE order_id=$1 AND fulfillment_organization_id=$2 ORDER BY created_at`, o.ID, orgID)
		if e != nil {
			return nil, e
		}
		for items.Next() {
			item := &types.ShopOrderItem{}
			var raw []byte
			if e = items.Scan(&item.ID, &item.Quantity, &item.FulfilledQuantity, &item.RefundedQuantity, &item.ProductNameSnapshot, &item.VariantLabelSnapshot, &item.SKUSnapshot, &item.FulfillmentMethod, &item.LineTotalCents, &item.Status, &raw); e != nil {
				items.Close()
				return nil, e
			}
			o.Items = append(o.Items, item)
			if item.FulfillmentMethod == types.ShopFulfillmentShip {
				origin := &types.EasyshipSettings{}
				if json.Unmarshal(raw, origin) == nil && origin.IsConfigured() {
					o.ShipFrom = origin
				}
				if o.Status != "refunded" && item.Quantity > item.FulfilledQuantity+item.RefundedQuantity {
					o.CanShip = true
				}
			}
		}
		e = items.Err()
		items.Close()
		if e != nil {
			return nil, e
		}
		var shippingRaw []byte
		var courier, service string
		var shippingCost uint
		err := app.DB.QueryRow(c, `SELECT raw_response,courier_name,service_name,amount_cents FROM shipping_rate_quotes WHERE order_id=$1 ORDER BY created_at DESC LIMIT 1`, o.ID).Scan(&shippingRaw, &courier, &service, &shippingCost)
		if err == nil {
			var bundle struct {
				Rates []struct {
					OrganizationID string `json:"organization_id"`
					Rate           struct {
						CourierName, ServiceName string
						AmountCents              uint
					} `json:"rate"`
				} `json:"organization_rates"`
			}
			if json.Unmarshal(shippingRaw, &bundle) == nil && len(bundle.Rates) > 0 {
				for _, part := range bundle.Rates {
					if part.OrganizationID == orgID {
						o.ShippingService = part.Rate.CourierName + " · " + part.Rate.ServiceName
						o.ShippingCents = part.Rate.AmountCents
					}
				}
			} else {
				o.ShippingService = courier + " · " + service
				o.ShippingCents = shippingCost
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		var a types.ShopAddress
		e = app.DB.QueryRow(c, `SELECT name,line1,line2,city,region,postal_code,country,phone FROM shop_order_addresses WHERE order_id=$1 AND address_type='shipping'`, o.ID).Scan(&a.Name, &a.Line1, &a.Line2, &a.City, &a.Region, &a.PostalCode, &a.Country, &a.Phone)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return nil, e
		}
		if e == nil {
			o.Address = &a
		}
		tracks, e := app.DB.Query(c, `SELECT DISTINCT s.courier_name||' · '||s.tracking_number FROM shipments s JOIN shipment_items si ON si.shipment_id=s.id JOIN shop_order_items i ON i.id=si.order_item_id WHERE s.order_id=$1 AND i.fulfillment_organization_id=$2`, o.ID, orgID)
		if e != nil {
			return nil, e
		}
		for tracks.Next() {
			var tracking string
			if e = tracks.Scan(&tracking); e != nil {
				tracks.Close()
				return nil, e
			}
			o.Tracking = append(o.Tracking, tracking)
		}
		e = tracks.Err()
		tracks.Close()
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
func ShipOrganizationOrder(app *config.AppContext, orgID, orderID, actor, courier, number, tracking string) error {
	if strings.TrimSpace(courier) == "" || strings.TrimSpace(number) == "" {
		return fmt.Errorf("enter a carrier and tracking number")
	}
	if tracking != "" {
		u, e := url.Parse(tracking)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("tracking link must be a complete HTTPS URL")
		}
	}
	c := app.DatabaseContext()
	tx, e := app.DB.Begin(c)
	if e != nil {
		return e
	}
	defer tx.Rollback(c)
	var status string
	if e = tx.QueryRow(c, `SELECT status FROM shop_orders WHERE id=$1 AND EXISTS(SELECT 1 FROM shop_order_items WHERE order_id=$1 AND coalesce(fulfillment_organization_id::text,'')=$2) FOR UPDATE`, orderID, orgID).Scan(&status); e != nil {
		return fmt.Errorf("order not found in this organization")
	}
	if status != "paid" && status != "partially_refunded" {
		return fmt.Errorf("only paid orders can be shipped")
	}
	rows, e := tx.Query(c, `SELECT id::text,quantity-fulfilled_quantity-refunded_quantity,sku_snapshot,product_name_snapshot FROM shop_order_items WHERE order_id=$1 AND coalesce(fulfillment_organization_id::text,'')=$2 AND fulfillment_method='ship' AND quantity>fulfilled_quantity+refunded_quantity FOR UPDATE`, orderID, orgID)
	if e != nil {
		return e
	}
	type line struct {
		id, sku, name string
		qty           int
	}
	lines := []line{}
	for rows.Next() {
		var x line
		if e = rows.Scan(&x.id, &x.qty, &x.sku, &x.name); e != nil {
			rows.Close()
			return e
		}
		lines = append(lines, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(lines) == 0 {
		return nil
	}
	var shipment string
	if e = tx.QueryRow(c, `INSERT INTO shipments(order_id,provider,courier_name,tracking_number,tracking_url,status,shipped_at) VALUES($1,'manual',$2,$3,$4,'shipped',now()) RETURNING id::text`, orderID, courier, number, tracking).Scan(&shipment); e != nil {
		return e
	}
	for _, item := range lines {
		if _, e = tx.Exec(c, `INSERT INTO shipment_items(shipment_id,order_item_id,quantity,sku,description) VALUES($1,$2,$3,$4,$5)`, shipment, item.id, item.qty, item.sku, item.name); e != nil {
			return e
		}
		if _, e = tx.Exec(c, `UPDATE shop_order_items SET fulfilled_quantity=fulfilled_quantity+$2,status='fulfilled' WHERE id=$1`, item.id, item.qty); e != nil {
			return e
		}
	}
	_, e = tx.Exec(c, `INSERT INTO shop_events(event_type,actor_type,actor_email,entity_type,entity_id,order_id,metadata) VALUES('order.shipped','admin',$2,'shop_order',$1,$1,jsonb_build_object('fulfillment_organization_id',$3::text,'shipment_id',$4::text))`, orderID, actor, orgID, shipment)
	if e != nil {
		return e
	}
	return tx.Commit(c)
}
func ShopOrderShippingOrigin(app *config.AppContext, orderID string) (*types.EasyshipSettings, error) {
	rows, e := app.DB.Query(app.DatabaseContext(), `SELECT DISTINCT fulfillment_organization_id::text,ship_from_snapshot FROM shop_order_items WHERE order_id=$1 AND fulfillment_method='ship'`, orderID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	count := 0
	var origin *types.EasyshipSettings
	for rows.Next() {
		count++
		var org *string
		var raw []byte
		if e = rows.Scan(&org, &raw); e != nil {
			return nil, e
		}
		if org != nil {
			origin = &types.EasyshipSettings{}
			if e = json.Unmarshal(raw, origin); e != nil || !origin.IsConfigured() {
				return nil, fmt.Errorf("saved shipping origin is incomplete")
			}
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if count > 1 {
		return nil, fmt.Errorf("this order requires separate shipments from different origins")
	}
	if origin != nil {
		return origin, nil
	}
	return GetEasyshipSettings(app)
}

// PendingShopTaxCalculations makes retries safe when only some origins have been recorded.
func PendingShopTaxCalculations(app *config.AppContext, orderID string) ([]TaxQuoteInput, error) {
	rows, e := app.DB.Query(app.DatabaseContext(), `SELECT DISTINCT ON(q.origin_key) q.origin_key,q.provider_quote_id,q.sales_tax_amount_cents FROM tax_quotes q JOIN shop_orders o ON o.id=q.order_id WHERE q.order_id=$1 AND o.status IN ('paid','partially_refunded','refunded') AND q.sales_tax_provider='stripe' AND q.provider_quote_id<>'' AND NOT EXISTS(SELECT 1 FROM tax_transactions t WHERE t.order_id=q.order_id AND t.provider='stripe' AND t.status='recorded' AND t.origin_key=q.origin_key) ORDER BY q.origin_key,q.created_at DESC`, orderID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []TaxQuoteInput{}
	for rows.Next() {
		var q TaxQuoteInput
		if e = rows.Scan(&q.OriginKey, &q.ProviderQuoteID, &q.SalesTaxAmountCents); e != nil {
			return nil, e
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

type ShopFulfillmentGroup struct {
	OrganizationID, Name string
	Remaining            int
}

func ShopOrderFulfillmentGroups(app *config.AppContext, orderID string) ([]ShopFulfillmentGroup, error) {
	rows, e := app.DB.Query(app.DatabaseContext(), `SELECT coalesce(i.fulfillment_organization_id::text,''),coalesce(o.name,'bitcoin++'),sum(greatest(i.quantity-i.fulfilled_quantity-i.refunded_quantity,0)) FROM shop_order_items i LEFT JOIN organizations o ON o.id=i.fulfillment_organization_id WHERE i.order_id=$1 AND i.fulfillment_method='ship' GROUP BY i.fulfillment_organization_id,o.name ORDER BY o.name NULLS FIRST`, orderID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ShopFulfillmentGroup{}
	for rows.Next() {
		var g ShopFulfillmentGroup
		if e = rows.Scan(&g.OrganizationID, &g.Name, &g.Remaining); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

type OrganizationEventStock struct {
	Event, Product, Variant string
	Available               int
}

func OrganizationEventInventory(app *config.AppContext, orgID string) ([]OrganizationEventStock, error) {
	rows, e := app.DB.Query(app.DatabaseContext(), `SELECT c.description,p.name,v.label,s.available FROM conference_pos_stock s JOIN conferences c ON c.id=s.conference_id JOIN merch_variants v ON v.id=s.variant_id JOIN merch_products p ON p.id=v.product_id WHERE p.fulfillment_organization_id=$1 AND s.available>0 ORDER BY c.start_date DESC,p.name,v.label`, orgID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []OrganizationEventStock{}
	for rows.Next() {
		var x OrganizationEventStock
		if e = rows.Scan(&x.Event, &x.Product, &x.Variant, &x.Available); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

type OrganizationEventSale struct {
	Event, Product, Variant string
	Quantity                int
	TotalSats               int64
	CreatedAt               time.Time
	HandedOver              bool
}

func OrganizationEventPurchases(app *config.AppContext, orgID string) ([]OrganizationEventSale, error) {
	rows, e := app.DB.Query(app.DatabaseContext(), `SELECT c.description,i.product_name,i.variant_label,i.quantity,i.quantity*i.price_sats,s.created_at,s.handed_over_at IS NOT NULL FROM conference_pos_sale_items i JOIN conference_pos_sales s ON s.id=i.sale_id JOIN conferences c ON c.id=s.conference_id WHERE i.fulfillment_organization_id=$1 AND s.status='paid' ORDER BY s.created_at DESC LIMIT 100`, orgID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []OrganizationEventSale{}
	for rows.Next() {
		var x OrganizationEventSale
		if e = rows.Scan(&x.Event, &x.Product, &x.Variant, &x.Quantity, &x.TotalSats, &x.CreatedAt, &x.HandedOver); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
