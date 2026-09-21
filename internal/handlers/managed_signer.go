package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

const managedSignerPendingAuthorizationKey = "managed_signer_pending_authorization"

type ManagedSignerAuthorizationPage struct {
	ContentTitle      string
	ContentIdentifier string
	ContentBody       string
	ContentTags       [][]string
	StreamStatus      string
	PersonName        string
	Tenant            string
	TenantID          string
	TenantName        string
	Role              string
	Action            string
	ActionLabel       string
	ApplicationURL    string
	ApplicationName   string
	ClientPubkey      string
	Permissions       []string
	RequestMethod     string
	HTTPURL           string
	HTTPMethod        string
	BadgeIdentifier   string
	BadgeName         string
	BadgeImage        string
	BadgeDescription  string
	AwardRecipient    string
	RevocationEventID string
	RevocationReason  string
	ProfileBadgeCount int
	EventHash         string
	Target            string
	EventKind         int
	RecipientCount    int
	VerifiedGrants    int
	BadgeAddress      string
	BatchRecipients   []ManagedSignerBatchRecipient
	CSRF              string
	ReturnTo          string
	Token             string
	Error             string
	Year              uint
}

type ManagedSignerBatchRecipient struct {
	Pubkey            string `json:"pubkey"`
	PersonID          string `json:"person_id"`
	GrantID           string `json:"grant_id"`
	SubjectProfileURL string `json:"subject_profile_url"`
}

type managedSignerRequestReview struct {
	ContentTitle      string                        `json:"content_title"`
	ContentIdentifier string                        `json:"content_identifier"`
	ContentBody       string                        `json:"content_body"`
	ContentTags       [][]string                    `json:"content_tags"`
	StreamStatus      string                        `json:"stream_status"`
	ID                string                        `json:"id"`
	Tenant            string                        `json:"tenant"`
	TenantID          string                        `json:"tenant_id"`
	ClientPubkey      string                        `json:"client_pubkey"`
	Method            string                        `json:"method"`
	EventHash         string                        `json:"event_hash"`
	EventKind         int                           `json:"event_kind"`
	RecipientCount    int                           `json:"recipient_count"`
	Permissions       []string                      `json:"permissions"`
	ApplicationName   string                        `json:"application_name"`
	ApplicationURL    string                        `json:"application_url"`
	HTTPURL           string                        `json:"http_url"`
	HTTPMethod        string                        `json:"http_method"`
	BadgeIdentifier   string                        `json:"badge_identifier"`
	BadgeName         string                        `json:"badge_name"`
	BadgeImage        string                        `json:"badge_image"`
	BadgeDescription  string                        `json:"badge_description"`
	BadgeAddress      string                        `json:"badge_address"`
	AwardRecipient    string                        `json:"award_recipient"`
	RevocationEventID string                        `json:"revocation_event_id"`
	RevocationReason  string                        `json:"revocation_reason"`
	ProfileBadgeCount int                           `json:"profile_badge_count"`
	Recipients        []ManagedSignerBatchRecipient `json:"recipients"`
}

type managedSignerPendingAuthorization struct {
	ID        string              `json:"id"`
	PersonID  string              `json:"person_id"`
	Values    map[string][]string `json:"values"`
	ExpiresAt time.Time           `json:"expires_at"`
}

var loadManagedSignerBadgeGrant = getters.GetBadgeGrant
var loadManagedSignerSubjectProfileURL = currentManagedSignerSubjectProfileURL

func currentManagedSignerSubjectProfileURL(ctx *config.AppContext, personID string) (string, error) {
	person, err := getters.FetchSpeakerByID(ctx, personID)
	if err != nil || person == nil {
		return "", err
	}
	if publicID, public := resolvedWhoIsPublicID(ctx, person); public {
		return strings.TrimRight(ctx.Env.GetURI(), "/") + "/whois/" + url.PathEscape(publicID), nil
	}
	return "", nil
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
		http.Redirect(w, r, managedSignerDeniedURL(ctx.Env.SignerURL, page), http.StatusSeeOther)
		return
	}
	if err := requireSignerAuthentication(identity, page); err != nil {
		pendingID, pendingErr := storePendingSignerAuthorization(ctx, r, identity.PersonID)
		if pendingErr != nil {
			ctx.Err.Printf("store pending managed signer authorization: %s", pendingErr)
			page.Error, page.CSRF, page.Year = err.Error(), r.FormValue("csrf"), helpers.CurrentYear()
			w.WriteHeader(http.StatusForbidden)
			_ = ctx.TemplateCache.ExecuteTemplate(w, "managed_signer_authorize.tmpl", page)
			return
		}
		resume := "/signer/authorize/resume?id=" + url.QueryEscape(pendingID)
		http.Redirect(w, r, reauthenticationURL(resume, signerActionRequiresStrongAuthentication(page)), http.StatusSeeOther)
		return
	}
	renderManagedSignerAuthorization(w, r, ctx, identity, page)
}

// ManagedSignerAuthorizeResume consumes the exact authorization the person
// already approved before step-up authentication. It is short-lived, bound to
// the same person and session, and re-runs membership and batch validation.
func ManagedSignerAuthorizeResume(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	setManagedSignerHeaders(w, ctx)
	pending, err := takePendingSignerAuthorization(ctx, r, strings.TrimSpace(r.URL.Query().Get("id")))
	if err != nil {
		http.Error(w, "That signer authorization expired. Return to the signer and try again.", http.StatusBadRequest)
		return
	}
	identity, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok || identity == nil {
		return
	}
	if pending.PersonID != identity.PersonID {
		http.Error(w, "Sign in with the same bitcoin++ account that approved this request.", http.StatusForbidden)
		return
	}
	resumed := r.Clone(r.Context())
	resumed.Method = http.MethodPost
	resumed.Form = url.Values(pending.Values)
	resumed.PostForm = url.Values(pending.Values)
	page, err := managedSignerAuthorizationPage(resumed, ctx, identity, memberships)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := requireSignerAuthentication(identity, page); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	renderManagedSignerAuthorization(w, resumed, ctx, identity, page)
}

func renderManagedSignerAuthorization(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, identity *auth.Identity, page *ManagedSignerAuthorizationPage) {
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

func storePendingSignerAuthorization(ctx *config.AppContext, r *http.Request, personID string) (string, error) {
	id, err := randomSignerTokenID()
	if err != nil {
		return "", err
	}
	values := make(map[string][]string)
	for _, key := range []string{"return_to", "tenant", "tenant_id", "action", "event_hash", "target", "event_kind", "recipient_count"} {
		if value := strings.TrimSpace(r.FormValue(key)); value != "" {
			values[key] = []string{value}
		}
	}
	pending := managedSignerPendingAuthorization{ID: id, PersonID: personID, Values: values, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	encoded, err := json.Marshal(pending)
	if err != nil {
		return "", err
	}
	ctx.Session.Put(r.Context(), managedSignerPendingAuthorizationKey, string(encoded))
	return id, nil
}

func takePendingSignerAuthorization(ctx *config.AppContext, r *http.Request, id string) (*managedSignerPendingAuthorization, error) {
	encoded := ctx.Session.GetString(r.Context(), managedSignerPendingAuthorizationKey)
	ctx.Session.Remove(r.Context(), managedSignerPendingAuthorizationKey)
	var pending managedSignerPendingAuthorization
	if encoded == "" || json.Unmarshal([]byte(encoded), &pending) != nil || id == "" || !secureTokenEqual(pending.ID, id) || pending.PersonID == "" || time.Now().UTC().After(pending.ExpiresAt) {
		return nil, errors.New("pending signer authorization is missing or expired")
	}
	return &pending, nil
}

func managedSignerAuthorizationPage(r *http.Request, ctx *config.AppContext, identity *auth.Identity, memberships []*types.OrganizationMembership) (*ManagedSignerAuthorizationPage, error) {
	returnTo, err := managedSignerReturnURL(ctx.Env.SignerURL, r.FormValue("return_to"))
	if err != nil {
		return nil, errors.New("invalid managed signer return URL")
	}
	action := strings.TrimSpace(r.FormValue("action"))
	switch action {
	case "connect", "connect_login", "create_identity", "import_identity", "export_identity", "protect_identity", "recover_identity", "create_unlock_enrollment", "accept_unlock_enrollment", "rotate_identity", "revoke_connection", "sign":
	default:
		return nil, errors.New("unsupported managed signer action")
	}
	tenant, tenantID := strings.TrimSpace(r.FormValue("tenant")), strings.TrimSpace(r.FormValue("tenant_id"))
	page := &ManagedSignerAuthorizationPage{PersonName: identity.Speaker.Name, Tenant: tenant, TenantID: tenantID, Action: action, ActionLabel: strings.ReplaceAll(action, "_", " "), EventHash: strings.TrimSpace(r.FormValue("event_hash")), Target: strings.TrimSpace(r.FormValue("target")), ReturnTo: returnTo}
	if action == "create_identity" {
		page.ActionLabel = "create a new Nostr signer"
	}
	if action == "connect_login" {
		page.ActionLabel = "connect Badge Studio and sign in"
		page.ApplicationURL = strings.TrimRight(ctx.Env.BadgeStudioURL, "/") + "/api/auth/session"
		if strings.TrimSpace(ctx.Env.BadgeStudioURL) == "" {
			return nil, errors.New("Badge Studio connection is not configured")
		}
	}
	if action == "protect_identity" {
		page.ActionLabel = "add a two-factor unlock passphrase"
	}
	if action == "recover_identity" {
		page.ActionLabel = "recover an identity with its original nsec"
	}
	if action == "create_unlock_enrollment" {
		page.ActionLabel = "invite another manager to unlock this signer"
	}
	if action == "accept_unlock_enrollment" {
		page.ActionLabel = "enroll this manager to unlock the organization signer"
	}
	page.EventKind, _ = strconv.Atoi(r.FormValue("event_kind"))
	page.RecipientCount, _ = strconv.Atoi(r.FormValue("recipient_count"))
	if err := validateManagedSignerBinding(page); err != nil {
		return nil, err
	}
	if tenant == "person" && tenantID == identity.PersonID {
		page.TenantName, page.Role = identity.Speaker.Name, "member"
		if err := validateManagedSignerRequest(page); err != nil {
			return nil, err
		}
		if err := validateManagedBadgeBatch(r, ctx, page); err != nil {
			return nil, err
		}
		return page, nil
	}
	if tenant != "organization" {
		return nil, errors.New("managed signer tenant does not match this account")
	}
	for _, membership := range memberships {
		if membership != nil && membership.OrganizationID == tenantID && membership.Status == "active" && (membership.Role == getters.OrganizationRoleOwner || membership.Role == getters.OrganizationRoleManager) {
			if membership.Role != getters.OrganizationRoleOwner && (action == "create_identity" || action == "import_identity" || action == "rotate_identity" || action == "export_identity") {
				return nil, errors.New("Only organization owners can create, import, rotate, or export keys.")
			}
			page.Role = membership.Role
			if membership.Organization != nil {
				page.TenantName = membership.Organization.Name
			}
			if err := validateManagedSignerRequest(page); err != nil {
				return nil, err
			}
			if err := validateManagedBadgeBatch(r, ctx, page); err != nil {
				return nil, err
			}
			return page, nil
		}
	}
	return nil, errors.New("organization owner or manager access is required")
}

func managedSignerReturnURL(signerURL, raw string) (string, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(signerURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", errors.New("managed signer is not configured")
	}
	candidate, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || candidate.Scheme != base.Scheme || candidate.Host != base.Host || candidate.User != nil || candidate.Path != "/api/authorizations/callback" || candidate.Fragment != "" {
		return "", errors.New("invalid managed signer callback")
	}
	query := candidate.Query()
	state := query.Get("state")
	if len(query) != 1 || len(query["state"]) != 1 || !isLowerHex(state, 64) {
		return "", errors.New("managed signer callback has no browser state")
	}
	return candidate.String(), nil
}

func validateManagedSignerBinding(page *ManagedSignerAuthorizationPage) error {
	if page.Action == "sign" && !isLowerHex(page.EventHash, 64) {
		return errors.New("signing authorization requires an exact event hash")
	}
	if page.Action == "sign" && !isLowerHex(page.Target, 48) {
		return errors.New("signer authorization requires an exact pending request")
	}
	if page.Action == "connect" && (page.EventHash != "" || page.Target != "") && (!isLowerHex(page.EventHash, 64) || !isLowerHex(page.Target, 48)) {
		return errors.New("application connection requires an exact pending request")
	}
	if page.Action == "connect_login" && (!isLowerHex(page.EventHash, 64) || !isLowerHex(page.Target, 48) || page.EventKind != 27235 || page.RecipientCount != 0) {
		return errors.New("Badge Studio connection and login requires an exact pending request")
	}
	if page.Action == "revoke_connection" && !isLowerHex(page.Target, 48) {
		return errors.New("connection revocation requires an exact connection target")
	}
	if page.Action == "accept_unlock_enrollment" && !isLowerHex(page.Target, 48) {
		return errors.New("manager enrollment requires an exact invitation target")
	}
	return nil
}

func validateManagedBadgeBatch(r *http.Request, ctx *config.AppContext, page *ManagedSignerAuthorizationPage) error {
	if page.Target == "" || (page.Action != "sign" && page.Action != "connect" && page.Action != "connect_login") {
		return nil
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(ctx.Env.SignerURL, "/")+"/api/authorizations/requests/"+page.Target, nil)
	if err != nil {
		return errors.New("signer request review is unavailable")
	}
	client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("the managed signer could not provide this request for review")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return errors.New("the managed signer no longer has this request")
	}
	var review managedSignerRequestReview
	decoder := json.NewDecoder(io.LimitReader(response.Body, 256<<10))
	if decoder.Decode(&review) != nil || review.ID != page.Target || review.Tenant != page.Tenant || review.TenantID != page.TenantID || review.EventHash != page.EventHash || review.ClientPubkey == "" {
		return errors.New("signer request review does not match the requested authorization")
	}
	if page.Action == "connect" || page.Action == "connect_login" {
		if review.Method != "connect" || len(review.Permissions) == 0 {
			return errors.New("application connection review is incomplete")
		}
		page.ApplicationName, page.ApplicationURL, page.ClientPubkey, page.Permissions, page.RequestMethod = review.ApplicationName, review.ApplicationURL, review.ClientPubkey, review.Permissions, review.Method
		return nil
	}
	if (review.Method != "sign_event" && review.Method != "sign_event_batch") || review.EventKind != page.EventKind || review.RecipientCount != page.RecipientCount {
		return errors.New("signed event review does not match the requested authorization")
	}
	page.ContentTitle, page.ContentIdentifier, page.ContentBody, page.ContentTags, page.StreamStatus = review.ContentTitle, review.ContentIdentifier, review.ContentBody, review.ContentTags, review.StreamStatus
	page.ClientPubkey, page.RequestMethod = review.ClientPubkey, review.Method
	page.HTTPURL, page.HTTPMethod = review.HTTPURL, review.HTTPMethod
	page.BadgeIdentifier, page.BadgeName, page.BadgeImage, page.BadgeDescription = review.BadgeIdentifier, review.BadgeName, review.BadgeImage, review.BadgeDescription
	page.BadgeAddress, page.AwardRecipient = review.BadgeAddress, review.AwardRecipient
	page.RevocationEventID, page.RevocationReason, page.ProfileBadgeCount = review.RevocationEventID, review.RevocationReason, review.ProfileBadgeCount
	if review.Method != "sign_event_batch" {
		return nil
	}
	if review.EventKind != 8 || len(review.Recipients) != page.RecipientCount {
		return errors.New("badge batch review does not match the requested authorization")
	}
	parts := strings.SplitN(review.BadgeAddress, ":", 3)
	if len(parts) != 3 || parts[0] != "30009" || !isLowerHex(parts[1], 64) || strings.TrimSpace(parts[2]) == "" {
		return errors.New("badge batch uses an invalid badge definition")
	}
	verified := 0
	seen := make(map[string]struct{}, len(review.Recipients))
	for _, recipient := range review.Recipients {
		if !isLowerHex(recipient.Pubkey, 64) {
			return errors.New("badge batch contains an invalid recipient")
		}
		if _, duplicate := seen[recipient.Pubkey]; duplicate {
			return errors.New("badge batch contains a duplicate recipient")
		}
		seen[recipient.Pubkey] = struct{}{}
		if recipient.GrantID == "" {
			continue
		}
		grant, err := loadManagedSignerBadgeGrant(ctx, recipient.GrantID)
		if err != nil || grant == nil {
			return fmt.Errorf("Bitcoin++ badge grant %s was not found", recipient.GrantID)
		}
		expectedProfileURL, personErr := loadManagedSignerSubjectProfileURL(ctx, grant.RecipientPersonID)
		if personErr != nil {
			return fmt.Errorf("Bitcoin++ badge grant %s recipient was not found", recipient.GrantID)
		}
		if grant.OrganizationID != page.TenantID || grant.IssuerPubkey != parts[1] || grant.BadgeIdentifier != parts[2] || grant.RecipientPubkey != recipient.Pubkey || grant.RecipientPersonID != recipient.PersonID || expectedProfileURL != recipient.SubjectProfileURL || (grant.State != getters.BadgeGrantStateReady && grant.State != getters.BadgeGrantStateDeliveryError) {
			return fmt.Errorf("Bitcoin++ badge grant %s no longer matches this award", recipient.GrantID)
		}
		verified++
	}
	page.BadgeAddress, page.BatchRecipients, page.VerifiedGrants = review.BadgeAddress, review.Recipients, verified
	return nil
}

func validateManagedSignerRequest(page *ManagedSignerAuthorizationPage) error {
	if page.Action == "rotate_identity" && page.Tenant != "organization" {
		return errors.New("personal Nostr identities cannot be transparently rotated")
	}
	if (page.Action == "create_unlock_enrollment" || page.Action == "accept_unlock_enrollment") && page.Tenant != "organization" {
		return errors.New("manager unlock enrollment is only available for organizations")
	}
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
	case 1:
		page.ActionLabel = "publish a note, reply, or announcement"
	case 30023:
		page.ActionLabel = "publish or update a long-form article"
	case 30311:
		page.ActionLabel = "publish or update a live stream"
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
	strong := signerActionRequiresStrongAuthentication(page)
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

func signerActionRequiresStrongAuthentication(page *ManagedSignerAuthorizationPage) bool {
	return page != nil && (page.Action == "connect" || page.Action == "connect_login" || page.Action == "create_identity" || page.Action == "import_identity" || page.Action == "export_identity" || page.Action == "protect_identity" || page.Action == "recover_identity" || page.Action == "create_unlock_enrollment" || page.Action == "accept_unlock_enrollment" || page.Action == "rotate_identity" || page.Action == "revoke_connection" || (page.Action == "sign" && (page.EventKind == 27235 || page.EventKind == 5 || page.RecipientCount > 20)))
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

// Preserve the validated vault context even when another tab last selected a
// different vault. Cancellation never carries an authorization or action handle.
func managedSignerDeniedURL(signerURL string, page *ManagedSignerAuthorizationPage) string {
	query := url.Values{"authorization": {"denied"}, "tenant": {page.Tenant}, "tenant_id": {page.TenantID}}
	return strings.TrimRight(signerURL, "/") + "/?" + query.Encode()
}

func setManagedSignerHeaders(w http.ResponseWriter, ctx *config.AppContext) {
	ancestor := managedSignerFrameAncestor(ctx.Env.BadgeStudioURL)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors "+ancestor+"; form-action 'self' "+ctx.Env.SignerURL)
	// The cross-origin callback is a form POST: no-referrer would make its
	// Origin header null, which the signer must reject. Send only the origin,
	// never the authorization URL path or query, and suppress HTTPS downgrades.
	w.Header().Set("Referrer-Policy", "strict-origin")
	if ancestor == "'none'" {
		w.Header().Set("X-Frame-Options", "DENY")
	} else {
		w.Header().Del("X-Frame-Options")
	}
}

// Only the configured Badge Studio origin may host signer consent.
func managedSignerFrameAncestor(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || strings.ContainsAny(u.Host, "*; \t\r\n") ||
		(u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "localhost")) {
		return "'none'"
	}
	return u.Scheme + "://" + u.Host
}
