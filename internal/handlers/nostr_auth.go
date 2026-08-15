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
	ctx.Session.Put(r.Context(), nostrNextKey, auth.SafeNext(r.URL.Query().Get("next"), "/dashboard"))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(nostrChallengeResponse{
		Challenge: challenge, URL: nostrVerifyURL(ctx), Method: http.MethodPost,
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

	person, err := getters.FindPersonByNostrPubkey(ctx, event.PubKey)
	if errors.Is(err, getters.ErrNostrPubkeyConflict) {
		writeNostrAuthError(w, http.StatusConflict, "That Nostr key is attached to multiple profiles. An administrator must merge them before it can sign in.")
		return
	}
	if err != nil {
		ctx.Err.Printf("Nostr profile lookup: %s", err)
		writeNostrAuthError(w, http.StatusInternalServerError, "Unable to resolve that Nostr profile.")
		return
	}
	if person == nil {
		writeNostrAuthError(w, http.StatusUnauthorized, "That Nostr key is not attached to a bitcoin++ profile yet. Sign in by email and add the npub to your profile first.")
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
