package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/bip353"
)

func TestClientLifecycleRequests(t *testing.T) {
	t.Parallel()
	var stored dnsRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone/dnssec":
			writeJSON(t, w, map[string]any{"success": true, "result": map[string]any{"status": "active"}})
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone/dns_records":
			if r.URL.Query().Get("type") != "TXT" || r.URL.Query().Get("name") == "" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			result := []dnsRecord{}
			if stored.ID != "" {
				result = append(result, stored)
			}
			writeJSON(t, w, map[string]any{"success": true, "result": result, "result_info": map[string]any{"page": 1, "total_pages": 1}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone/dns_records":
			if err := json.NewDecoder(r.Body).Decode(&stored); err != nil {
				t.Fatal(err)
			}
			stored.ID = "created-id"
			writeJSON(t, w, map[string]any{"success": true, "result": stored})
		case r.Method == http.MethodPut && r.URL.Path == "/zones/zone/dns_records/created-id":
			if err := json.NewDecoder(r.Body).Decode(&stored); err != nil {
				t.Fatal(err)
			}
			stored.ID = "created-id"
			writeJSON(t, w, map[string]any{"success": true, "result": stored})
		case r.Method == http.MethodDelete && r.URL.Path == "/zones/zone/dns_records/created-id":
			stored = dnsRecord{}
			writeJSON(t, w, map[string]any{"success": true, "result": map[string]any{"id": "created-id"}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New("secret", "zone", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := bip353.NewManager(client, "zap.example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := manager.Put(ctx, bip353.Entry{User: "conf", URI: "bitcoin:?lno=lno1first", TTL: 300})
	if err != nil || !created.Created {
		t.Fatalf("create = %+v, %v", created, err)
	}
	updated, err := manager.Put(ctx, bip353.Entry{User: "conf", URI: "bitcoin:?lno=lno1second", TTL: 600})
	if err != nil || updated.Record.Content != "bitcoin:?lno=lno1second" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if err := manager.Delete(ctx, "conf"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDNSSECInactive(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{"success": true, "result": map[string]any{"status": "pending"}})
	}))
	defer server.Close()
	client, _ := New("secret", "zone", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	if err := client.CheckDNSSEC(context.Background()); !errors.Is(err, ErrDNSSECInactive) {
		t.Fatalf("CheckDNSSEC() error = %v", err)
	}
}

func TestAPIErrorDoesNotExposeToken(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeJSON(t, w, map[string]any{"success": false, "errors": []map[string]any{{"code": 10000, "message": "Authentication error"}}})
	}))
	defer server.Close()
	client, _ := New("top-secret-token", "zone", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	err := client.CheckDNSSEC(context.Background())
	if err == nil || strings.Contains(err.Error(), "top-secret-token") {
		t.Fatalf("unsafe API error: %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
