package easyship

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestCollectRatesFindsCheapestNestedRate(t *testing.T) {
	payload := map[string]any{
		"rates": []any{
			map[string]any{
				"courier_service_id": "slow",
				"courier_name":       "SlowShip",
				"total_charge":       json.Number("12.50"),
				"currency":           "usd",
			},
			map[string]any{
				"courier_service_id": "fast",
				"courier_name":       "FastShip",
				"total_charge":       json.Number("8.25"),
				"currency":           "USD",
			},
		},
	}
	rates := collectRates(payload)
	if len(rates) != 2 {
		t.Fatalf("collectRates returned %d rates, want 2", len(rates))
	}
	var cheapest Rate
	for i, rate := range rates {
		if i == 0 || rate.AmountCents < cheapest.AmountCents {
			cheapest = rate
		}
	}
	if cheapest.ProviderQuoteID != "fast" || cheapest.AmountCents != 825 {
		t.Fatalf("cheapest = (%q, %d), want fast 825", cheapest.ProviderQuoteID, cheapest.AmountCents)
	}
}

func TestCollectRatesReadsCurrentNestedCourierService(t *testing.T) {
	payload := map[string]any{"rates": []any{map[string]any{
		"total_charge": json.Number("9.25"), "currency": "USD",
		"courier_service": map[string]any{
			"id": "service-current", "name": "FedEx Ground", "umbrella_name": "FedEx",
		},
	}}}
	rates := collectRates(payload)
	if len(rates) != 1 {
		t.Fatalf("rates = %#v", rates)
	}
	if rates[0].ProviderQuoteID != "service-current" || rates[0].CourierName != "FedEx" || rates[0].ServiceName != "FedEx Ground" {
		t.Fatalf("rate = %#v", rates[0])
	}
}

func TestEasyshipEndpointDefaultsRatePath(t *testing.T) {
	if got := easyshipEndpoint("", ""); got != "https://public-api.easyship.com/2024-09/rates" {
		t.Fatalf("empty endpoint = %q", got)
	}
	if got := easyshipEndpoint("https://public-api-sandbox.easyship.com", "2024-09"); got != "https://public-api-sandbox.easyship.com/2024-09/rates" {
		t.Fatalf("sandbox endpoint = %q", got)
	}
	if got := easyshipEndpoint("https://example.test/custom", "2024-09"); got != "https://example.test/custom" {
		t.Fatalf("custom endpoint = %q", got)
	}
}

func TestQuoteUsesCurrentRatesContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2024-09/rates" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sand_test" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Fatal(err)
		}
		parcels, ok := body["parcels"].([]any)
		if !ok || len(parcels) != 1 {
			t.Fatalf("parcels = %#v", body["parcels"])
		}
		parcel := parcels[0].(map[string]any)
		items := parcel["items"].([]any)
		item := items[0].(map[string]any)
		if got := item["declared_customs_value"]; got != json.Number("25") {
			t.Errorf("declared_customs_value = %#v", got)
		}
		if got := item["origin_country_alpha2"]; got != "US" {
			t.Errorf("origin_country_alpha2 = %#v", got)
		}
		if _, exists := body["items"]; exists {
			t.Error("legacy top-level items field was sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rates":[{"courier_service_id":"svc_123","courier_name":"USPS Ground Advantage","total_charge":7.45,"currency":"USD","min_delivery_time":2,"max_delivery_time":5}]}`))
	}))
	defer server.Close()

	rate, err := Quote(context.Background(), types.EasyshipConfig{
		APIKey: "sand_test", Endpoint: server.URL, APIVersion: "2024-09",
	}, Address{ContactName: "Fulfillment", Country: "US", Region: "TX", City: "Austin", PostalCode: "78701", Line1: "101 Main St"}, Address{Country: "US", Region: "NY", City: "Brooklyn", PostalCode: "11201", Line1: "55 Prospect St"}, []Item{{
		SKU: "hat-black", Name: "Bitcoin++ hat", Quantity: 3, ValueCents: 2500,
		WeightGrams: 200, LengthMM: 250, WidthMM: 200, HeightMM: 120,
		Category: "fashion", OriginCountry: "US",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rate.ProviderQuoteID != "svc_123" || rate.AmountCents != 745 {
		t.Fatalf("rate = %#v", rate)
	}
}

func TestQuoteRejectsIncompleteParcelDataBeforeRequest(t *testing.T) {
	_, err := Quote(context.Background(), types.EasyshipConfig{
		APIKey: "sand_test",
	}, Address{Country: "US", City: "Austin", PostalCode: "78701", Line1: "101 Main St"}, Address{Country: "US", City: "Brooklyn", PostalCode: "11201", Line1: "55 Prospect St"}, []Item{{
		SKU: "hat-black", Name: "Hat", Quantity: 1, ValueCents: 2500,
		Category: "fashion", OriginCountry: "US",
	}})
	if err == nil || !strings.Contains(err.Error(), "weight and dimensions") {
		t.Fatalf("error = %v, want missing parcel dimensions", err)
	}
}

func TestRatesReturnsAllServicesSortedByPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rates":[
			{"courier_service_id":"express","courier_name":"Express","total_charge":19.50,"currency":"USD"},
			{"courier_service_id":"ground","courier_name":"Ground","total_charge":7.25,"currency":"USD"},
			{"courier_service_id":"ground","courier_name":"Ground duplicate","total_charge":7.25,"currency":"USD"}
		]}`))
	}))
	defer server.Close()

	rates, err := Rates(context.Background(), types.EasyshipConfig{
		APIKey: "sand_test", Endpoint: server.URL, APIVersion: "2024-09",
	}, Address{Country: "US", City: "Austin", PostalCode: "78701", Line1: "101 Main St"}, Address{Country: "US", City: "Brooklyn", PostalCode: "11201", Line1: "55 Prospect St"}, []Item{{
		SKU: "hat", Name: "Hat", ValueCents: 2500, WeightGrams: 200,
		LengthMM: 250, WidthMM: 200, HeightMM: 120, Category: "fashion", OriginCountry: "US",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 2 {
		t.Fatalf("rates = %#v, want two unique services", rates)
	}
	if rates[0].ProviderQuoteID != "ground" || rates[1].ProviderQuoteID != "express" {
		t.Fatalf("rate order = %q, %q", rates[0].ProviderQuoteID, rates[1].ProviderQuoteID)
	}
}

func TestCreateShipmentUsesSelectedServiceAndIdempotencyKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2024-09/shipments" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "shipment-key" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		courierSettings := body["courier_settings"].(map[string]any)
		if got := courierSettings["courier_service_id"]; got != "service-123" {
			t.Errorf("courier_service_id = %#v", got)
		}
		dest := body["destination_address"].(map[string]any)
		if dest["contact_name"] != "Ada Buyer" || dest["contact_email"] != "ada@example.test" {
			t.Errorf("destination contact = %#v", dest)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"shipment":{"easyship_shipment_id":"ESUS12345678","courier_service_id":"service-123","courier_name":"USPS","courier_service_name":"Ground","label_state":"not_created"}}`))
	}))
	defer server.Close()

	result, err := CreateShipment(context.Background(), types.EasyshipConfig{
		APIKey: "sand_test", Endpoint: server.URL, APIVersion: "2024-09",
	}, Address{ContactName: "Warehouse", Country: "US", City: "Austin", PostalCode: "78701", Line1: "1 Main"}, Address{
		ContactName: "Ada Buyer", Email: "ada@example.test", Country: "US", City: "LA", PostalCode: "90001", Line1: "2 Main",
	}, []Item{{SKU: "hat", Name: "Hat", ValueCents: 2500, WeightGrams: 200, LengthMM: 250, WidthMM: 200, HeightMM: 120, Category: "fashion", OriginCountry: "US"}}, "service-123", "ORDER-123", "shipment-key")
	if err != nil {
		t.Fatal(err)
	}
	if result.EasyshipShipmentID != "ESUS12345678" || result.CourierServiceID != "service-123" || result.LabelState != "not_created" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCreateLabelRequestsURLDocumentsAndParsesTracking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2024-09/shipments/ESUS12345678/label" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "label-key" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		options := body["printing_options"].(map[string]any)
		if options["format"] != "url" || options["label"] != "4x6" {
			t.Errorf("printing_options = %#v", options)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"shipment":{"label_state":"generated","shipping_documents":[{"category":"label","format":"url","url":"https://labels.example.test/label.pdf"}],"tracking_number":"TRACK123","tracking_page_url":"https://track.example.test/TRACK123"}}`))
	}))
	defer server.Close()

	result, err := CreateLabel(context.Background(), types.EasyshipConfig{
		APIKey: "sand_test", Endpoint: server.URL, APIVersion: "2024-09",
	}, "ESUS12345678", "service-123", "label-key")
	if err != nil {
		t.Fatal(err)
	}
	if result.LabelState != "generated" || result.LabelURL == "" || result.TrackingNumber != "TRACK123" {
		t.Fatalf("result = %#v", result)
	}
}
