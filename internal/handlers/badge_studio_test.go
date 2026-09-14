package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoadBadgeStudioProfileBuildsCredentialLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/btcpp/people/person-1/badges" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issued":[{"definition":{"name":"Mentor","image_url":"https://media.example/mentor.png"},"award":{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","recipients":["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"],"acceptances":{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":{"event_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","created_at":"2026-09-05T12:00:00Z"}}}}],"pending":[{"recipient_name":"Mara","badge":{"name":"Host"}}]}`))
	}))
	defer server.Close()
	previous := badgeStudioHTTPClient
	badgeStudioHTTPClient = server.Client()
	t.Cleanup(func() { badgeStudioHTTPClient = previous })
	profile, err := loadBadgeStudioProfile(context.Background(), server.URL, "person-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Issued) != 1 || len(profile.Pending) != 1 || !profile.Issued[0].Accepted || profile.Issued[0].CredentialURL != server.URL+"/credentials/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || profile.Issued[0].ClaimURL != "" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestLoadBadgeStudioProfileBuildsClaimLinkForUnacceptedAward(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issued":[{"definition":{"name":"Mentor"},"award":{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","recipients":["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]}}]}`))
	}))
	defer server.Close()
	previous := badgeStudioHTTPClient
	badgeStudioHTTPClient = server.Client()
	t.Cleanup(func() { badgeStudioHTTPClient = previous })

	profile, err := loadBadgeStudioProfile(context.Background(), server.URL, "person-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Issued) != 1 || profile.Issued[0].Accepted || profile.Issued[0].ClaimURL != server.URL+"/api/auth/btcpp/continue?return_to=%2Fclaim%2Faaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
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
	if catalog.IssuerPubkey != issuer || len(catalog.Badges) != 1 || catalog.CatalogURL != server.URL+"/btcpp/organizations/org-1" {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestExpiredStudioAwardsCannotReappearFromLocalGrants(t *testing.T) {
	active := `{"issued":[{"definition":{"issuer_pubkey":"issuer","name":"Membership"},"award":{"event_id":"award","recipients":["recipient"]}}],"pending":[]}`
	for _, tc := range []struct {
		name, body   string
		status, want int
	}{
		{"expired", `{"issued":[],"pending":[]}`, 200, 0},
		{"renewed", active, 200, 1},
		{"unavailable", "unavailable", 503, 0},
		{"invalid response", `{"issued":`, 200, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			profile, err := loadBadgeStudioProfile(context.Background(), server.URL, "person")
			if err != nil {
				profile = nil
			}
			grants := []*types.OrganizationBadgeGrant{{ID: "grant", OrganizationID: "org", IssuerPubkey: "issuer", AwardEventID: "award", State: getters.BadgeGrantStateAccepted, BadgeName: "Membership"}}
			profile, grants = organizationOnlyProfileBadges(profile, grants)
			attachWhoIsBadgeIssuers(profile, grants)
			collection := buildWhoIsBadgeCollection(publicWhoIsBadgeProfile(profile), whoIsBadgeGrants(grants), nil)
			if len(collection.All) != tc.want || len(collection.Featured) != tc.want {
				t.Fatalf("badge count = %d, featured = %d; want %d", len(collection.All), len(collection.Featured), tc.want)
			}
		})
	}
}

func TestPendingBadgeGrantsRemainVisibleWithoutStudio(t *testing.T) {
	now := time.Now()
	grants := []*types.OrganizationBadgeGrant{
		{ID: "granted", State: getters.BadgeGrantStateGranted},
		{ID: "ready", State: getters.BadgeGrantStateReady},
		{ID: "retry", State: getters.BadgeGrantStateDeliveryError},
		{ID: "issued-without-id", State: getters.BadgeGrantStateIssued},
		{ID: "accepted-without-id", State: getters.BadgeGrantStateAccepted},
		{ID: "stale-state", State: getters.BadgeGrantStateReady, AwardEventID: "award"},
		{ID: "issued-at", State: getters.BadgeGrantStateGranted, IssuedAt: &now},
		{ID: "unknown", State: "unknown"},
	}
	visible := whoIsBadgeGrants(grants)
	if len(visible) != 3 || visible[0].ID != "granted" || visible[1].ID != "ready" || visible[2].ID != "retry" {
		t.Fatalf("unexpected pending grants: %+v", visible)
	}
}
