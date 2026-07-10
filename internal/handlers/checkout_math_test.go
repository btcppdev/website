package handlers

import (
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/types"
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
