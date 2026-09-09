package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadBadgeStudioProfileBuildsCredentialLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/btcpp/people/person-1/badges" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issued":[{"definition":{"name":"Mentor","image_url":"https://media.example/mentor.png"},"award":{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","recipients":["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]}}],"pending":[{"recipient_name":"Mara","badge":{"name":"Host"}}]}`))
	}))
	defer server.Close()
	previous := badgeStudioHTTPClient
	badgeStudioHTTPClient = server.Client()
	t.Cleanup(func() { badgeStudioHTTPClient = previous })
	profile, err := loadBadgeStudioProfile(context.Background(), server.URL, "person-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Issued) != 1 || len(profile.Pending) != 1 || profile.Issued[0].CredentialURL != server.URL+"/credentials/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || profile.Issued[0].ClaimURL != server.URL+"/claim/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestLoadOrganizationBadgeCatalog(t *testing.T) {
	issuer := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/btcpp/organizations/org-1/badges" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organization":{"pubkey":"` + issuer + `"},"badges":[{"name":"Mentor","description":"Shared knowledge","image_url":"https://media.example/mentor.png"}]}`))
	}))
	defer server.Close()
	previous := badgeStudioHTTPClient
	badgeStudioHTTPClient = server.Client()
	t.Cleanup(func() { badgeStudioHTTPClient = previous })
	catalog, err := loadOrganizationBadgeCatalog(context.Background(), server.URL, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.IssuerPubkey != issuer || len(catalog.Badges) != 1 || catalog.CatalogURL != server.URL+"/organizations/"+issuer {
		t.Fatalf("catalog = %#v", catalog)
	}
}
