package getters

import (
	"btcpp-web/internal/types"
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"sync"
	"testing"
)

func TestOrganizationShopMixedOrderLifecycle(t *testing.T) {
	app := postgresSmokeContext(t)
	c := context.Background()
	suffix := postgresSmokeSuffix()
	email := "shops-" + suffix + "@example.test"
	orgs := []string{uuid.NewString(), uuid.NewString()}
	products := []string{}
	variants := []string{}
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM shop_orders WHERE buyer_email=$1`, email)
		app.DB.Exec(c, `DELETE FROM merch_products WHERE id::text=ANY($1)`, products)
		app.DB.Exec(c, `DELETE FROM organization_shop_discounts WHERE organization_id::text=ANY($1)`, orgs)
		app.DB.Exec(c, `DELETE FROM organization_shop_profiles WHERE organization_id::text=ANY($1)`, orgs)
		app.DB.Exec(c, `DELETE FROM organizations WHERE id::text=ANY($1)`, orgs)
	})
	profile := types.OrganizationShopProfile{BillingName: "Example company", Billing: types.ShopAddress{Line1: "1 Test St", City: "Berlin", PostalCode: "10115", Country: "DE"}, ShipFrom: types.EasyshipSettings{ContactName: "Test", CompanyName: "Example", Email: email, Phone: "+4930123456", Line1: "1 Test St", City: "Berlin", Region: "Berlin", PostalCode: "10115", CountryAlpha2: "DE"}}
	for i, org := range orgs {
		if _, e := app.DB.Exec(c, `INSERT INTO organizations(id,name) VALUES($1,$2)`, org, "Test shop "+org); e != nil {
			t.Fatal(e)
		}
		rev, e := SaveOrganizationShopProfile(app, org, "", profile)
		if e != nil || rev != 1 {
			t.Fatalf("profile %d %v", rev, e)
		}
		if _, e = SaveOrganizationShopProfile(app, org, "", profile); !errors.Is(e, ErrShopEditConflict) {
			t.Fatalf("stale profile %v", e)
		}
		in := OrganizationProductInput{MerchProductInput: MerchProductInput{Slug: "shop-" + org, Name: "Shop product", Status: "published", ProductType: "apparel", BasePriceCents: 2500, Currency: "USD", StripeTaxCode: types.StripeTaxCodeTangibleGood, EasyshipCategory: "fashion", CountryOfOrigin: "US"}, OnlineEnabled: true}
		id, e := SaveOrganizationProduct(app, org, in)
		if e != nil {
			t.Fatal(e)
		}
		products = append(products, id)
		variant := MerchVariantInput{SKU: org, Label: "Medium", WeightGrams: 200, LengthMM: 200, WidthMM: 200, HeightMM: 50, Status: "active"}
		v, e := SaveOrganizationVariant(app, org, id, "", email, uuid.NewString(), variant, 10)
		if e != nil {
			t.Fatal(e)
		}
		variants = append(variants, v)
		op := uuid.NewString()
		for range 2 {
			if _, e = SaveOrganizationVariant(app, org, id, v, email, op, variant, 2); e != nil {
				t.Fatal(e)
			}
		}
		got, _, e := GetMerchVariant(app, v)
		if e != nil || got.Stock != 12 {
			t.Fatalf("idempotent stock %v %v", got, e)
		}
		if _, e = SaveOrganizationVariant(app, org, id, v, email, uuid.NewString(), variant, -13); e == nil {
			t.Fatal("negative stock")
		}
		if _, e = SaveOrganizationVariant(app, orgs[1-i], id, v, email, uuid.NewString(), variant, 100); e == nil {
			t.Fatal("cross-org stock write")
		}
	}
	code := "SHOP-" + strings.ToUpper(suffix)
	if e := SaveOrganizationDiscount(app, orgs[0], types.OrganizationShopDiscount{Code: code, PercentOff: 10, Active: true, MaxUses: 1}); e != nil {
		t.Fatal(e)
	}
	items := []ShopOrderItemInput{}
	for i := range products {
		discount := uint(0)
		if i == 0 {
			discount = 250
		}
		items = append(items, ShopOrderItemInput{ProductID: products[i], VariantID: variants[i], Quantity: 1, UnitPriceCents: 2500, DiscountAmountCents: discount, LineTotalCents: 2500 - discount, ProductNameSnapshot: "Shop product", VariantLabelSnapshot: "Medium", SKUSnapshot: orgs[i], FulfillmentMethod: types.ShopFulfillmentShip})
	}
	input := ShopOrderInput{BuyerEmail: email, BuyerName: "Test buyer", Source: types.ShopOrderSourceOnline, CheckoutKind: types.ShopCheckoutKindMerch, PaymentProvider: "stripe", Currency: "USD", SubtotalCents: 5000, DiscountAmountCents: 250, DiscountCode: code, TotalCents: 4750, ShopOriginRevisions: map[string]int64{orgs[0]: 1, orgs[1]: 1}, ShippingAddress: &types.ShopAddress{Name: "Test buyer", Line1: "2 Example St", City: "Berlin", Region: "Berlin", PostalCode: "10115", Country: "DE", Phone: "+4930123456"}}
	tampered := append([]ShopOrderItemInput(nil), items...)
	tampered[1].DiscountAmountCents = 250
	tampered[1].LineTotalCents = 2250
	if _, e := CreateShopOrder(app, input, tampered); e == nil {
		t.Fatal("discount leaked across organizations")
	}
	badRevision := input
	badRevision.ShopOriginRevisions = map[string]int64{orgs[0]: 0, orgs[1]: 1}
	if _, e := CreateShopOrder(app, badRevision, items); e == nil {
		t.Fatal("stale origin quote")
	}
	// Only one concurrent checkout may reserve the final discount use.
	var wg sync.WaitGroup
	orders := make(chan *types.ShopOrder, 2)
	for range 2 {
		wg.Go(func() {
			o, e := CreateShopOrder(app, input, items)
			if e == nil {
				orders <- o
			}
		})
	}
	wg.Wait()
	close(orders)
	var order *types.ShopOrder
	count := 0
	for o := range orders {
		count++
		order = o
	}
	if count != 1 {
		t.Fatalf("discount reservations=%d", count)
	}
	if e := ShipOrganizationOrder(app, orgs[0], order.ID, email, "Carrier", "TEST123", ""); e == nil {
		t.Fatal("unpaid order shipped")
	}
	if e := CancelShopOrder(app, order.ID, email, "test cancellation"); e != nil {
		t.Fatal(e)
	}
	order, e := CreateShopOrder(app, input, items)
	if e != nil {
		t.Fatalf("cancelled discount not released %v", e)
	}
	for _, v := range variants {
		got, _, e := GetMerchVariant(app, v)
		if e != nil || got.Stock != 11 {
			t.Fatalf("stock reservation %v %v", got, e)
		}
	}
	profile.Revision = 1
	profile.ShipFrom.Line1 = "99 New Street"
	if _, e = SaveOrganizationShopProfile(app, orgs[0], "", profile); e != nil {
		t.Fatal(e)
	}
	var saved string
	if e = app.DB.QueryRow(c, `SELECT ship_from_snapshot->>'Line1' FROM shop_order_items WHERE order_id=$1 AND fulfillment_organization_id=$2`, order.ID, orgs[0]).Scan(&saved); e != nil || saved != "1 Test St" {
		t.Fatalf("mutable origin %q %v", saved, e)
	}
	if _, e = MarkShopOrderPaid(app, order.ID, "stripe", "cs_shop_"+suffix, 0, 4750); e != nil {
		t.Fatal(e)
	}
	for i, org := range orgs {
		var bps int
		var fee, supplier uint
		err := app.DB.QueryRow(c, `SELECT organization_fee_bps,organization_fee_cents,supplier_amount_cents FROM shop_order_items WHERE order_id=$1 AND fulfillment_organization_id=$2`, order.ID, org).Scan(&bps, &fee, &supplier)
		wantFee, wantSupplier := uint(375), uint(2125)
		if i == 0 {
			wantFee, wantSupplier = 338, 1912
		}
		if err != nil || bps != 1500 || fee != wantFee || supplier != wantSupplier {
			t.Fatalf("saved supplier split: %d %d %d %v", bps, fee, supplier, err)
		}
	}
	for _, org := range orgs {
		owned, e := ListOrganizationShopOrders(app, org)
		if e != nil || len(owned) != 1 || len(owned[0].Items) != 1 {
			t.Fatalf("scoped purchases %v %v", owned, e)
		}
	}
	for range 2 {
		if e = ShipOrganizationOrder(app, orgs[0], order.ID, email, "Carrier", "TEST123", "https://example.test/tracking"); e != nil {
			t.Fatal(e)
		}
	}
	loaded, e := GetShopOrderByID(app, order.ID)
	if e != nil || loaded.UnfulfilledShippingQuantity != 1 || len(loaded.Shipments) != 1 {
		t.Fatalf("partial fulfillment %+v %v", loaded, e)
	}
	second, e := ListOrganizationShopOrders(app, orgs[1])
	if e != nil || !second[0].CanShip || len(second[0].Tracking) != 0 {
		t.Fatalf("other organization affected %v", e)
	}
	if e = ShipOrganizationOrder(app, uuid.NewString(), order.ID, email, "Carrier", "TEST", ""); e == nil {
		t.Fatal("foreign order shipped")
	}
	// Record two tax calculations independently and retry only the unfinished origin.
	for _, org := range orgs {
		if e = CreateTaxQuote(app, TaxQuoteInput{OrderID: order.ID, ProviderQuoteID: "taxcalc_" + org, OriginKey: org, SalesTaxAmountCents: 100}); e != nil {
			t.Fatal(e)
		}
	}
	pending, e := PendingShopTaxCalculations(app, order.ID)
	if e != nil || len(pending) != 2 {
		t.Fatalf("tax groups %d %v", len(pending), e)
	}
	if e = RecordShopTaxTransaction(app, order.ID, "tax_"+orgs[0], 100, "{}", orgs[0]); e != nil {
		t.Fatal(e)
	}
	pending, e = PendingShopTaxCalculations(app, order.ID)
	if e != nil || len(pending) != 1 || pending[0].OriginKey != orgs[1] {
		t.Fatalf("partial tax retry %+v %v", pending, e)
	}
	conf, _ := insertSmokeConference(t, app)
	if e = POSConfigure(app, conf, variants[0], email, uuid.NewString(), true, 1000, 2); e == nil {
		t.Fatal("allocated ineligible event stock")
	}
	for i, p := range products {
		if _, e = app.DB.Exec(c, `INSERT INTO merch_product_events(product_id,conference_id) VALUES($1,$2)`, p, conf); e != nil {
			t.Fatal(e)
		}
		if e = POSConfigure(app, conf, variants[i], email, uuid.NewString(), true, 1000, 2); e != nil {
			t.Fatal(e)
		}
	}
	if e = POSSetEnabled(app, conf, email, true); e != nil {
		t.Fatal(e)
	}
	sale, _, e := POSCreateSale(app, conf, "register", uuid.NewString(), "USD", 80000, []types.POSCartLine{{VariantID: variants[0], Quantity: 1, PriceSats: 1000}, {VariantID: variants[1], Quantity: 1, PriceSats: 1000}})
	if e != nil {
		t.Fatal(e)
	}
	var snapshotted int
	if err := app.DB.QueryRow(c, `SELECT count(*) FROM conference_pos_sale_items WHERE sale_id=$1 AND organization_fee_bps=1500 AND organization_fee_sats=150 AND supplier_amount_sats=850`, sale.ID).Scan(&snapshotted); err != nil || snapshotted != 2 {
		t.Fatalf("event supplier splits: %d %v", snapshotted, err)
	}
	// Catalog edits cannot alter the agreed amounts on existing sales.
	if _, err := app.DB.Exec(c, `UPDATE merch_products SET base_price_cents=9900 WHERE id=$1`, products[0]); err != nil {
		t.Fatal(err)
	}
	var retained uint
	if err := app.DB.QueryRow(c, `SELECT supplier_amount_cents FROM shop_order_items WHERE order_id=$1 AND fulfillment_organization_id=$2`, order.ID, orgs[0]).Scan(&retained); err != nil || retained != 1912 {
		t.Fatalf("mutable supplier purchase amount: %d %v", retained, err)
	}
	if e = POSApplyPayment(app, sale.ID, "charge-"+sale.ID, "paid", 2000); e != nil {
		t.Fatal(e)
	}
	for _, org := range orgs {
		purchases, e := OrganizationEventPurchases(app, org)
		if e != nil || len(purchases) != 1 || purchases[0].TotalSats != 1000 {
			t.Fatalf("scoped event purchases %+v %v", purchases, e)
		}
		inventory, e := OrganizationEventInventory(app, org)
		if e != nil || len(inventory) != 1 || inventory[0].Available != 1 {
			t.Fatalf("event inventory %+v %v", inventory, e)
		}
	}

}
