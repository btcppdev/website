package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

type OAuthAuthorizePage struct {
	Client            *types.OAuthClient
	Scopes            []string
	ScopeDescriptions map[string]string
	ClientID          string
	RedirectURI       string
	State             string
	CodeChallenge     string
	CSRF              string
	Year              uint
}

type oauthAuthorizationRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

func OAuthServerMetadata(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	base := strings.TrimRight(ctx.Env.GetURI(), "/")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"revocation_endpoint":                   base + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"},
		"scopes_supported":                      append(append([]string{}, types.APITokenScopes...), "offline_access"),
	})
}

func OAuthAuthorize(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	request, client, scopes, ok := validOAuthAuthorizationRequest(w, r, ctx)
	if !ok {
		return
	}
	identity, err := auth.Resolve(r, ctx)
	if err != nil {
		ctx.Err.Printf("OAuth authorize resolve identity: %s", err)
		http.Error(w, "Unable to load your account.", http.StatusInternalServerError)
		return
	}
	if identity == nil || identity.PersonID == "" {
		next := r.URL.RequestURI()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to start authorization.", http.StatusInternalServerError)
		return
	}
	page := &OAuthAuthorizePage{
		Client: client, Scopes: scopes, ScopeDescriptions: oauthScopeDescriptions(),
		ClientID: request.ClientID, RedirectURI: request.RedirectURI, State: request.State,
		CodeChallenge: request.CodeChallenge, CSRF: csrf, Year: helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "oauth_authorize.tmpl", page); err != nil {
		ctx.Err.Printf("render OAuth consent: %s", err)
		http.Error(w, "Unable to render authorization.", http.StatusInternalServerError)
	}
}

func OAuthAuthorizeDecision(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid authorization request.", http.StatusBadRequest)
		return
	}
	request, client, scopes, ok := validOAuthAuthorizationRequest(w, r, ctx)
	if !ok {
		return
	}
	identity, err := auth.Resolve(r, ctx)
	if err != nil || identity == nil || identity.PersonID == "" {
		http.Error(w, "Sign in again to authorize this application.", http.StatusUnauthorized)
		return
	}
	if r.FormValue("decision") != "allow" {
		recordAuthAudit(ctx, r, identity.PersonID, "oauth_server", "oauth_authorization_denied", map[string]any{"client_id": client.ClientID, "scopes": scopes})
		redirectOAuthAuthorization(w, r, request.RedirectURI, request.State, "", "access_denied", ctx.Env.GetURI())
		return
	}
	code, codeHash, err := auth.GenerateOAuthAuthorizationCode()
	if err != nil {
		http.Error(w, "Unable to issue authorization.", http.StatusInternalServerError)
		return
	}
	if err := getters.StoreOAuthAuthorizationCode(ctx, codeHash, &types.OAuthAuthorizationCode{
		ClientDBID: client.ID, PersonID: identity.PersonID, RedirectURI: request.RedirectURI,
		Scopes: scopes, CodeChallenge: request.CodeChallenge,
		ExpiresAt: time.Now().Add(auth.OAuthAuthorizationCodeTTL),
	}); err != nil {
		ctx.Err.Printf("store OAuth authorization code: %s", err)
		http.Error(w, "Unable to issue authorization.", http.StatusInternalServerError)
		return
	}
	if err := getters.UpsertOAuthConsent(ctx, identity.PersonID, client.ID, scopes); err != nil {
		ctx.Err.Printf("store OAuth consent: %s", err)
	}
	recordAuthAudit(ctx, r, identity.PersonID, "oauth_server", "oauth_authorization_approved", map[string]any{"client_id": client.ClientID, "scopes": scopes})
	redirectOAuthAuthorization(w, r, request.RedirectURI, request.State, code, "", ctx.Env.GetURI())
}

func OAuthToken(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := r.ParseForm(); err != nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_request", "The token request could not be parsed.")
		return
	}
	rateSubject := r.FormValue("client_id")
	if basicClient, _, ok := r.BasicAuth(); ok {
		rateSubject = basicClient
	}
	if !allowAuthAttempt(ctx, r, rateSubject, 60, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeOAuthTokenError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Too many token requests. Try again shortly.")
		return
	}
	client, ok := authenticateOAuthClient(r, ctx)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="btcpp-oauth"`)
		writeOAuthTokenError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
		return
	}
	if !setOAuthClientCORS(w, r, client) {
		writeOAuthTokenError(w, http.StatusForbidden, "invalid_client", "The browser origin is not registered for this client.")
		return
	}
	if err := getters.CleanupOAuthServerState(ctx); err != nil {
		ctx.Err.Printf("cleanup OAuth server state: %s", err)
	}
	switch r.FormValue("grant_type") {
	case "authorization_code":
		exchangeOAuthAuthorizationCode(w, r, ctx, client)
	case "refresh_token":
		exchangeOAuthRefreshToken(w, r, ctx, client)
	default:
		writeOAuthTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "Supported grant types are authorization_code and refresh_token.")
	}
}

func exchangeOAuthAuthorizationCode(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, client *types.OAuthClient) {
	code, redirectURI, verifier := r.FormValue("code"), r.FormValue("redirect_uri"), r.FormValue("code_verifier")
	challenge, validVerifier := auth.PKCEChallenge(verifier)
	if code == "" || redirectURI == "" || !validVerifier {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_request", "code, redirect_uri, and a valid PKCE code_verifier are required.")
		return
	}
	access, selector, accessHash, err := auth.GenerateOAuthToken(auth.OAuthAccessTokenVersion)
	if err != nil {
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", "A token could not be issued.")
		return
	}
	refresh, _, refreshHash, err := auth.GenerateOAuthToken(auth.OAuthRefreshTokenVersion)
	if err != nil {
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", "A token could not be issued.")
		return
	}
	codeDigest := sha256.Sum256([]byte(code))
	grant, err := getters.RedeemOAuthAuthorizationCode(ctx, codeDigest[:], client.ID, redirectURI, challenge, getters.OAuthTokenIssue{
		AccessSelector: selector, AccessHash: accessHash, RefreshHash: refreshHash,
		AccessTTL: auth.OAuthAccessTokenTTL, RefreshTTL: auth.OAuthRefreshTokenTTL,
	})
	if err != nil {
		ctx.Err.Printf("redeem OAuth authorization code: %s", err)
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", "The authorization code could not be exchanged.")
		return
	}
	if grant == nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", "The authorization code is invalid, expired, already used, or does not match PKCE.")
		return
	}
	recordAuthAudit(ctx, r, grant.PersonID, "oauth_server", "oauth_access_token_issued", map[string]any{"client_id": client.ClientID, "access_token_id": grant.AccessTokenID, "refresh_token_issued": grant.RefreshTokenID != "", "scopes": grant.Scopes})
	writeOAuthTokenResponse(w, access, refresh, grant)
}

func exchangeOAuthRefreshToken(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, client *types.OAuthClient) {
	_, refreshHash, err := auth.ParseOAuthToken(r.FormValue("refresh_token"), auth.OAuthRefreshTokenVersion)
	if err != nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", "The refresh token is invalid.")
		return
	}
	access, selector, accessHash, err := auth.GenerateOAuthToken(auth.OAuthAccessTokenVersion)
	if err != nil {
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", "A token could not be issued.")
		return
	}
	refresh, _, nextRefreshHash, err := auth.GenerateOAuthToken(auth.OAuthRefreshTokenVersion)
	if err != nil {
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", "A token could not be issued.")
		return
	}
	grant, err := getters.RotateOAuthRefreshToken(ctx, refreshHash, client.ID, getters.OAuthTokenIssue{
		AccessSelector: selector, AccessHash: accessHash, RefreshHash: nextRefreshHash,
		AccessTTL: auth.OAuthAccessTokenTTL, RefreshTTL: auth.OAuthRefreshTokenTTL,
	})
	if err != nil {
		ctx.Err.Printf("rotate OAuth refresh token: %s", err)
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", "The refresh token could not be exchanged.")
		return
	}
	if grant == nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", "The refresh token is invalid, expired, revoked, or was already used.")
		return
	}
	recordAuthAudit(ctx, r, grant.PersonID, "oauth_server", "oauth_refresh_token_rotated", map[string]any{"client_id": client.ClientID, "access_token_id": grant.AccessTokenID, "refresh_token_id": grant.RefreshTokenID})
	writeOAuthTokenResponse(w, access, refresh, grant)
}

func OAuthRevoke(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	rateSubject := r.FormValue("client_id")
	if basicClient, _, ok := r.BasicAuth(); ok {
		rateSubject = basicClient
	}
	if !allowAuthAttempt(ctx, r, rateSubject, 60, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeOAuthTokenError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Too many revocation requests. Try again shortly.")
		return
	}
	client, ok := authenticateOAuthClient(r, ctx)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="btcpp-oauth"`)
		writeOAuthTokenError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
		return
	}
	if !setOAuthClientCORS(w, r, client) {
		writeOAuthTokenError(w, http.StatusForbidden, "invalid_client", "The browser origin is not registered for this client.")
		return
	}
	raw := r.FormValue("token")
	if selector, digest, err := auth.ParseOAuthToken(raw, auth.OAuthAccessTokenVersion); err == nil {
		_ = getters.RevokeOAuthAccessToken(ctx, selector, digest, client.ID)
	} else if _, digest, err := auth.ParseOAuthToken(raw, auth.OAuthRefreshTokenVersion); err == nil {
		_ = getters.RevokeOAuthRefreshToken(ctx, digest, client.ID)
	}
	w.WriteHeader(http.StatusOK)
}

func OAuthTokenPreflight(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	clients, err := getters.ListOAuthClients(ctx)
	if err != nil {
		http.Error(w, "Unable to validate OAuth origin.", http.StatusInternalServerError)
		return
	}
	for _, client := range clients {
		if client != nil && client.RevokedAt == nil && oauthClientAllowsOrigin(client, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Set("Vary", "Origin")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, "OAuth origin is not registered.", http.StatusForbidden)
}

func setOAuthClientCORS(w http.ResponseWriter, r *http.Request, client *types.OAuthClient) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if !oauthClientAllowsOrigin(client, origin) {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	return true
}

func oauthClientAllowsOrigin(client *types.OAuthClient, origin string) bool {
	wanted, err := url.Parse(origin)
	if err != nil || (wanted.Scheme != "https" && wanted.Scheme != "http") || wanted.Host == "" || wanted.Path != "" {
		return false
	}
	for _, raw := range client.RedirectURIs {
		redirect, err := url.Parse(raw)
		if err == nil && redirect.Scheme == wanted.Scheme && redirect.Host == wanted.Host {
			return true
		}
	}
	return false
}

func DashboardOAuthClientCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requireGlobalAdmin(w, r, ctx)
	if viewer == nil {
		return
	}
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form submission.", http.StatusBadRequest)
		return
	}
	if !recentAuthentication(viewer) {
		http.Redirect(w, r, "/dashboard/settings?error="+url.QueryEscape("Sign in again before registering an OAuth application."), http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	redirects, validRedirects := parseOAuthRedirectURIs(r.FormValue("redirect_uris"))
	scopes := r.Form["scopes"]
	allowedScopes, validScopes := allowedOAuthScopes(strings.Join(scopes, " "), append(append([]string{}, types.APITokenScopes...), "offline_access"))
	confidential := r.FormValue("client_type") == "confidential"
	if name == "" || len(name) > 100 || !validRedirects || !validScopes {
		http.Redirect(w, r, "/dashboard/settings?error="+url.QueryEscape("Application name, valid redirect URIs, and at least one valid scope are required."), http.StatusSeeOther)
		return
	}
	clientID, secret, secretHash, err := auth.GenerateOAuthClientCredentials(confidential)
	if err != nil {
		http.Error(w, "Unable to register OAuth application.", http.StatusInternalServerError)
		return
	}
	method := "none"
	if confidential {
		method = "client_secret_basic"
	}
	client := &types.OAuthClient{
		ClientID: clientID, ClientSecretHash: secretHash, Name: name,
		RedirectURIs: redirects, AllowedScopes: allowedScopes,
		TokenEndpointAuthMethod: method, CreatedByPersonID: viewer.PersonID,
	}
	if err := getters.CreateOAuthClient(ctx, client); err != nil {
		ctx.Err.Printf("create OAuth client: %s", err)
		http.Error(w, "Unable to register OAuth application.", http.StatusInternalServerError)
		return
	}
	_ = getters.RecordAuthAuditEvent(ctx, &types.AuthAuditEvent{
		PersonID: viewer.PersonID, Method: "session", Event: "oauth_client_created",
		RemoteAddress: r.RemoteAddr, UserAgent: r.UserAgent(), Metadata: map[string]any{"client_id": client.ID, "public_client": !confidential},
	})
	renderAccountSettings(w, r, ctx, viewer, "", clientID, secret)
}

func DashboardOAuthConsentRevoke(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requirePersonIdentity(w, r, ctx)
	if viewer == nil {
		return
	}
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form submission.", http.StatusBadRequest)
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if clientID == "" {
		http.Error(w, "OAuth application is required.", http.StatusBadRequest)
		return
	}
	if err := getters.RevokePersonOAuthConsent(ctx, viewer.PersonID, clientID); err != nil {
		ctx.Err.Printf("revoke OAuth consent: %s", err)
		http.Error(w, "Unable to revoke application access.", http.StatusInternalServerError)
		return
	}
	_ = getters.RecordAuthAuditEvent(ctx, &types.AuthAuditEvent{
		PersonID: viewer.PersonID, Method: "session", Event: "oauth_consent_revoked",
		RemoteAddress: r.RemoteAddr, UserAgent: r.UserAgent(), Metadata: map[string]any{"client_id": clientID},
	})
	http.Redirect(w, r, "/dashboard/settings?flash="+url.QueryEscape("Application access revoked. Its existing tokens no longer work."), http.StatusSeeOther)
}

func DashboardOAuthClientRevoke(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requireGlobalAdmin(w, r, ctx)
	if viewer == nil {
		return
	}
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form submission.", http.StatusBadRequest)
		return
	}
	if !recentAuthentication(viewer) {
		http.Redirect(w, r, "/dashboard/settings?error="+url.QueryEscape("Sign in again before revoking an OAuth application."), http.StatusSeeOther)
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if clientID == "" || getters.RevokeOAuthClient(ctx, clientID) != nil {
		http.Error(w, "Unable to revoke OAuth application.", http.StatusInternalServerError)
		return
	}
	_ = getters.RecordAuthAuditEvent(ctx, &types.AuthAuditEvent{
		PersonID: viewer.PersonID, Method: "session", Event: "oauth_client_revoked",
		RemoteAddress: r.RemoteAddr, UserAgent: r.UserAgent(), Metadata: map[string]any{"client_id": clientID},
	})
	http.Redirect(w, r, "/dashboard/settings?flash="+url.QueryEscape("OAuth application revoked. Its access and refresh tokens no longer work."), http.StatusSeeOther)
}

func parseOAuthRedirectURIs(raw string) ([]string, bool) {
	lines := strings.FieldsFunc(raw, func(char rune) bool { return char == '\n' || char == '\r' })
	redirects := make([]string, 0, len(lines))
	seen := map[string]bool{}
	for _, line := range lines {
		value := strings.TrimSpace(line)
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme == "" || parsed.Fragment != "" || parsed.User != nil || seen[value] {
			return nil, false
		}
		if parsed.Scheme == "https" {
			if parsed.Host == "" {
				return nil, false
			}
		} else if parsed.Scheme == "http" {
			host := strings.ToLower(parsed.Hostname())
			if host != "localhost" && host != "127.0.0.1" && host != "::1" {
				return nil, false
			}
		} else if parsed.Opaque == "" && parsed.Host == "" && parsed.Path == "" {
			return nil, false
		}
		seen[value] = true
		redirects = append(redirects, value)
	}
	return redirects, len(redirects) > 0 && len(redirects) <= 10
}

func validOAuthAuthorizationRequest(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (oauthAuthorizationRequest, *types.OAuthClient, []string, bool) {
	request := oauthAuthorizationRequest{
		ClientID: r.FormValue("client_id"), RedirectURI: r.FormValue("redirect_uri"),
		ResponseType: r.FormValue("response_type"), Scope: r.FormValue("scope"),
		State: r.FormValue("state"), CodeChallenge: r.FormValue("code_challenge"),
		CodeChallengeMethod: r.FormValue("code_challenge_method"),
	}
	client, err := getters.GetOAuthClient(ctx, request.ClientID)
	if err != nil {
		http.Error(w, "Unable to validate OAuth client.", http.StatusInternalServerError)
		return request, nil, nil, false
	}
	if client == nil || !oauthContainsString(client.RedirectURIs, request.RedirectURI) {
		http.Error(w, "Unknown OAuth client or redirect URI.", http.StatusBadRequest)
		return request, nil, nil, false
	}
	if request.ResponseType != "code" || request.State == "" || request.CodeChallengeMethod != "S256" || !auth.ValidPKCEChallenge(request.CodeChallenge) {
		redirectOAuthAuthorization(w, r, request.RedirectURI, request.State, "", "invalid_request", ctx.Env.GetURI())
		return request, nil, nil, false
	}
	scopes, ok := allowedOAuthScopes(request.Scope, client.AllowedScopes)
	if !ok {
		redirectOAuthAuthorization(w, r, request.RedirectURI, request.State, "", "invalid_scope", ctx.Env.GetURI())
		return request, nil, nil, false
	}
	return request, client, scopes, true
}

func allowedOAuthScopes(raw string, allowed []string) ([]string, bool) {
	requested := strings.Fields(raw)
	if len(requested) == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	for _, scope := range requested {
		if seen[scope] || !oauthContainsString(allowed, scope) {
			return nil, false
		}
		seen[scope] = true
	}
	return requested, true
}

func authenticateOAuthClient(r *http.Request, ctx *config.AppContext) (*types.OAuthClient, bool) {
	clientID, secret, basic := r.BasicAuth()
	if !basic {
		clientID = r.FormValue("client_id")
	}
	client, err := getters.GetOAuthClient(ctx, clientID)
	if err != nil || client == nil {
		return nil, false
	}
	if client.TokenEndpointAuthMethod == "none" {
		return client, !basic && secret == ""
	}
	if !basic || secret == "" {
		return nil, false
	}
	digest := sha256.Sum256([]byte(secret))
	return client, subtle.ConstantTimeCompare(digest[:], client.ClientSecretHash) == 1
}

func redirectOAuthAuthorization(w http.ResponseWriter, r *http.Request, redirectURI, state, code, oauthError, issuer string) {
	target, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "Invalid redirect URI.", http.StatusBadRequest)
		return
	}
	query := target.Query()
	if code != "" {
		query.Set("code", code)
	}
	if oauthError != "" {
		query.Set("error", oauthError)
	}
	if state != "" {
		query.Set("state", state)
	}
	if issuer != "" {
		query.Set("iss", strings.TrimRight(issuer, "/"))
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func writeOAuthTokenResponse(w http.ResponseWriter, access, refresh string, grant *getters.OAuthTokenGrant) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	response := map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": int(auth.OAuthAccessTokenTTL.Seconds()),
		"scope": strings.Join(grant.Scopes, " "),
	}
	if grant.RefreshTokenID != "" {
		response["refresh_token"] = refresh
	}
	_ = json.NewEncoder(w).Encode(response)
}

func writeOAuthTokenError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

func oauthScopeDescriptions() map[string]string {
	return map[string]string{
		"identity:self:read": "View your account identity and current roles",
		"profile:self:read":  "View your bitcoin++ profile and verified email addresses",
		"profile:self:write": "Update your public bitcoin++ profile",
		"talks:read":         "View your submitted and accepted talks",
		"talks:write":        "Edit talks you speak at",
		"schedule:write":     "Update event schedules when your current admin role permits it",
		"recordings:write":   "Manage recording metadata when your current admin role permits it",
		"offline_access":     "Stay connected when you are not actively using the app",
	}
}

func oauthContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
