package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

const (
	passkeyLoginSessionKey    = "passkey_login_session"
	passkeyLoginNextKey       = "passkey_login_next"
	passkeyRegisterSessionKey = "passkey_register_session"
	passkeyRegisterNameKey    = "passkey_register_name"
)

func PasskeyLoginChallenge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if !allowAuthAttempt(ctx, r, "", 30, time.Minute) {
		writePasskeyError(w, http.StatusTooManyRequests, "Too many passkey attempts. Try again in a minute.")
		return
	}
	rp, err := newWebAuthn(ctx)
	if err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Passkey sign-in is not configured correctly.")
		return
	}
	assertion, session, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		ctx.Err.Printf("begin passkey login: %s", err)
		writePasskeyError(w, http.StatusInternalServerError, "Unable to start passkey sign-in.")
		return
	}
	if err := storePasskeySession(ctx, r, passkeyLoginSessionKey, session); err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Unable to start passkey sign-in.")
		return
	}
	ctx.Session.Put(r.Context(), passkeyLoginNextKey, auth.SafeNext(r.URL.Query().Get("next"), "/dashboard"))
	writePasskeyJSON(w, assertion)
}

func PasskeyLoginVerify(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	session, err := takePasskeySession(ctx, r, passkeyLoginSessionKey)
	next := auth.SafeNext(ctx.Session.GetString(r.Context(), passkeyLoginNextKey), "/dashboard")
	ctx.Session.Remove(r.Context(), passkeyLoginNextKey)
	if err != nil {
		writePasskeyError(w, http.StatusUnauthorized, "That passkey challenge expired or was already used.")
		return
	}
	rp, err := newWebAuthn(ctx)
	if err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Passkey sign-in is not configured correctly.")
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	validatedUser, credential, err := rp.FinishPasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		id, err := uuid.FromBytes(userHandle)
		if err != nil {
			return nil, errors.New("invalid passkey user handle")
		}
		personID := id.String()
		ownerID, err := getters.FindPasskeyOwner(ctx, rawID)
		if err != nil || ownerID == "" || ownerID != personID {
			return nil, errors.New("unknown passkey")
		}
		return auth.LoadPasskeyUser(ctx, personID)
	}, *session, r)
	if err != nil {
		recordAuthAudit(ctx, r, "", string(auth.MethodPasskey), "login_failed", nil)
		writePasskeyError(w, http.StatusUnauthorized, "That passkey could not be verified.")
		return
	}
	user, ok := validatedUser.(*auth.PasskeyUser)
	if !ok || user.PersonID == "" {
		writePasskeyError(w, http.StatusInternalServerError, "Unable to resolve that passkey account.")
		return
	}
	if err := getters.UpdatePersonPasskeyCredentialUse(ctx, user.PersonID, credential); err != nil {
		ctx.Err.Printf("update passkey after login: %s", err)
		writePasskeyError(w, http.StatusInternalServerError, "The passkey was valid, but its security state could not be saved.")
		return
	}
	if err := auth.LoginPerson(ctx, r, user.PersonID, auth.MethodPasskey); err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "The passkey was valid, but the session could not be started.")
		return
	}
	recordAuthAudit(ctx, r, user.PersonID, string(auth.MethodPasskey), "login_succeeded", nil)
	writePasskeyJSON(w, map[string]string{"redirect": next})
}

func PasskeyRegisterChallenge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer, err := auth.Resolve(r, ctx)
	if err != nil || !recentAuthentication(viewer) {
		writePasskeyError(w, http.StatusUnauthorized, "Sign in again before adding a passkey.")
		return
	}
	user, err := auth.LoadPasskeyUser(ctx, viewer.PersonID)
	if err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Unable to load your account for passkey registration.")
		return
	}
	rp, err := newWebAuthn(ctx)
	if err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Passkey registration is not configured correctly.")
		return
	}
	requireResident := true
	selection := protocol.AuthenticatorSelection{
		RequireResidentKey: &requireResident,
		ResidentKey:        protocol.ResidentKeyRequirementRequired,
		UserVerification:   protocol.VerificationRequired,
	}
	creation, session, err := rp.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(selection),
		webauthn.WithExclusions(webauthn.Credentials(user.WebAuthnCredentials()).CredentialDescriptors()),
		webauthn.WithExtensions(protocol.AuthenticationExtensions{"credProps": true}),
	)
	if err != nil {
		ctx.Err.Printf("begin passkey registration: %s", err)
		writePasskeyError(w, http.StatusInternalServerError, "Unable to start passkey registration.")
		return
	}
	if err := storePasskeySession(ctx, r, passkeyRegisterSessionKey, session); err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Unable to start passkey registration.")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "Passkey"
	}
	if len([]rune(name)) > 80 {
		writePasskeyError(w, http.StatusBadRequest, "Passkey names must be no more than 80 characters.")
		return
	}
	ctx.Session.Put(r.Context(), passkeyRegisterNameKey, name)
	writePasskeyJSON(w, creation)
}

func PasskeyRegisterVerify(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer, err := auth.Resolve(r, ctx)
	if err != nil || !recentAuthentication(viewer) {
		clearPasskeyRegistration(ctx, r)
		writePasskeyError(w, http.StatusUnauthorized, "Sign in again before adding a passkey.")
		return
	}
	session, err := takePasskeySession(ctx, r, passkeyRegisterSessionKey)
	name := ctx.Session.GetString(r.Context(), passkeyRegisterNameKey)
	ctx.Session.Remove(r.Context(), passkeyRegisterNameKey)
	if err != nil {
		writePasskeyError(w, http.StatusUnauthorized, "That passkey challenge expired or was already used.")
		return
	}
	user, err := auth.LoadPasskeyUser(ctx, viewer.PersonID)
	if err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Unable to load your account for passkey registration.")
		return
	}
	rp, err := newWebAuthn(ctx)
	if err != nil {
		writePasskeyError(w, http.StatusInternalServerError, "Passkey registration is not configured correctly.")
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	credential, err := rp.FinishRegistration(user, *session, r)
	if err != nil {
		recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodPasskey), "passkey_registration_failed", nil)
		writePasskeyError(w, http.StatusUnauthorized, "That passkey registration could not be verified.")
		return
	}
	stored, err := getters.CreatePersonPasskeyCredential(ctx, viewer.PersonID, name, credential)
	if errors.Is(err, getters.ErrPasskeyCredentialLinked) {
		writePasskeyError(w, http.StatusConflict, "That passkey is already linked to another account.")
		return
	}
	if err != nil {
		ctx.Err.Printf("store passkey: %s", err)
		writePasskeyError(w, http.StatusInternalServerError, "Unable to save that passkey.")
		return
	}
	linkedAt := time.Now().UTC()
	sendAccountSecurityNotice(ctx, viewer.PersonID, identitySecurityEmail(viewer),
		"Passkey added", markdownEmailText(stored.DisplayName)+" was added as a sign-in passkey.", linkedAt)
	recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodPasskey), "passkey_credential_linked", map[string]any{"credential_id": stored.ID})
	writePasskeyJSON(w, map[string]string{"redirect": "/dashboard/settings?flash=Passkey+added."})
}

func PasskeyUnlink(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requirePersonIdentity(w, r, ctx)
	if viewer == nil {
		return
	}
	if !recentAuthentication(viewer) {
		redirectPersonEmails(w, r, "", "Sign in again before removing a passkey.")
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		redirectPersonEmails(w, r, "", "That passkey removal request expired.")
		return
	}
	credential, err := getters.UnlinkPersonPasskeyCredential(ctx, viewer.PersonID, strings.TrimSpace(r.FormValue("credential_id")))
	if err != nil {
		ctx.Err.Printf("unlink passkey: %s", err)
		redirectPersonEmails(w, r, "", "Unable to remove that passkey.")
		return
	}
	if credential == nil {
		redirectPersonEmails(w, r, "", "That passkey is no longer linked.")
		return
	}
	unlinkedAt := time.Now().UTC()
	sendAccountSecurityNotice(ctx, viewer.PersonID, identitySecurityEmail(viewer),
		"Passkey removed", markdownEmailText(credential.DisplayName)+" was removed as a sign-in passkey.", unlinkedAt)
	if err := revokeOtherPersonSessions(ctx, r, viewer.PersonID); err != nil {
		ctx.Err.Printf("passkey unlink session revocation: %s", err)
		auth.Logout(ctx, r)
		redirectPasswordLogin(w, r, "/dashboard/settings", "Passkey removed. Sign in again to continue.")
		return
	}
	recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodPasskey), "passkey_credential_unlinked", map[string]any{"credential_id": credential.ID})
	redirectPersonEmails(w, r, "Passkey removed.", "")
}

func newWebAuthn(ctx *config.AppContext) (*webauthn.WebAuthn, error) {
	origin, err := url.Parse(ctx.Env.GetURI())
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.Hostname() == "" {
		return nil, fmt.Errorf("invalid WebAuthn origin")
	}
	return webauthn.New(&webauthn.Config{
		RPID:          origin.Hostname(),
		RPDisplayName: "bitcoin++",
		RPOrigins:     []string{origin.Scheme + "://" + origin.Host},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute, TimeoutUVD: 5 * time.Minute},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute, TimeoutUVD: 5 * time.Minute},
		},
	})
}

func storePasskeySession(ctx *config.AppContext, r *http.Request, key string, session *webauthn.SessionData) error {
	encoded, err := json.Marshal(session)
	if err != nil {
		return err
	}
	ctx.Session.Put(r.Context(), key, string(encoded))
	return nil
}

func takePasskeySession(ctx *config.AppContext, r *http.Request, key string) (*webauthn.SessionData, error) {
	encoded := ctx.Session.GetString(r.Context(), key)
	ctx.Session.Remove(r.Context(), key)
	if encoded == "" {
		return nil, errors.New("passkey session is missing")
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(encoded), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func clearPasskeyRegistration(ctx *config.AppContext, r *http.Request) {
	ctx.Session.Remove(r.Context(), passkeyRegisterSessionKey)
	ctx.Session.Remove(r.Context(), passkeyRegisterNameKey)
}

func writePasskeyJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writePasskeyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
