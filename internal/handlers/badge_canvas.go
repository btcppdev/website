package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"fmt"
	"net/http"
	"strings"
)

func (line shopCartLine) Key() string { return canvasCartKey(line.VariantID, line.BadgeReference) }
func canvasCartKey(variantID, grantID string) string {
	if grantID != "" {
		return variantID + ":" + grantID
	}
	return variantID
}
func (item *shopCartItem) Key() string {
	if item.BadgeCanvas != nil {
		return canvasCartKey(item.Variant.ID, item.BadgeCanvas.Reference)
	}
	return item.Variant.ID
}
func (item *shopCartItem) Image() string {
	if item.BadgeCanvas != nil {
		return item.BadgeCanvas.ArtworkURL
	}
	return merchImage(item.Product)
}
func (item *shopCartItem) Label() string {
	if item.BadgeCanvas != nil {
		return item.Variant.Label + " · " + item.BadgeCanvas.BadgeName
	}
	return item.Variant.Label
}

func loadCanvasSelection(ctx *config.AppContext, r *http.Request, product *types.MerchProduct, grantID string) (*types.BadgeCanvas, error) {
	if product.ProductType != types.MerchProductTypeBadgeCanvas {
		if grantID != "" {
			return nil, fmt.Errorf("This product does not accept a badge design.")
		}
		return nil, nil
	}
	if product.Status != types.MerchProductStatusPublished {
		return nil, fmt.Errorf("This canvas is not available yet.")
	}
	identity, err := auth.Resolve(r, ctx)
	if err != nil || identity == nil || identity.PersonID == "" {
		return nil, fmt.Errorf("Sign in to choose a badge issued to your account.")
	}
	badges, err := canvasBadgesForRequest(ctx, r, identity.PersonID)
	if err != nil {
		return nil, fmt.Errorf("Unable to load your issued badges.")
	}
	for _, badge := range badges {
		if badge.Reference == grantID {
			return badge, nil
		}
	}
	return nil, fmt.Errorf("Choose one of your issued badges. Pending or revoked badges cannot be printed.")
}

func prepareCanvasProduct(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, page *shopPage) bool {
	if page.Product.ProductType != types.MerchProductTypeBadgeCanvas {
		return true
	}
	w.Header().Set("Cache-Control", "private, no-store")
	identity, err := auth.Resolve(r, ctx)
	if err != nil {
		http.Error(w, "Unable to load your account", 500)
		return false
	}
	if identity != nil && identity.PersonID != "" {
		page.CanvasSignedIn = true
		page.CanvasBadges, err = canvasBadgesForRequest(ctx, r, identity.PersonID)
		if err != nil {
			http.Error(w, "Unable to load your badges", 500)
			return false
		}
		page.CanvasCSRF, err = ensureAuthMethodsCSRF(ctx, r)
		if err != nil {
			http.Error(w, "Unable to prepare the badge selector", 500)
			return false
		}
	}
	selected := r.URL.Query().Get("badge")
	for _, badge := range page.CanvasBadges {
		if badge.Reference == selected {
			page.SelectedCanvasBadge = badge
		}
	}
	if page.SelectedCanvasBadge == nil && len(page.CanvasBadges) > 0 {
		page.SelectedCanvasBadge = page.CanvasBadges[0]
	}
	return true
}

func canvasLineFromKey(key string, qty uint) shopCartLine {
	variant, grant, _ := strings.Cut(key, ":")
	return shopCartLine{VariantID: variant, BadgeReference: grant, Qty: qty}
}

// Reuse the live ownership lookup within one request, never across checkouts.
type canvasRequestKey struct{}
type canvasRequestBadges struct {
	loaded bool
	badges []*types.BadgeCanvas
	err    error
}

func canvasBadgesForRequest(ctx *config.AppContext, r *http.Request, personID string) ([]*types.BadgeCanvas, error) {
	memo, _ := r.Context().Value(canvasRequestKey{}).(*canvasRequestBadges)
	if memo == nil {
		return getters.ListPersonCanvasBadges(ctx, personID)
	}
	if !memo.loaded {
		memo.badges, memo.err = getters.ListPersonCanvasBadges(ctx, personID)
		memo.loaded = true
	}
	return memo.badges, memo.err
}
func canvasCartRequest(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), canvasRequestKey{}, &canvasRequestBadges{}))
}
