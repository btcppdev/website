package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/signer"
	"btcpp-web/internal/types"
)

type ManagedSignerAuthorizationPage struct {
	PersonName     string
	Tenant         string
	TenantID       string
	TenantName     string
	Role           string
	Action         string
	ActionLabel    string
	EventHash      string
	Target         string
	EventKind      int
	RecipientCount int
	CSRF           string
	ReturnTo       string
	Token          string
	Error          string
	Year           uint
}

func ManagedSignerAuthorize(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	setManagedSignerHeaders(w, ctx)
	identity, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok || identity == nil {
		return
	}
	page, err := managedSignerAuthorizationPage(r, ctx, identity, memberships)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to begin signer authorization.", http.StatusInternalServerError)
		return
	}
	page.CSRF, page.Year = csrf, helpers.CurrentYear()
	if err := ctx.TemplateCache.ExecuteTemplate(w, "managed_signer_authorize.tmpl", page); err != nil {
		ctx.Err.Printf("render managed signer authorization: %s", err)
		http.Error(w, "Unable to render signer authorization.", http.StatusInternalServerError)
	}
}

func ManagedSignerAuthorizeDecision(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	setManagedSignerHeaders(w, ctx)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid signer authorization request.", http.StatusBadRequest)
		return
	}
	identity, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok || identity == nil {
		return
	}
	page, err := managedSignerAuthorizationPage(r, ctx, identity, memberships)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.FormValue("decision") != "allow" {
		recordAuthAudit(ctx, r, identity.PersonID, "managed_signer", "signer_authorization_denied", map[string]any{"tenant": page.Tenant, "tenant_id": page.TenantID, "action": page.Action})
		http.Redirect(w, r, ctx.Env.SignerURL+"/?authorization=denied", http.StatusSeeOther)
		return
	}
	if err := requireSignerAuthentication(identity, page); err != nil {
		page.Error, page.CSRF, page.Year = err.Error(), r.FormValue("csrf"), helpers.CurrentYear()
		w.WriteHeader(http.StatusForbidden)
		_ = ctx.TemplateCache.ExecuteTemplate(w, "managed_signer_authorize.tmpl", page)
		return
	}
	issuer, err := signer.NewIssuer(ctx.Env.SignerAuthPrivateKey)
	if err != nil {
		ctx.Err.Printf("managed signer issuer: %s", err)
		http.Error(w, "Managed signer authorization is not configured.", http.StatusServiceUnavailable)
		return
	}
	now := time.Now().UTC()
	tokenID, err := randomSignerTokenID()
	if err != nil {
		http.Error(w, "Unable to create signer authorization.", http.StatusInternalServerError)
		return
	}
	token, err := issuer.Sign(signer.Claims{
		Issuer: strings.TrimRight(ctx.Env.GetURI(), "/"), Audience: ctx.Env.SignerURL, Subject: identity.PersonID,
		Tenant: page.Tenant, TenantID: page.TenantID, Role: page.Role, Action: page.Action, EventHash: page.EventHash, Target: page.Target,
		AuthMethod: signerAuthMethod(identity.Method), AuthenticatedAt: identity.AuthenticatedAt.Unix(),
		IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), TokenID: tokenID,
	})
	if err != nil {
		http.Error(w, "Unable to sign authorization.", http.StatusInternalServerError)
		return
	}
	page.Token, page.Year = token, helpers.CurrentYear()
	recordAuthAudit(ctx, r, identity.PersonID, "managed_signer", "signer_authorization_issued", map[string]any{"tenant": page.Tenant, "tenant_id": page.TenantID, "action": page.Action, "event_hash": page.EventHash, "target": page.Target, "jti": tokenID})
	if err := ctx.TemplateCache.ExecuteTemplate(w, "managed_signer_continue.tmpl", page); err != nil {
		http.Error(w, "Unable to continue to managed signer.", http.StatusInternalServerError)
	}
}

func managedSignerAuthorizationPage(r *http.Request, ctx *config.AppContext, identity *auth.Identity, memberships []*types.OrganizationMembership) (*ManagedSignerAuthorizationPage, error) {
	if ctx.Env.SignerURL == "" || r.FormValue("return_to") != ctx.Env.SignerURL+"/api/authorizations/callback" {
		return nil, errors.New("invalid managed signer return URL")
	}
	action := strings.TrimSpace(r.FormValue("action"))
	switch action {
	case "connect", "create_identity", "import_identity", "export_identity", "rotate_identity", "revoke_connection", "sign":
	default:
		return nil, errors.New("unsupported managed signer action")
	}
	tenant, tenantID := strings.TrimSpace(r.FormValue("tenant")), strings.TrimSpace(r.FormValue("tenant_id"))
	page := &ManagedSignerAuthorizationPage{PersonName: identity.Speaker.Name, Tenant: tenant, TenantID: tenantID, Action: action, ActionLabel: strings.ReplaceAll(action, "_", " "), EventHash: strings.TrimSpace(r.FormValue("event_hash")), Target: strings.TrimSpace(r.FormValue("target")), ReturnTo: ctx.Env.SignerURL + "/api/authorizations/callback"}
	page.EventKind, _ = strconv.Atoi(r.FormValue("event_kind"))
	page.RecipientCount, _ = strconv.Atoi(r.FormValue("recipient_count"))
	if action == "sign" && !isLowerHex(page.EventHash, 64) {
		return nil, errors.New("signing authorization requires an exact event hash")
	}
	if action == "revoke_connection" && !isLowerHex(page.Target, 48) {
		return nil, errors.New("connection revocation requires an exact connection target")
	}
	if tenant == "person" && tenantID == identity.PersonID {
		page.TenantName, page.Role = identity.Speaker.Name, "member"
		if err := validateManagedSignerRequest(page); err != nil {
			return nil, err
		}
		return page, nil
	}
	if tenant != "organization" {
		return nil, errors.New("managed signer tenant does not match this account")
	}
	for _, membership := range memberships {
		if membership != nil && membership.OrganizationID == tenantID && membership.Status == "active" && (membership.Role == getters.OrganizationRoleOwner || membership.Role == getters.OrganizationRoleManager) {
			page.Role = membership.Role
			if membership.Organization != nil {
				page.TenantName = membership.Organization.Name
			}
			if err := validateManagedSignerRequest(page); err != nil {
				return nil, err
			}
			return page, nil
		}
	}
	return nil, errors.New("organization owner or manager access is required")
}

func validateManagedSignerRequest(page *ManagedSignerAuthorizationPage) error {
	if page.Action != "sign" {
		return nil
	}
	if page.Tenant == "person" {
		if page.EventKind != 27235 && page.EventKind != 10008 {
			return errors.New("personal signer cannot authorize this event kind")
		}
		page.ActionLabel = map[int]string{27235: "sign in to an application", 10008: "accept badges on Nostr"}[page.EventKind]
		return nil
	}
	switch page.EventKind {
	case 27235:
		page.ActionLabel = "sign in to an application"
	case 30009:
		page.ActionLabel = "publish a badge definition"
	case 8:
		if page.RecipientCount < 1 || page.RecipientCount > 100 {
			return errors.New("badge issuance must contain between 1 and 100 recipients")
		}
		if page.RecipientCount == 1 {
			page.ActionLabel = "issue one badge award"
		} else {
			page.ActionLabel = "issue " + strconv.Itoa(page.RecipientCount) + " badge awards"
		}
	case 5:
		page.ActionLabel = "revoke a badge award"
	default:
		return errors.New("organization signer cannot authorize this event kind")
	}
	return nil
}

func requireSignerAuthentication(identity *auth.Identity, page *ManagedSignerAuthorizationPage) error {
	strong := page.Action == "create_identity" || page.Action == "import_identity" || page.Action == "export_identity" || page.Action == "rotate_identity" || page.Action == "revoke_connection" || (page.Action == "sign" && (page.EventKind == 5 || page.RecipientCount > 20))
	maxAge := 15 * time.Minute
	if strong {
		maxAge = 5 * time.Minute
	}
	if identity.AuthenticatedAt.IsZero() || time.Since(identity.AuthenticatedAt) > maxAge {
		if strong {
			return errors.New("Sign in again with a passkey or verified Nostr key to approve this sensitive action.")
		}
		return errors.New("Sign in again to approve this action.")
	}
	if strong && identity.Method != auth.MethodPasskey && identity.Method != auth.MethodNostr {
		return errors.New("This action requires a recent passkey or verified Nostr sign-in.")
	}
	return nil
}

func signerAuthMethod(method auth.Method) string {
	if method == auth.MethodEmailLink {
		return "email"
	}
	return string(method)
}

func randomSignerTokenID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func isLowerHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func setManagedSignerHeaders(w http.ResponseWriter, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self' "+ctx.Env.SignerURL)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
}
