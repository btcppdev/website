# Badge canvas shop

Migration 103 creates a draft `/shop/badge-canvas` product and a 9×9-inch
variant, priced at $65 USD, with shipping and event pickup enabled. Pickup uses
the shop's eligible upcoming event. Shipping requires actual packed weight and
dimensions; the seed deliberately leaves them unset. Configure those and the
manual printing workflow before publishing the product.

All merchandise product pages, catalog cards, and cart/checkout totals lead with an estimated satoshi price
using the existing CoinGecko rate service, rounded to the nearest 1,000 sats
and displayed as `70k sats`. Successful display quotes are cached
for five minutes; checkout bypasses that cache and requests a fresh rate.
Cart entries store item IDs and quantities, so checkout reloads current product
prices and variant adjustments. Failed lookups
fall back to USD and retry after a minute. Shipping and tax changes also update the displayed satoshi total in checkout.
USD remains the checkout accounting
currency, and the payment provider quotes the final bitcoin invoice amount.
Shipping and tax are additional.

Signed-in customers can select any current issued badge returned by Badge
Studio for their Bitcoin++ person ID, including independent issuers. Public
profile visibility rules are unchanged. Pending and revoked awards are excluded.
When `BADGE_STUDIO_URL` is configured, a failed Studio lookup blocks canvas
selection and checkout; local grants cannot resurrect a remotely revoked award.
Without Studio configured, locally issued/accepted organization grants are used.

The cart distinguishes designs even when they share the same merchandise SKU.
Ownership is checked when adding to the cart and again when creating the order.
Each order item stores its badge name, artwork URL, issuer, identifier and award
event ID. Receipts and admin fulfillment show the selected design. Artwork URL
metadata is retained; image bytes are not archived, so the remote artwork can
still change. Printing is a manual fulfillment step through the existing merch
order workflow; this change does not submit orders to a print provider.

## Local preview

With a disposable database migrated through 103:

```sh
CANVAS_BROWSER_PREVIEW=1 BTCPP_POSTGRES_SMOKE=1 DATABASE_URL='postgres://…' \
  go test ./internal/handlers -run '^TestBadgeCanvasShopFlow$' -v -timeout 0
```

Open http://127.0.0.1:8096/preview. The fixture uses synthetic badges, the $65 USD
base price, a fixed test exchange rate and sample parcel dimensions. Payment submission routes are disabled.
The fixture includes an independent issuer and a local mock of Badge Studio.

Run `go test ./...`, and with the disposable database run:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL='postgres://…' go test \
  ./external/getters ./internal/handlers \
  -run 'TestBadgeCanvas|TestCanvas|TestDatabaseSmokeShop|TestDatabaseSmokeMerch'
```

## Storefront merchandising

The home page features the available badge canvas, then established hat tags
(`core-hat`, `libbit-hat`, `bpp-hat`) as fallbacks. Without an available featured
product it shows a general shop introduction. The first grid includes up to six
products and gives each collection a place before repeating a category.

Category tabs combine stickers, patches and pins, preserve category counts when
filtering, and retain older `?cat=pins` links. Category copy and the featured-tag
priority live in `internal/handlers/shop_merchandising.go`. Attendee exclusives
remain a separate collection. No new admin configuration is required.

A signed-in customer with an issued badge sees their art in the canvas feature
and a link preselecting it. Personalized home responses are private/no-store;
Studio lookup failures fall back to the generic feature. Product recommendations
use complementary category priorities, skip unavailable products, exclude other
badge canvases on canvas pages, and never exceed four. These are editorial rules,
not sales-ranked or purchase-history recommendations.

## Admin editor

The product editor has section links, a separate pricing area, collapsed advanced
settings and expandable variant rows. Base prices and variant adjustments use
exact decimal currency inputs; cents-only requests remain compatible. Invalid
fractional cents and out-of-range prices are rejected. The browser shows a USD
satoshi preview and warns about unsaved form changes. Existing canvas behavior
is preserved when editing its category; it remains a personalized Prints item.

The local fixture also serves `/preview/admin` and `/preview/admin/new` for
visual review. These pages have no save routes; they do not modify real products.
