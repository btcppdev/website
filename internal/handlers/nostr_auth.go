package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"

	"github.com/nbd-wtf/go-nostr"
)

const (
	nostrAuthKind = 27235
	nostrAuthTTL  = 5 * time.Minute

	nostrChallengeKey = "nostr_auth_challenge"
	nostrStartedAtKey = "nostr_auth_started_at"
	nostrNextKey      = "nostr_auth_next"
)

type nostrChallengeResponse struct {
	Challenge string `json:"challenge"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Kind      int    `json:"kind"`
	CreatedAt int64  `json:"created_at"`
}

type nostrVerifyRequest struct {
	Event nostr.Event `json:"event"`
}

func NostrChallenge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if !allowAuthAttempt(ctx, r, "", 30, time.Minute) {
		writeNostrAuthError(w, http.StatusTooManyRequests, "Too many Nostr sign-in attempts. Try again in a minute.")
		return
	}
	startNostrChallenge(w, r, ctx, auth.SafeNext(r.URL.Query().Get("next"), "/dashboard"), nostrVerifyURL(ctx))
}

func NostrLinkChallenge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if !allowAuthAttempt(ctx, r, "", 30, time.Minute) {
		writeNostrAuthError(w, http.StatusTooManyRequests, "Too many Nostr linking attempts. Try again in a minute.")
		return
	}
	viewer, err := auth.Resolve(r, ctx)
	if err != nil || !recentAuthentication(viewer) {
		writeNostrAuthError(w, http.StatusUnauthorized, "Sign in again before linking a Nostr key.")
		return
	}
	startNostrChallenge(w, r, ctx, "/dashboard/settings", nostrLinkVerifyURL(ctx))
}

func startNostrChallenge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, next, verifyURL string) {
	challenge, err := randomAuthToken()
	if err != nil {
		ctx.Err.Printf("Nostr auth challenge: %s", err)
		http.Error(w, "Unable to start Nostr sign-in", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	clearNostrChallenge(ctx, r)
	ctx.Session.Put(r.Context(), nostrChallengeKey, challenge)
	ctx.Session.Put(r.Context(), nostrStartedAtKey, now.Format(time.RFC3339Nano))
	ctx.Session.Put(r.Context(), nostrNextKey, auth.SafeNext(next, "/dashboard"))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(nostrChallengeResponse{
		Challenge: challenge, URL: verifyURL, Method: http.MethodPost,
		Kind: nostrAuthKind, CreatedAt: now.Unix(),
	})
}

func NostrVerify(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	challenge := ctx.Session.GetString(r.Context(), nostrChallengeKey)
	startedAtRaw := ctx.Session.GetString(r.Context(), nostrStartedAtKey)
	next := auth.SafeNext(ctx.Session.GetString(r.Context(), nostrNextKey), "/dashboard")
	clearNostrChallenge(ctx, r)
	startedAt, startedErr := time.Parse(time.RFC3339Nano, startedAtRaw)
	age := time.Since(startedAt)
	if challenge == "" || startedErr != nil || age < -time.Minute || age > nostrAuthTTL {
		writeNostrAuthError(w, http.StatusUnauthorized, "That Nostr sign-in challenge expired or was already used. Try again.")
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	var input nostrVerifyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeNostrAuthError(w, http.StatusBadRequest, "The Nostr signer returned an invalid event.")
		return
	}
	event := input.Event
	if err := validateNostrAuthEvent(event, challenge, nostrVerifyURL(ctx), time.Now()); err != nil {
		writeNostrAuthError(w, http.StatusUnauthorized, err.Error())
		return
	}

	credential, err := getters.FindNostrCredentialByPubkey(ctx, event.PubKey)
	if errors.Is(err, getters.ErrNostrPubkeyConflict) {
		writeNostrAuthError(w, http.StatusConflict, "That Nostr key is attached to multiple profiles. An administrator must merge them before it can sign in.")
		return
	}
	if err != nil {
		ctx.Err.Printf("Nostr profile lookup: %s", err)
		writeNostrAuthError(w, http.StatusInternalServerError, "Unable to resolve that Nostr profile.")
		return
	}
	if credential == nil {
		writeNostrAuthError(w, http.StatusUnauthorized, "That Nostr key is not linked to a bitcoin++ account yet. Sign in another way and link it in Account settings.")
		return
	}
	person, err := getters.FetchSpeakerByID(ctx, credential.PersonID)
	if err != nil || person == nil {
		ctx.Err.Printf("Nostr credential person lookup: %s", err)
		writeNostrAuthError(w, http.StatusInternalServerError, "Unable to resolve that Nostr account.")
		return
	}
	if err := getters.VerifyNostrCredential(ctx, credential.ID, person.ID, event.PubKey); err != nil {
		if errors.Is(err, getters.ErrNostrPubkeyConflict) {
			writeNostrAuthError(w, http.StatusConflict, "That Nostr key is attached to multiple accounts.")
			return
		}
		ctx.Err.Printf("Nostr credential verification: %s", err)
		writeNostrAuthError(w, http.StatusInternalServerError, "Unable to update that Nostr credential.")
		return
	}
	if err := auth.LoginPerson(ctx, r, person.ID, auth.MethodNostr); err != nil {
		ctx.Err.Printf("Nostr session for %s: %s", person.ID, err)
		writeNostrAuthError(w, http.StatusInternalServerError, "The signature was valid, but the session could not be started.")
		return
	}
	recordAuthAudit(ctx, r, person.ID, string(auth.MethodNostr), "login_succeeded", map[string]any{"pubkey": strings.ToLower(event.PubKey)})
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"redirect": next})
}

func NostrLinkVerify(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer, err := auth.Resolve(r, ctx)
	if err != nil || !recentAuthentication(viewer) {
		clearNostrChallenge(ctx, r)
		writeNostrAuthError(w, http.StatusUnauthorized, "Sign in again before linking a Nostr key.")
		return
	}
	challenge := ctx.Session.GetString(r.Context(), nostrChallengeKey)
	startedAtRaw := ctx.Session.GetString(r.Context(), nostrStartedAtKey)
	clearNostrChallenge(ctx, r)
	startedAt, startedErr := time.Parse(time.RFC3339Nano, startedAtRaw)
	age := time.Since(startedAt)
	if challenge == "" || startedErr != nil || age < -time.Minute || age > nostrAuthTTL {
		writeNostrAuthError(w, http.StatusUnauthorized, "That Nostr linking challenge expired or was already used. Try again.")
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	var input nostrVerifyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeNostrAuthError(w, http.StatusBadRequest, "The Nostr signer returned an invalid event.")
		return
	}
	if err := validateNostrAuthEvent(input.Event, challenge, nostrLinkVerifyURL(ctx), time.Now()); err != nil {
		writeNostrAuthError(w, http.StatusUnauthorized, err.Error())
		return
	}
	credential, err := getters.LinkNostrCredential(ctx, viewer.PersonID, input.Event.PubKey)
	if errors.Is(err, getters.ErrNostrCredentialLinked) {
		recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodNostr), "nostr_link_conflict", map[string]any{"reason": "person_credential_already_linked"})
		writeNostrAuthError(w, http.StatusConflict, "This profile already has a Nostr key linked. Unlink it before connecting a different key.")
		return
	}
	if errors.Is(err, getters.ErrNostrPubkeyConflict) {
		recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodNostr), "nostr_link_conflict", map[string]any{"pubkey": strings.ToLower(input.Event.PubKey)})
		writeNostrAuthError(w, http.StatusConflict, "That Nostr key is already linked to another account.")
		return
	}
	if err != nil {
		ctx.Err.Printf("Nostr credential link: %s", err)
		writeNostrAuthError(w, http.StatusInternalServerError, "Unable to link that Nostr key.")
		return
	}
	linkedAt := time.Now().UTC()
	sendAccountSecurityNotice(ctx, viewer.PersonID, identitySecurityEmail(viewer),
		"Nostr sign-in linked", getters.NostrPubkeyDisplay(credential.PubkeyHex)+" was added as a sign-in method.", linkedAt)
	recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodNostr), "nostr_credential_linked", map[string]any{"credential_id": credential.ID, "pubkey": credential.PubkeyHex})
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"redirect": "/dashboard/settings?flash=Nostr+key+linked."})
}

func NostrUnlink(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requirePersonIdentity(w, r, ctx)
	if viewer == nil {
		return
	}
	if !recentAuthentication(viewer) {
		redirectPersonEmails(w, r, "", "Sign in again before unlinking a Nostr key.")
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodNostr), "nostr_unlink_rejected", nil)
		redirectPersonEmails(w, r, "", "That unlink request expired. Reload the page and try again.")
		return
	}
	credential, err := getters.UnlinkNostrCredential(ctx, viewer.PersonID, strings.TrimSpace(r.FormValue("credential_id")))
	if err != nil {
		ctx.Err.Printf("Nostr credential unlink: %s", err)
		redirectPersonEmails(w, r, "", "That Nostr key could not be unlinked.")
		return
	}
	if credential == nil {
		redirectPersonEmails(w, r, "", "That Nostr key is no longer linked.")
		return
	}
	unlinkedAt := time.Now().UTC()
	value := credential.PubkeyHex
	if value == "" {
		value = credential.LegacyValue
	}
	sendAccountSecurityNotice(ctx, viewer.PersonID, identitySecurityEmail(viewer),
		"Nostr sign-in removed", getters.NostrPubkeyDisplay(value)+" was removed as a sign-in method.", unlinkedAt)
	if err := revokeOtherPersonSessions(ctx, r, viewer.PersonID); err != nil {
		ctx.Err.Printf("Nostr unlink session revocation: %s", err)
		auth.Logout(ctx, r)
		redirectPasswordLogin(w, r, "/dashboard/settings", "Nostr key unlinked. Sign in again to continue.")
		return
	}
	recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodNostr), "nostr_credential_unlinked", map[string]any{"credential_id": credential.ID})
	redirectPersonEmails(w, r, "Nostr key unlinked.", "")
}

func recentAuthentication(viewer *auth.Identity) bool {
	if viewer == nil || viewer.PersonID == "" || viewer.AuthenticatedAt.IsZero() {
		return false
	}
	age := time.Since(viewer.AuthenticatedAt)
	return age >= -time.Minute && age <= 15*time.Minute
}

func validateNostrAuthEvent(event nostr.Event, challenge, expectedURL string, now time.Time) error {
	eventAge := now.Sub(time.Unix(int64(event.CreatedAt), 0))
	if event.Kind != nostrAuthKind || event.Content != "" || eventAge < -time.Minute || eventAge > nostrAuthTTL ||
		singleNostrTag(event.Tags, "u") != expectedURL ||
		singleNostrTag(event.Tags, "method") != http.MethodPost ||
		!secureTokenEqual(challenge, singleNostrTag(event.Tags, "challenge")) ||
		!event.CheckID() {
		return errors.New("The signed Nostr event did not match this sign-in request.")
	}
	valid, err := event.CheckSignature()
	if err != nil || !valid {
		return errors.New("The Nostr signature could not be verified.")
	}
	return nil
}

func singleNostrTag(tags nostr.Tags, name string) string {
	value := ""
	count := 0
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == name {
			count++
			value = tag[1]
		}
	}
	if count != 1 {
		return ""
	}
	return value
}

func nostrVerifyURL(ctx *config.AppContext) string {
	return strings.TrimRight(ctx.Env.GetURI(), "/") + "/auth/nostr/verify"
}

func nostrLinkVerifyURL(ctx *config.AppContext) string {
	return strings.TrimRight(ctx.Env.GetURI(), "/") + "/auth/nostr/link/verify"
}

func clearNostrChallenge(ctx *config.AppContext, r *http.Request) {
	ctx.Session.Remove(r.Context(), nostrChallengeKey)
	ctx.Session.Remove(r.Context(), nostrStartedAtKey)
	ctx.Session.Remove(r.Context(), nostrNextKey)
}

func writeNostrAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
