package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/alexedwards/scs/v2"
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
		{"protection upgrade rejects email", &auth.Identity{Method: auth.MethodEmailLink, AuthenticatedAt: now.Add(-time.Minute)}, &ManagedSignerAuthorizationPage{Action: "protect_identity"}, true},
		{"nsec recovery accepts nostr", &auth.Identity{Method: auth.MethodNostr, AuthenticatedAt: now.Add(-time.Minute)}, &ManagedSignerAuthorizationPage{Action: "recover_identity"}, false},
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

func TestManagedBadgeBatchReviewVerifiesGrantSnapshot(t *testing.T) {
	target := strings.Repeat("1", 48)
	eventHash := strings.Repeat("2", 64)
	issuer := strings.Repeat("3", 64)
	recipient := strings.Repeat("4", 64)
	organizationID := "00000000-0000-4000-8000-000000000010"
	personID := "00000000-0000-4000-8000-000000000011"
	grantID := "00000000-0000-4000-8000-000000000012"
	profileURL := "https://btcpp.dev/whois/example"
	signer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + target + `","tenant":"organization","tenant_id":"` + organizationID + `","event_hash":"` + eventHash + `","event_kind":8,"recipient_count":1,"badge_address":"30009:` + issuer + `:mentor","recipients":[{"pubkey":"` + recipient + `","person_id":"` + personID + `","grant_id":"` + grantID + `","subject_profile_url":"` + profileURL + `"}]}`))
	}))
	defer signer.Close()
	previous := loadManagedSignerBadgeGrant
	previousProfile := loadManagedSignerSubjectProfileURL
	loadManagedSignerBadgeGrant = func(_ *config.AppContext, id string) (*types.OrganizationBadgeGrant, error) {
		return &types.OrganizationBadgeGrant{ID: id, OrganizationID: organizationID, IssuerPubkey: issuer, BadgeIdentifier: "mentor", RecipientPubkey: recipient, RecipientPersonID: personID, SubjectProfileURL: profileURL, State: getters.BadgeGrantStateReady}, nil
	}
	loadManagedSignerSubjectProfileURL = func(_ *config.AppContext, id string) (string, error) {
		if id != personID {
			t.Fatalf("profile person = %q", id)
		}
		return profileURL, nil
	}
	t.Cleanup(func() {
		loadManagedSignerBadgeGrant = previous
		loadManagedSignerSubjectProfileURL = previousProfile
	})
	ctx := &config.AppContext{Env: &types.EnvConfig{SignerURL: signer.URL}}
	page := &ManagedSignerAuthorizationPage{Tenant: "organization", TenantID: organizationID, Action: "sign", Target: target, EventHash: eventHash, EventKind: 8, RecipientCount: 1}
	request := httptest.NewRequest(http.MethodGet, "/signer/authorize", nil)
	if err := validateManagedBadgeBatch(request, ctx, page); err != nil || page.VerifiedGrants != 1 {
		t.Fatalf("verified grants = %d, %v", page.VerifiedGrants, err)
	}
	loadManagedSignerBadgeGrant = func(_ *config.AppContext, id string) (*types.OrganizationBadgeGrant, error) {
		return &types.OrganizationBadgeGrant{ID: id, OrganizationID: organizationID, IssuerPubkey: issuer, BadgeIdentifier: "different", RecipientPubkey: recipient, RecipientPersonID: personID, SubjectProfileURL: profileURL, State: getters.BadgeGrantStateReady}, nil
	}
	if err := validateManagedBadgeBatch(request, ctx, page); err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("changed grant was accepted: %v", err)
	}
}

func TestManagedSignerAuthenticationRejectsMissingTimestamp(t *testing.T) {
	err := requireSignerAuthentication(&auth.Identity{Method: auth.MethodPasskey}, &ManagedSignerAuthorizationPage{Action: "create_identity"})
	if err == nil {
		t.Fatalf("expected recent authentication error, got %v", err)
	}
}

func TestManagedSignerCreatePageNamesOrganizationAndAction(t *testing.T) {
	organizationID := "00000000-0000-4000-8000-000000000501"
	signerURL := "https://signer.example"
	query := url.Values{
		"return_to": {signerURL + "/api/authorizations/callback"},
		"tenant":    {"organization"},
		"tenant_id": {organizationID},
		"action":    {"create_identity"},
	}
	request := httptest.NewRequest(http.MethodGet, "/signer/authorize?"+query.Encode(), nil)
	identity := &auth.Identity{Speaker: &types.Speaker{Name: "Mara Chen"}}
	memberships := []*types.OrganizationMembership{{
		OrganizationID: organizationID,
		Role:           getters.OrganizationRoleManager,
		Status:         "active",
		Organization:   &types.Org{Name: "Signet Systems"},
	}}

	page, err := managedSignerAuthorizationPage(request, &config.AppContext{Env: &types.EnvConfig{SignerURL: signerURL}}, identity, memberships)
	if err != nil {
		t.Fatal(err)
	}
	if page.TenantName != "Signet Systems" || page.Role != getters.OrganizationRoleManager || page.ActionLabel != "create a new Nostr signer" {
		t.Fatalf("managed signer setup context = %#v", page)
	}
}

func TestPendingManagedSignerAuthorizationIsBoundAndOneUse(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"return_to": {"https://signer.example/api/authorizations/callback"},
		"tenant":    {"organization"}, "tenant_id": {"org-1"}, "action": {"import_identity"},
		"csrf": {"must-not-be-replayed"}, "decision": {"allow"},
	}
	request := httptest.NewRequest(http.MethodPost, "/signer/authorize", strings.NewReader(values.Encode())).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := &config.AppContext{Session: manager}
	id, err := storePendingSignerAuthorization(ctx, request, "person-1")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := takePendingSignerAuthorization(ctx, request, id)
	if err != nil {
		t.Fatal(err)
	}
	if pending.PersonID != "person-1" || pending.Values["tenant_id"][0] != "org-1" {
		t.Fatalf("pending authorization = %#v", pending)
	}
	if _, exists := pending.Values["csrf"]; exists {
		t.Fatal("pending authorization retained CSRF token")
	}
	if _, err := takePendingSignerAuthorization(ctx, request, id); err == nil {
		t.Fatal("pending signer authorization was reusable")
	}
}

func TestManagedSignerBindingDistinguishesDashboardAndNIP46Connect(t *testing.T) {
	for _, test := range []struct {
		name    string
		page    ManagedSignerAuthorizationPage
		wantErr bool
	}{
		{"open signer dashboard", ManagedSignerAuthorizationPage{Action: "connect"}, false},
		{"exact NIP-46 connection", ManagedSignerAuthorizationPage{Action: "connect", EventHash: strings.Repeat("a", 64), Target: strings.Repeat("b", 48)}, false},
		{"exact combined Badge Studio login", ManagedSignerAuthorizationPage{Action: "connect_login", EventHash: strings.Repeat("a", 64), Target: strings.Repeat("b", 48), EventKind: 27235}, false},
		{"combined login without NIP-98 kind", ManagedSignerAuthorizationPage{Action: "connect_login", EventHash: strings.Repeat("a", 64), Target: strings.Repeat("b", 48)}, true},
		{"partial NIP-46 connection", ManagedSignerAuthorizationPage{Action: "connect", EventHash: strings.Repeat("a", 64)}, true},
		{"sign without pending target", ManagedSignerAuthorizationPage{Action: "sign", EventHash: strings.Repeat("a", 64)}, true},
		{"manager enrollment exact target", ManagedSignerAuthorizationPage{Action: "accept_unlock_enrollment", Target: strings.Repeat("c", 48)}, false},
		{"manager enrollment missing target", ManagedSignerAuthorizationPage{Action: "accept_unlock_enrollment"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateManagedSignerBinding(&test.page); (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, test.wantErr)
			}
		})
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

func TestManagedBadgeBatchReviewBindsExactRecipients(t *testing.T) {
	target := strings.Repeat("a", 48)
	eventHash := strings.Repeat("b", 64)
	issuer := strings.Repeat("c", 64)
	recipient := strings.Repeat("d", 64)
	signer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/authorizations/requests/"+target {
			t.Fatalf("request path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + target + `","tenant":"organization","tenant_id":"org-1","event_hash":"` + eventHash + `","event_kind":8,"recipient_count":1,"badge_address":"30009:` + issuer + `:mentor","recipients":[{"pubkey":"` + recipient + `","subject_profile_url":"https://btcpp.dev/whois/example"}]}`))
	}))
	defer signer.Close()

	ctx := &config.AppContext{Env: &types.EnvConfig{SignerURL: signer.URL}}
	page := &ManagedSignerAuthorizationPage{Tenant: "organization", TenantID: "org-1", Action: "sign", Target: target, EventHash: eventHash, EventKind: 8, RecipientCount: 1}
	request := httptest.NewRequest(http.MethodGet, "/signer/authorize", nil)
	if err := validateManagedBadgeBatch(request, ctx, page); err != nil {
		t.Fatal(err)
	}
	if page.BadgeAddress != "30009:"+issuer+":mentor" || len(page.BatchRecipients) != 1 || page.BatchRecipients[0].Pubkey != recipient {
		t.Fatalf("reviewed page = %#v", page)
	}

	page.EventHash = strings.Repeat("e", 64)
	if err := validateManagedBadgeBatch(request, ctx, page); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered authorization was accepted: %v", err)
	}
}
