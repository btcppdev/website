package handlers

import (
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/types"
	stripe "github.com/stripe/stripe-go/v76"
)

func TestCardSurchargePrice(t *testing.T) {
	tests := []struct {
		name         string
		basePrice    uint
		surchargeBPS uint
		want         uint
	}{
		{"zero price", 0, 1000, 0},
		{"default surcharge when bps omitted", 100, 0, 110},
		{"ten percent exact", 100, 1000, 110},
		{"ten percent rounds up", 101, 1000, 112},
		{"two point five percent rounds up", 99, 250, 102},
		{"no surcharge explicit", 100, 1, 101},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cardSurchargePrice(tt.basePrice, tt.surchargeBPS); got != tt.want {
				t.Fatalf("cardSurchargePrice(%d, %d) = %d, want %d", tt.basePrice, tt.surchargeBPS, got, tt.want)
			}
		})
	}
}

func TestCheckoutDiscountThenCardSurcharge(t *testing.T) {
	discount := &types.DiscountCode{Discount: "%10"}
	if err := discount.ParseDiscountExpr(); err != nil {
		t.Fatalf("parse discount: %s", err)
	}

	basePrice := uint(125)
	discounted := discount.ApplyDiscount(basePrice)
	if discounted != 112 {
		t.Fatalf("discounted price = %d, want 112", discounted)
	}

	cardPrice := cardSurchargePrice(discounted, 1000)
	if cardPrice != 124 {
		t.Fatalf("card price = %d, want 124", cardPrice)
	}
	if surcharge := cardPrice - discounted; surcharge != 12 {
		t.Fatalf("card surcharge = %d, want 12", surcharge)
	}
}

func TestCheckoutQuantityUsesPerTicketDiscountAndSurcharge(t *testing.T) {
	discount := &types.DiscountCode{Discount: "$15"}
	if err := discount.ParseDiscountExpr(); err != nil {
		t.Fatalf("parse discount: %s", err)
	}

	count := uint(3)
	basePrice := uint(100)
	discountedEach := discount.ApplyDiscount(basePrice)
	cardEach := cardSurchargePrice(discountedEach, 1000)

	if discountedEach != 85 {
		t.Fatalf("discounted each = %d, want 85", discountedEach)
	}
	if cardEach != 94 {
		t.Fatalf("card each = %d, want 94", cardEach)
	}
	if total := cardEach * count; total != 282 {
		t.Fatalf("card total = %d, want 282", total)
	}
}

func TestStripePerTicketAmount(t *testing.T) {
	tests := []struct {
		name      string
		lineTotal int64
		quantity  int64
		want      []int64
	}{
		{"single", 11000, 1, []int64{11000}},
		{"even split", 33000, 3, []int64{11000, 11000, 11000}},
		{"distributes cents", 10000, 3, []int64{3334, 3333, 3333}},
		{"zero quantity fallback", 12345, 0, []int64{12345}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, want := range tt.want {
				if got := stripePerTicketAmount(tt.lineTotal, tt.quantity, int64(i)); got != want {
					t.Fatalf("stripePerTicketAmount(%d, %d, %d) = %d, want %d", tt.lineTotal, tt.quantity, i, got, want)
				}
			}
		})
	}
}

func TestStripeTicketItemsExcludeMixedCheckoutNonTicketLines(t *testing.T) {
	lines := []*stripe.LineItem{
		{
			Description: "DEV26 ticket", Quantity: 1, AmountTotal: 12500,
			Price: &stripe.Price{Product: &stripe.Product{Metadata: map[string]string{"line-kind": "ticket"}}},
		},
		{
			Description: "bitcoin++ hat", Quantity: 3, AmountTotal: 10500,
			Price: &stripe.Price{Product: &stripe.Product{Metadata: map[string]string{"line-kind": "merch"}}},
		},
		{
			Description: "Sales tax", Quantity: 1, AmountTotal: 866,
			Price: &stripe.Price{Product: &stripe.Product{Metadata: map[string]string{"line-kind": "tax"}}},
		},
	}

	items, total := stripeTicketItems(lines, types.TicketTypeGeneral)
	if len(items) != 1 {
		t.Fatalf("ticket count = %d, want 1", len(items))
	}
	if total != 12500 || items[0].Total != 12500 {
		t.Fatalf("ticket totals = (%d, %d), want 12500", total, items[0].Total)
	}
}

func TestTicketStripeTaxCodeDefaultsToNontaxable(t *testing.T) {
	if got := ticketStripeTaxCode(nil); got != types.StripeTaxCodeNontaxable {
		t.Fatalf("nil ticket tax code = %q, want %q", got, types.StripeTaxCodeNontaxable)
	}
	if got := ticketStripeTaxCode(&types.ConfTicket{}); got != types.StripeTaxCodeNontaxable {
		t.Fatalf("empty ticket tax code = %q, want %q", got, types.StripeTaxCodeNontaxable)
	}
	if got := ticketStripeTaxCode(&types.ConfTicket{StripeTaxCode: "txcd_custom"}); got != "txcd_custom" {
		t.Fatalf("custom ticket tax code = %q", got)
	}
}

func TestMerchMoneyFormatsCents(t *testing.T) {
	tests := []struct {
		amount uint
		want   string
	}{
		{0, "$0"},
		{999, "$9.99"},
		{3500, "$35"},
		{3550, "$35.50"},
	}
	for _, tt := range tests {
		if got := merchMoney(tt.amount, nil); got != tt.want {
			t.Fatalf("merchMoney(%d) = %q, want %q", tt.amount, got, tt.want)
		}
	}
}

func TestConfTicketCanonicalPrices(t *testing.T) {
	ticket := &types.ConfTicket{
		BasePrice:        140,
		BTC:              99,
		USD:              88,
		CardSurchargeBPS: 1000,
	}

	if got := ticket.StandardPrice(); got != 140 {
		t.Fatalf("StandardPrice() = %d, want 140", got)
	}
	if got := ticket.CardPrice(false); got != 154 {
		t.Fatalf("CardPrice(false) = %d, want 154", got)
	}
	if got := ticket.CardSurcharge(false); got != 14 {
		t.Fatalf("CardSurcharge(false) = %d, want 14", got)
	}
}

func TestValidateCheckoutDiscountPriceWithoutCode(t *testing.T) {
	ref, price, err := validateCheckoutDiscountPrice(nil, &types.Conf{Ref: "conf"}, 125, "", 125)
	if err != nil {
		t.Fatalf("validateCheckoutDiscountPrice exact price error: %s", err)
	}
	if ref != "" || price != 125 {
		t.Fatalf("validateCheckoutDiscountPrice exact = (%q, %d), want empty ref and 125", ref, price)
	}

	_, price, err = validateCheckoutDiscountPrice(nil, &types.Conf{Ref: "conf"}, 125, "", 100)
	if err == nil {
		t.Fatalf("validateCheckoutDiscountPrice stale price returned nil error")
	}
	if price != 125 {
		t.Fatalf("validateCheckoutDiscountPrice stale price = %d, want 125", price)
	}
}

func TestDetermineTixKind(t *testing.T) {
	tests := []struct {
		name     string
		slug     string
		wantID   string
		wantKind string
		wantErr  bool
	}{
		{"standard", "abc123", "abc123", types.TicketTypeGeneral, false},
		{"local", "abc123+local", "abc123", types.TicketTypeLocal, false},
		{"sponsor alias", "abc123+sponsor", "abc123", types.TicketTypeSponsored, false},
		{"sponsored", "abc123+sponsored", "abc123", types.TicketTypeSponsored, false},
		{"unknown suffix", "abc123+vip", "", "", true},
		{"too many parts", "abc123+local+extra", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotKind, err := determineTixKind(tt.slug)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("determineTixKind(%q) expected error", tt.slug)
				}
				return
			}
			if err != nil {
				t.Fatalf("determineTixKind(%q) unexpected error: %s", tt.slug, err)
			}
			if gotID != tt.wantID || gotKind != tt.wantKind {
				t.Fatalf("determineTixKind(%q) = (%q, %q), want (%q, %q)", tt.slug, gotID, gotKind, tt.wantID, tt.wantKind)
			}
		})
	}
}

func TestCheckoutDefaultPaymentMethod(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"default", "/tix/abc/checkout", "btc"},
		{"payment card", "/tix/abc/checkout?payment=card", "card"},
		{"payment fiat alias", "/tix/abc/checkout?payment=fiat", "card"},
		{"pay card", "/tix/abc/checkout?pay=card", "card"},
		{"unknown", "/tix/abc/checkout?payment=cash", "btc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if got := checkoutDefaultPaymentMethod(req); got != tt.want {
				t.Fatalf("checkoutDefaultPaymentMethod(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
