package handlers

import (
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestMerchSEODescription(t *testing.T) {
	product := &types.MerchProduct{
		Name:        "Hat",
		Subtitle:    "  A concise\nproduct subtitle.  ",
		Description: "A longer description that should not be used.",
	}
	if got, want := merchSEODescription(product), "A concise product subtitle."; got != want {
		t.Fatalf("merchSEODescription() = %q, want %q", got, want)
	}

	product.Subtitle = ""
	product.Description = strings.Repeat("description ", 30)
	got := merchSEODescription(product)
	if len([]rune(got)) > 200 || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated merchSEODescription() = %q", got)
	}
}

func TestShopSEOImageFallsBackWithoutFeaturedProduct(t *testing.T) {
	if got, want := shopSEOImage(nil), "/static/img/rebrand/breakthroughs.jpg"; got != want {
		t.Fatalf("shopSEOImage(nil) = %q, want %q", got, want)
	}
}
