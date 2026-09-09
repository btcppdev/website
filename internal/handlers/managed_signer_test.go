package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/auth"
)

func TestManagedSignerAuthenticationEscalation(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name     string
		identity *auth.Identity
		page     *ManagedSignerAuthorizationPage
		wantErr  bool
	}{
		{"recent ordinary connect", &auth.Identity{Method: auth.MethodEmailLink, AuthenticatedAt: now.Add(-time.Minute)}, &ManagedSignerAuthorizationPage{Action: "connect"}, false},
		{"stale connect", &auth.Identity{Method: auth.MethodEmailLink, AuthenticatedAt: now.Add(-16 * time.Minute)}, &ManagedSignerAuthorizationPage{Action: "connect"}, true},
		{"import rejects password", &auth.Identity{Method: auth.MethodPassword, AuthenticatedAt: now.Add(-time.Minute)}, &ManagedSignerAuthorizationPage{Action: "import_identity"}, true},
		{"import accepts passkey", &auth.Identity{Method: auth.MethodPasskey, AuthenticatedAt: now.Add(-time.Minute)}, &ManagedSignerAuthorizationPage{Action: "import_identity"}, false},
		{"revocation rejects old passkey", &auth.Identity{Method: auth.MethodPasskey, AuthenticatedAt: now.Add(-6 * time.Minute)}, &ManagedSignerAuthorizationPage{Action: "sign", EventKind: 5}, true},
		{"large award accepts nostr", &auth.Identity{Method: auth.MethodNostr, AuthenticatedAt: now.Add(-time.Minute)}, &ManagedSignerAuthorizationPage{Action: "sign", EventKind: 8, RecipientCount: 21}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := requireSignerAuthentication(test.identity, test.page)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, test.wantErr)
			}
		})
	}
}

func TestManagedSignerAuthenticationRejectsMissingTimestamp(t *testing.T) {
	err := requireSignerAuthentication(&auth.Identity{Method: auth.MethodPasskey}, &ManagedSignerAuthorizationPage{Action: "create_identity"})
	if err == nil {
		t.Fatalf("expected recent authentication error, got %v", err)
	}
}

func TestManagedSignerRequestValidationMatchesBunkerSurface(t *testing.T) {
	tests := []struct {
		name      string
		page      ManagedSignerAuthorizationPage
		wantLabel string
		wantErr   bool
	}{
		{"organization badge batch", ManagedSignerAuthorizationPage{Tenant: "organization", Action: "sign", EventKind: 8, RecipientCount: 21}, "issue 21 badge awards", false},
		{"one independently revocable award", ManagedSignerAuthorizationPage{Tenant: "organization", Action: "sign", EventKind: 8, RecipientCount: 1}, "issue one badge award", false},
		{"oversized batch", ManagedSignerAuthorizationPage{Tenant: "organization", Action: "sign", EventKind: 8, RecipientCount: 101}, "", true},
		{"personal acceptance", ManagedSignerAuthorizationPage{Tenant: "person", Action: "sign", EventKind: 10008}, "accept badges on Nostr", false},
		{"personal award denied", ManagedSignerAuthorizationPage{Tenant: "person", Action: "sign", EventKind: 8, RecipientCount: 1}, "", true},
		{"organization arbitrary event denied", ManagedSignerAuthorizationPage{Tenant: "organization", Action: "sign", EventKind: 1}, "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateManagedSignerRequest(&test.page)
			if (err != nil) != test.wantErr || (!test.wantErr && test.page.ActionLabel != test.wantLabel) {
				t.Fatalf("label=%q error=%v", test.page.ActionLabel, err)
			}
		})
	}
}
