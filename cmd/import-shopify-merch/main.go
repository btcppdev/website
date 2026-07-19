package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/spaces"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

type shopifyResponse struct {
	Products []shopifyProduct `json:"products"`
}

type shopifyProduct struct {
	ID          int64            `json:"id"`
	Title       string           `json:"title"`
	Handle      string           `json:"handle"`
	BodyHTML    string           `json:"body_html"`
	PublishedAt string           `json:"published_at"`
	Vendor      string           `json:"vendor"`
	ProductType string           `json:"product_type"`
	Tags        []string         `json:"tags"`
	Variants    []shopifyVariant `json:"variants"`
	Images      []shopifyImage   `json:"images"`
}

type shopifyVariant struct {
	ID               int64   `json:"id"`
	Title            string  `json:"title"`
	SKU              *string `json:"sku"`
	RequiresShipping bool    `json:"requires_shipping"`
	Taxable          bool    `json:"taxable"`
	Available        bool    `json:"available"`
	Price            string  `json:"price"`
	Grams            int     `json:"grams"`
	Position         int     `json:"position"`
}

type shopifyImage struct {
	ID       int64  `json:"id"`
	Position int    `json:"position"`
	Src      string `json:"src"`
}

var tagRe = regexp.MustCompile(`[^a-z0-9]+`)
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	source := flag.String("source", "https://shop.btcpp.dev/products.json", "Shopify products.json URL")
	limit := flag.Int("limit", 250, "products per Shopify page")
	availableStock := flag.Int("available-stock-default", 10, "initial stock to add for available variants without previous Shopify import stock event")
	status := flag.String("status", "", "force product status: draft, published, or archived")
	uploadImages := flag.Bool("upload-images", false, "download Shopify images, upload originals and AVIF derivatives to DigitalOcean Spaces, and store Spaces keys")
	dryRun := flag.Bool("dry-run", false, "fetch and print import plan without writing")
	flag.Parse()

	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	products, err := fetchAll(ctx, *source, *limit)
	if err != nil {
		log.Fatal(err)
	}
	if *dryRun {
		printPlan(products, *availableStock, *status, *uploadImages)
		return
	}
	if *uploadImages {
		spaces.Init(env.Spaces)
		if !spaces.IsConfigured() {
			log.Fatal("spaces is not configured; set SPACES_ENDPOINT, SPACES_REGION, SPACES_BUCKET, SPACES_KEY, and SPACES_SECRET or omit -upload-images")
		}
	}
	pool, err := db.Open(ctx, env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	counts, err := importProducts(ctx, tx, products, *availableStock, *status, *uploadImages)
	if err != nil {
		log.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}
	log.Printf("imported %d products, %d variants, %d images", counts.products, counts.variants, counts.images)
}

type importCounts struct {
	products int
	variants int
	images   int
}

func fetchAll(ctx context.Context, source string, limit int) ([]shopifyProduct, error) {
	if limit <= 0 || limit > 250 {
		limit = 250
	}
	var out []shopifyProduct
	client := &http.Client{Timeout: 15 * time.Second}
	for page := 1; ; page++ {
		url := source
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url = fmt.Sprintf("%s%slimit=%d&page=%d", url, sep, limit, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", url, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch %s: status %d: %s", url, resp.StatusCode, string(body))
		}
		var payload shopifyResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode %s: %w", url, err)
		}
		if len(payload.Products) == 0 {
			break
		}
		out = append(out, payload.Products...)
		if len(payload.Products) < limit {
			break
		}
	}
	return out, nil
}

func printPlan(products []shopifyProduct, availableStock int, forcedStatus string, uploadImages bool) {
	for _, p := range products {
		status := productStatus(p, forcedStatus)
		fmt.Printf("%s (%s) status=%s variants=%d images=%d upload-images=%t\n", p.Title, p.Handle, status, len(p.Variants), len(p.Images), uploadImages)
		for _, v := range p.Variants {
			fmt.Printf("  - %s sku=%s price=%d available=%t stock-default=%d\n", variantLabel(v), variantSKU(p, v), cents(v.Price), v.Available, availableStock)
		}
	}
}

func importProducts(ctx context.Context, tx pgx.Tx, products []shopifyProduct, availableStock int, forcedStatus string, uploadImages bool) (importCounts, error) {
	var counts importCounts
	for _, p := range products {
		if strings.TrimSpace(p.Handle) == "" || strings.TrimSpace(p.Title) == "" {
			continue
		}
		productID, err := upsertProduct(ctx, tx, p, forcedStatus)
		if err != nil {
			return counts, err
		}
		counts.products++
		base := basePrice(p)
		for _, v := range p.Variants {
			if err := upsertVariant(ctx, tx, productID, p, v, base, availableStock); err != nil {
				return counts, err
			}
			counts.variants++
		}
		for i, img := range p.Images {
			if strings.TrimSpace(img.Src) == "" || i >= 6 {
				continue
			}
			objectKey := strings.TrimSpace(img.Src)
			if uploadImages {
				key, err := mirrorShopifyImage(ctx, p.Handle, img)
				if err != nil {
					return counts, err
				}
				objectKey = key
			}
			if err := upsertImage(ctx, tx, productID, p.Title, objectKey, img.Position, i == 0); err != nil {
				return counts, err
			}
			counts.images++
		}
	}
	return counts, nil
}

func upsertProduct(ctx context.Context, tx pgx.Tx, p shopifyProduct, forcedStatus string) (string, error) {
	desc := stripHTML(p.BodyHTML)
	subtitle := desc
	if len(subtitle) > 140 {
		subtitle = subtitle[:140]
	}
	productType := strings.TrimSpace(p.ProductType)
	if productType == "" {
		productType = inferProductType(p)
	}
	var productID string
	err := tx.QueryRow(ctx, `
		INSERT INTO merch_products (
			tag, slug, name, subtitle, description, status, product_type,
			base_price_cents, currency, symbol, stripe_tax_code, easyship_category,
			country_of_origin, requires_shipping, allow_event_pickup
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, 'USD', '$', $9, $10, 'US', $11, true
		)
		ON CONFLICT (tag) DO UPDATE SET
			slug = EXCLUDED.slug,
			name = EXCLUDED.name,
			subtitle = EXCLUDED.subtitle,
			description = EXCLUDED.description,
			status = EXCLUDED.status,
			product_type = EXCLUDED.product_type,
			base_price_cents = EXCLUDED.base_price_cents,
			stripe_tax_code = EXCLUDED.stripe_tax_code,
			easyship_category = EXCLUDED.easyship_category,
			requires_shipping = EXCLUDED.requires_shipping,
			allow_event_pickup = EXCLUDED.allow_event_pickup
		RETURNING id::text
	`, normalizeSlug(p.Handle), normalizeSlug(p.Handle), strings.TrimSpace(p.Title), subtitle, desc,
		productStatus(p, forcedStatus), productType, basePrice(p), stripeTaxCode(p), easyshipCategory(productType), requiresShipping(p)).Scan(&productID)
	if err != nil {
		return "", fmt.Errorf("upsert product %s: %w", p.Handle, err)
	}
	return productID, nil
}

func upsertVariant(ctx context.Context, tx pgx.Tx, productID string, p shopifyProduct, v shopifyVariant, base int, availableStock int) error {
	sku := variantSKU(p, v)
	price := cents(v.Price)
	delta := price - base
	if delta < 0 {
		delta = 0
	}
	variantStatus := "inactive"
	if v.Available {
		variantStatus = "active"
	}
	var variantID string
	err := tx.QueryRow(ctx, `
		INSERT INTO merch_variants (
			product_id, sku, label, price_delta_cents, weight_grams, inventory_policy, status
		) VALUES (
			$1::uuid, $2, $3, $4, $5, 'deny', $6
		)
		ON CONFLICT (sku) DO UPDATE SET
			product_id = EXCLUDED.product_id,
			label = EXCLUDED.label,
			price_delta_cents = EXCLUDED.price_delta_cents,
			weight_grams = EXCLUDED.weight_grams,
			inventory_policy = EXCLUDED.inventory_policy,
			status = EXCLUDED.status
		RETURNING id::text
	`, productID, sku, variantLabel(v), delta, v.Grams, variantStatus).Scan(&variantID)
	if err != nil {
		return fmt.Errorf("upsert variant %s: %w", sku, err)
	}
	if v.Available && availableStock > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO merch_inventory_events (
				variant_id, event_type, quantity_delta, actor_email, notes
			)
			SELECT $1::uuid, 'initial', $2, 'dev-admin@example.test', 'shopify import default stock'
			WHERE NOT EXISTS (
				SELECT 1 FROM merch_inventory_events
				WHERE variant_id = $1::uuid
					AND event_type = 'initial'
					AND notes = 'shopify import default stock'
			)
		`, variantID, availableStock)
		if err != nil {
			return fmt.Errorf("seed variant stock %s: %w", sku, err)
		}
	}
	return nil
}

func upsertImage(ctx context.Context, tx pgx.Tx, productID string, title string, objectKey string, position int, primary bool) error {
	if primary {
		if _, err := tx.Exec(ctx, `
			UPDATE merch_product_images
			SET is_primary = false
			WHERE product_id = $1::uuid
		`, productID); err != nil {
			return fmt.Errorf("clear primary image %s: %w", productID, err)
		}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO merch_product_images (
			product_id, object_key, alt_text, display_order, is_primary
		) VALUES (
			$1::uuid, $2, $3, $4, $5
		)
		ON CONFLICT (product_id, object_key) DO UPDATE SET
			alt_text = EXCLUDED.alt_text,
			display_order = EXCLUDED.display_order,
			is_primary = EXCLUDED.is_primary
	`, productID, objectKey, title, position, primary)
	if err != nil {
		return fmt.Errorf("upsert image %s: %w", objectKey, err)
	}
	return nil
}

func mirrorShopifyImage(ctx context.Context, handle string, img shopifyImage) (string, error) {
	raw, contentType, err := downloadImage(ctx, img.Src)
	if err != nil {
		return "", err
	}
	shortID := imgproc.ShortID(raw)
	ext := imageExt(img.Src, contentType)
	base := fmt.Sprintf("merch/shopify/%s/%d-%s", normalizeSlug(handle), img.ID, shortID)
	origKey := base + ext
	if !spaces.Exists(origKey) {
		if _, err := spaces.Upload(origKey, raw, contentType, ""); err != nil {
			return "", fmt.Errorf("upload original %s: %w", origKey, err)
		}
	}
	avifKey := base + ".avif"
	if !spaces.Exists(avifKey) {
		avif, err := imgproc.MakeAVIF(raw, 0)
		if err != nil {
			return "", fmt.Errorf("make avif %s: %w", img.Src, err)
		}
		if _, err := spaces.Upload(avifKey, avif, "image/avif", ""); err != nil {
			return "", fmt.Errorf("upload avif %s: %w", avifKey, err)
		}
	}
	return avifKey, nil
}

func downloadImage(ctx context.Context, src string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download image %s: %w", src, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download image %s: status %d", src, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", err
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if contentType == "" {
		contentType = http.DetectContentType(raw)
	}
	return raw, contentType, nil
}

func imageExt(src, contentType string) string {
	if ext := strings.ToLower(filepath.Ext(strings.Split(src, "?")[0])); ext != "" {
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".avif", ".gif":
			return ext
		}
	}
	if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
		ext := strings.ToLower(exts[0])
		if ext == ".jpe" {
			return ".jpg"
		}
		return ext
	}
	return ".img"
}

func productStatus(p shopifyProduct, forced string) string {
	switch forced {
	case types.MerchProductStatusDraft, types.MerchProductStatusPublished, types.MerchProductStatusArchived:
		return forced
	}
	if strings.TrimSpace(p.PublishedAt) == "" {
		return types.MerchProductStatusDraft
	}
	return types.MerchProductStatusPublished
}

func basePrice(p shopifyProduct) int {
	base := 0
	for _, v := range p.Variants {
		price := cents(v.Price)
		if price > 0 && (base == 0 || price < base) {
			base = price
		}
	}
	return base
}

func variantSKU(p shopifyProduct, v shopifyVariant) string {
	if v.SKU != nil && strings.TrimSpace(*v.SKU) != "" {
		return strings.TrimSpace(*v.SKU)
	}
	return fmt.Sprintf("SHOPIFY-%d", v.ID)
}

func variantLabel(v shopifyVariant) string {
	title := strings.TrimSpace(v.Title)
	if title == "" || strings.EqualFold(title, "Default Title") {
		return "Default"
	}
	return title
}

func cents(price string) int {
	price = strings.TrimSpace(price)
	if price == "" {
		return 0
	}
	parts := strings.SplitN(price, ".", 3)
	whole, _ := strconv.Atoi(parts[0])
	c := whole * 100
	if len(parts) > 1 {
		frac := parts[1]
		if len(frac) == 1 {
			frac += "0"
		}
		if len(frac) > 2 {
			frac = frac[:2]
		}
		n, _ := strconv.Atoi(frac)
		c += n
	}
	return c
}

func stripHTML(s string) string {
	s = html.UnescapeString(s)
	s = htmlTagRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func normalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = tagRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func inferProductType(p shopifyProduct) string {
	name := strings.ToLower(p.Title + " " + strings.Join(p.Tags, " "))
	switch {
	case strings.Contains(name, "hat"):
		return "apparel"
	case strings.Contains(name, "shirt"), strings.Contains(name, "tee"):
		return "apparel"
	case strings.Contains(name, "sticker"):
		return "stickers"
	default:
		return "standard"
	}
}

func easyshipCategory(productType string) string {
	switch strings.ToLower(productType) {
	case "apparel":
		return "fashion"
	default:
		return "accessories"
	}
}

func stripeTaxCode(p shopifyProduct) string {
	for _, v := range p.Variants {
		if !v.Taxable {
			return types.StripeTaxCodeNontaxable
		}
	}
	return types.StripeTaxCodeTangibleGood
}

func requiresShipping(p shopifyProduct) bool {
	if len(p.Variants) == 0 {
		return true
	}
	for _, v := range p.Variants {
		if v.RequiresShipping {
			return true
		}
	}
	return false
}

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(0)
}
