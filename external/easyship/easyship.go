package easyship

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"btcpp-web/internal/types"
)

type Address struct {
	ContactName string
	CompanyName string
	Email       string
	Phone       string
	Country     string
	Region      string
	PostalCode  string
	City        string
	Line1       string
	Line2       string
}

type Item struct {
	SKU           string
	Name          string
	Quantity      uint
	ValueCents    uint
	WeightGrams   int
	LengthMM      int
	WidthMM       int
	HeightMM      int
	HSCode        string
	Category      string
	OriginCountry string
}

type Rate struct {
	ProviderQuoteID string
	CourierName     string
	ServiceName     string
	AmountCents     uint
	Currency        string
	MinDays         *int
	MaxDays         *int
	Raw             json.RawMessage
}

type ShipmentResult struct {
	EasyshipShipmentID string
	CourierServiceID   string
	CourierName        string
	ServiceName        string
	LabelID            string
	LabelURL           string
	LabelState         string
	TrackingNumber     string
	TrackingURL        string
	Raw                json.RawMessage
}

type LabelResult struct {
	LabelID        string
	LabelURL       string
	LabelState     string
	TrackingNumber string
	TrackingURL    string
	Raw            json.RawMessage
}

func Quote(ctx context.Context, cfg types.EasyshipConfig, origin, dest Address, items []Item) (*Rate, error) {
	rates, err := Rates(ctx, cfg, origin, dest, items)
	if err != nil {
		return nil, err
	}
	return &rates[0], nil
}

func Rates(ctx context.Context, cfg types.EasyshipConfig, origin, dest Address, items []Item) ([]Rate, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("easyship api key is not configured")
	}
	if err := validateQuote(origin, dest, items); err != nil {
		return nil, err
	}
	endpoint := easyshipEndpoint(cfg.Endpoint, cfg.APIVersion)
	body := map[string]any{
		"origin_address": map[string]any{
			"name":           strings.TrimSpace(origin.ContactName),
			"company_name":   strings.TrimSpace(origin.CompanyName),
			"email":          strings.TrimSpace(origin.Email),
			"phone":          strings.TrimSpace(origin.Phone),
			"country_alpha2": strings.ToUpper(strings.TrimSpace(origin.Country)),
			"state":          strings.TrimSpace(origin.Region),
			"city":           strings.TrimSpace(origin.City),
			"postal_code":    strings.TrimSpace(origin.PostalCode),
			"line_1":         strings.TrimSpace(origin.Line1),
			"line_2":         strings.TrimSpace(origin.Line2),
		},
		"destination_address": map[string]any{
			"country_alpha2": strings.ToUpper(firstNonEmpty(dest.Country, "US")),
			"state":          dest.Region,
			"city":           dest.City,
			"postal_code":    dest.PostalCode,
			"line_1":         dest.Line1,
			"line_2":         dest.Line2,
		},
		"incoterms": "DDU",
		"parcels": []map[string]any{{
			"items": easyshipItems(items),
		}},
	}
	rawBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("easyship quote request: %w", err)
	}
	defer resp.Body.Close()
	var payload any
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("easyship quote decode: %w", err)
	}
	raw, _ := json.Marshal(payload)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("easyship quote status %d: %s", resp.StatusCode, string(raw))
	}
	rates := collectRates(payload)
	if len(rates) == 0 {
		return nil, fmt.Errorf("easyship quote returned no rates")
	}
	unique := make([]Rate, 0, len(rates))
	seen := make(map[string]bool, len(rates))
	for _, rate := range rates {
		if rate.ProviderQuoteID == "" || rate.AmountCents == 0 || seen[rate.ProviderQuoteID] {
			continue
		}
		seen[rate.ProviderQuoteID] = true
		unique = append(unique, rate)
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("easyship quote returned no selectable rates")
	}
	sort.SliceStable(unique, func(i, j int) bool {
		return unique[i].AmountCents < unique[j].AmountCents
	})
	return unique, nil
}

func CreateShipment(ctx context.Context, cfg types.EasyshipConfig, origin, dest Address, items []Item, courierServiceID, orderReference, idempotencyKey string) (*ShipmentResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("easyship api key is not configured")
	}
	if err := validateQuote(origin, dest, items); err != nil {
		return nil, err
	}
	courierServiceID = strings.TrimSpace(courierServiceID)
	if courierServiceID == "" {
		return nil, fmt.Errorf("easyship courier service is required")
	}
	body := map[string]any{
		"origin_address":      easyshipAddress(origin, true),
		"destination_address": easyshipAddress(dest, true),
		"incoterms":           "DDU",
		"courier_settings": map[string]any{
			"allow_courier_fallback": false,
			"apply_shipping_rules":   false,
			"courier_service_id":     courierServiceID,
		},
		"metadata": map[string]any{
			"shop_order_id": strings.TrimSpace(orderReference),
		},
		"order_data": map[string]any{
			"platform_order_number": strings.TrimSpace(orderReference),
		},
		"parcels": []map[string]any{{
			"items": easyshipItems(items),
		}},
	}
	payload, err := easyshipRequest(ctx, cfg, http.MethodPost, "shipments", body, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("create easyship shipment: %w", err)
	}
	shipmentID := deepFirstString(payload, "easyship_shipment_id")
	if shipmentID == "" {
		return nil, fmt.Errorf("create easyship shipment: response is missing easyship_shipment_id")
	}
	return &ShipmentResult{
		EasyshipShipmentID: shipmentID,
		CourierServiceID:   firstNonEmpty(deepFirstString(payload, "courier_service_id"), courierServiceID),
		CourierName:        deepFirstString(payload, "courier_name", "courier_display_name"),
		ServiceName:        deepFirstString(payload, "courier_service_name", "service_name"),
		LabelID:            deepFirstString(payload, "label_id", "easyship_label_id"),
		LabelURL:           firstNonEmpty(deepFirstString(payload, "label_url"), deepShippingDocumentURL(payload, "label")),
		LabelState:         firstNonEmpty(deepFirstString(payload, "label_state"), "not_created"),
		TrackingNumber:     deepFirstString(payload, "tracking_number"),
		TrackingURL:        deepFirstString(payload, "tracking_page_url", "tracking_url"),
		Raw:                payload,
	}, nil
}

func CreateLabel(ctx context.Context, cfg types.EasyshipConfig, shipmentID, courierServiceID, idempotencyKey string) (*LabelResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("easyship api key is not configured")
	}
	shipmentID = strings.TrimSpace(shipmentID)
	if shipmentID == "" {
		return nil, fmt.Errorf("easyship shipment id is required")
	}
	body := map[string]any{
		"courier_service_id": strings.TrimSpace(courierServiceID),
		"printing_options": map[string]any{
			"format":             "url",
			"label":              "4x6",
			"commercial_invoice": "A4",
			"packing_slip":       "none",
		},
	}
	payload, err := easyshipRequest(ctx, cfg, http.MethodPost, "shipments/"+url.PathEscape(shipmentID)+"/label", body, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("create easyship label: %w", err)
	}
	state := firstNonEmpty(deepFirstString(payload, "label_state"), "pending")
	return &LabelResult{
		LabelID:        deepFirstString(payload, "label_id", "easyship_label_id", "id"),
		LabelURL:       firstNonEmpty(deepFirstString(payload, "label_url"), deepShippingDocumentURL(payload, "label")),
		LabelState:     state,
		TrackingNumber: deepFirstString(payload, "tracking_number"),
		TrackingURL:    deepFirstString(payload, "tracking_page_url", "tracking_url"),
		Raw:            payload,
	}, nil
}

func easyshipRequest(ctx context.Context, cfg types.EasyshipConfig, method, resource string, body any, idempotencyKey string) (json.RawMessage, error) {
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, easyshipResourceEndpoint(cfg.Endpoint, cfg.APIVersion, resource), bytes.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload any
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	raw, _ := json.Marshal(payload)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

func easyshipAddress(address Address, includeContact bool) map[string]any {
	out := map[string]any{
		"country_alpha2": strings.ToUpper(strings.TrimSpace(address.Country)),
		"state":          strings.TrimSpace(address.Region),
		"city":           strings.TrimSpace(address.City),
		"postal_code":    strings.TrimSpace(address.PostalCode),
		"line_1":         strings.TrimSpace(address.Line1),
		"line_2":         strings.TrimSpace(address.Line2),
	}
	if includeContact {
		out["contact_name"] = truncateRunes(address.ContactName, 22)
		out["company_name"] = strings.TrimSpace(address.CompanyName)
		out["contact_email"] = strings.TrimSpace(address.Email)
		out["contact_phone"] = strings.TrimSpace(address.Phone)
	}
	return out
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}

func easyshipResourceEndpoint(endpoint, version, resource string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "https://public-api.easyship.com"
	}
	version = firstNonEmpty(version, "2024-09")
	u, err := url.Parse(endpoint)
	if err != nil {
		return strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(resource, "/")
	}
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/rates") {
		path = strings.TrimSuffix(path, "/rates")
	}
	if !strings.HasSuffix(path, "/"+strings.Trim(version, "/")) {
		path += "/" + strings.Trim(version, "/")
	}
	u.Path = strings.TrimRight(path, "/") + "/" + strings.TrimLeft(resource, "/")
	return u.String()
}

func validateQuote(origin, dest Address, items []Item) error {
	if strings.TrimSpace(origin.Country) == "" || strings.TrimSpace(origin.City) == "" ||
		strings.TrimSpace(origin.PostalCode) == "" || strings.TrimSpace(origin.Line1) == "" {
		return fmt.Errorf("easyship origin address is incomplete")
	}
	if strings.TrimSpace(dest.Country) == "" || strings.TrimSpace(dest.City) == "" ||
		strings.TrimSpace(dest.PostalCode) == "" || strings.TrimSpace(dest.Line1) == "" {
		return fmt.Errorf("easyship destination address is incomplete")
	}
	if len(items) == 0 {
		return fmt.Errorf("easyship quote requires at least one item")
	}
	for _, item := range items {
		label := firstNonEmpty(item.SKU, item.Name, "item")
		if item.WeightGrams <= 0 || item.LengthMM <= 0 || item.WidthMM <= 0 || item.HeightMM <= 0 {
			return fmt.Errorf("easyship item %q requires positive weight and dimensions", label)
		}
		if strings.TrimSpace(item.HSCode) == "" && strings.TrimSpace(item.Category) == "" {
			return fmt.Errorf("easyship item %q requires an HS code or Easyship category", label)
		}
		if strings.TrimSpace(item.OriginCountry) == "" {
			return fmt.Errorf("easyship item %q requires a country of origin", label)
		}
		if item.ValueCents == 0 && !strings.EqualFold(strings.TrimSpace(item.Category), "documents") {
			return fmt.Errorf("easyship item %q requires a positive per-unit customs value", label)
		}
	}
	return nil
}

func easyshipEndpoint(endpoint, version string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "https://public-api.easyship.com"
	}
	version = firstNonEmpty(version, "2024-09")
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/rates") {
		return endpoint
	}
	if path == "" || path == "/" {
		u.Path = "/" + strings.Trim(version, "/") + "/rates"
		return u.String()
	}
	if strings.HasSuffix(path, "/"+strings.Trim(version, "/")) {
		u.Path = path + "/rates"
		return u.String()
	}
	// A non-root path is treated as an explicitly configured full endpoint.
	return endpoint
}

func easyshipItems(items []Item) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		qty := item.Quantity
		if qty == 0 {
			qty = 1
		}
		out = append(out, map[string]any{
			"sku":                    item.SKU,
			"description":            item.Name,
			"category":               item.Category,
			"hs_code":                item.HSCode,
			"origin_country_alpha2":  strings.ToUpper(firstNonEmpty(item.OriginCountry, "US")),
			"quantity":               qty,
			"actual_weight":          gramsToKg(item.WeightGrams),
			"declared_currency":      "USD",
			"declared_customs_value": centsToDecimal(item.ValueCents),
			"dimensions": map[string]any{
				"length": mmToCm(item.LengthMM),
				"width":  mmToCm(item.WidthMM),
				"height": mmToCm(item.HeightMM),
			},
		})
	}
	return out
}

func collectRates(v any) []Rate {
	var out []Rate
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			out = append(out, rateFromMap(asMap(item))...)
		}
	case map[string]any:
		out = append(out, rateFromMap(x)...)
		for _, key := range []string{"rates", "courier_rates", "available_courier_rates"} {
			out = append(out, collectRates(x[key])...)
		}
	}
	return out
}

func rateFromMap(m map[string]any) []Rate {
	if len(m) == 0 {
		return nil
	}
	amount := firstFloat(m, "total_charge", "total", "amount", "shipment_charge_total", "cost")
	if amount == 0 {
		return nil
	}
	courierService := asMap(m["courier_service"])
	providerQuoteID := firstNonEmpty(
		firstString(m, "courier_service_id", "courier_id", "id", "quote_id", "rate_id"),
		firstString(courierService, "id"),
	)
	serviceName := firstNonEmpty(
		firstString(m, "service_name", "courier_service_name", "service"),
		firstString(courierService, "name", "nickname"),
	)
	courierName := firstNonEmpty(
		firstString(m, "courier_name", "courier", "courier_display_name"),
		firstString(courierService, "umbrella_name", "official_name", "name"),
	)
	raw, _ := json.Marshal(m)
	return []Rate{{
		ProviderQuoteID: providerQuoteID,
		CourierName:     courierName,
		ServiceName:     serviceName,
		AmountCents:     uint(math.Round(amount * 100)),
		Currency:        strings.ToUpper(firstNonEmpty(firstString(m, "currency", "currency_code"), "USD")),
		MinDays:         firstIntPtr(m, "min_delivery_time", "min_delivery_days"),
		MaxDays:         firstIntPtr(m, "max_delivery_time", "max_delivery_days"),
		Raw:             raw,
	}}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func firstFloat(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case json.Number:
			f, _ := v.Float64()
			return f
		case string:
			f, _ := json.Number(v).Float64()
			return f
		}
	}
	return 0
}

func firstIntPtr(m map[string]any, keys ...string) *int {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			if v > 0 {
				i := int(v)
				return &i
			}
		case json.Number:
			if i64, err := v.Int64(); err == nil && i64 > 0 {
				i := int(i64)
				return &i
			}
		}
	}
	return nil
}

func deepFirstString(raw json.RawMessage, keys ...string) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	return deepFirstStringValue(value, keys...)
}

func deepFirstStringValue(value any, keys ...string) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if candidate, ok := typed[key].(string); ok && strings.TrimSpace(candidate) != "" {
				return strings.TrimSpace(candidate)
			}
		}
		for _, nested := range typed {
			if candidate := deepFirstStringValue(nested, keys...); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, nested := range typed {
			if candidate := deepFirstStringValue(nested, keys...); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func deepShippingDocumentURL(raw json.RawMessage, category string) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return deepShippingDocumentURLValue(value, category)
}

func deepShippingDocumentURLValue(value any, category string) string {
	switch typed := value.(type) {
	case map[string]any:
		if strings.EqualFold(firstString(typed, "category"), category) {
			return firstString(typed, "url")
		}
		for _, nested := range typed {
			if found := deepShippingDocumentURLValue(nested, category); found != "" {
				return found
			}
		}
	case []any:
		for _, nested := range typed {
			if found := deepShippingDocumentURLValue(nested, category); found != "" {
				return found
			}
		}
	}
	return ""
}

func centsToDecimal(cents uint) float64 {
	return float64(cents) / 100
}

func gramsToKg(grams int) float64 {
	if grams <= 0 {
		return 0.1
	}
	return float64(grams) / 1000
}

func mmToCm(mm int) float64 {
	if mm <= 0 {
		return 1
	}
	return float64(mm) / 10
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
