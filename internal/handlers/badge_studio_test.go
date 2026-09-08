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
	if len(profile.Issued) != 1 || len(profile.Pending) != 1 || profile.Issued[0].CredentialURL != server.URL+"/credentials/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("profile = %#v", profile)
	}
}
