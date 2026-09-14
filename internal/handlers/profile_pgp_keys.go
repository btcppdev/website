package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/pgpkeys"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

type ProfilePGPKeysPage struct {
	Keys         []*types.PersonPGPKey
	CSRF         string
	FlashMessage string
	FlashError   string
	Year         uint
}

func DashboardProfilePGPKeys(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	identity := requirePersonIdentity(w, r, ctx)
	if identity == nil {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, 1024*1024)
		if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostFormValue("csrf")) {
			http.Error(w, "This request expired. Reload the page and try again.", http.StatusForbidden)
			return
		}
		var err error
		message := ""
		fingerprint := strings.ToUpper(strings.TrimSpace(r.PostFormValue("fingerprint")))
		switch r.PostFormValue("action") {
		case "add":
			err = getters.AddPersonPGPKey(ctx, identity.PersonID, r.PostFormValue("public_key"))
			message = "Key saved privately. Generate a challenge below to verify it."
		case "challenge":
			err = getters.StartPersonPGPChallenge(ctx, identity.PersonID, fingerprint)
			message = "New challenge ready. Download it and sign it within 30 minutes."
		case "verify":
			err = getters.VerifyPersonPGPKey(ctx, identity.PersonID, fingerprint, r.PostFormValue("signature"))
			message = "Key verified. It is now available on your public profile and through the API."
		case "remove":
			err = getters.RemovePersonPGPKey(ctx, identity.PersonID, fingerprint)
			message = "Key removed from your profile."
		default:
			err = fmt.Errorf("Unknown key action.")
		}
		q := url.Values{}
		if err != nil {
			// Never reflect database errors (which can contain submitted key material).
			ctx.Err.Printf("profile PGP key %s failed: %s", r.PostFormValue("action"), err)
			q.Set("error", pgpKeyActionError(err))
		} else {
			q.Set("flash", message)
		}
		http.Redirect(w, r, "/dashboard/profile/keys?"+q.Encode(), http.StatusSeeOther)
		return
	}
	keys, err := getters.ListPersonPGPKeys(ctx, identity.PersonID, false)
	if err != nil {
		http.Error(w, "Unable to load PGP keys", http.StatusInternalServerError)
		return
	}
	if fingerprint := r.URL.Query().Get("challenge"); fingerprint != "" {
		for _, key := range keys {
			if key.Fingerprint == fingerprint && key.Challenge != "" && key.ChallengeExpiresAt != nil && time.Now().Before(*key.ChallengeExpiresAt) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("Content-Disposition", `attachment; filename="btcpp-pgp-challenge.txt"`)
				_, _ = w.Write([]byte(key.Challenge))
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to prepare PGP controls", http.StatusInternalServerError)
		return
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_profile_pgp_keys.tmpl", &ProfilePGPKeysPage{Keys: keys, CSRF: csrf, FlashMessage: r.URL.Query().Get("flash"), FlashError: r.URL.Query().Get("error"), Year: helpers.CurrentYear()}); err != nil {
		ctx.Err.Printf("profile PGP keys template: %s", err)
	}
}

func pgpKeyActionError(err error) string {
	// Validation messages are controlled by our key functions; DB errors are not.
	message := err.Error()
	for _, prefix := range []string{"Paste ", "Invalid ", "Invalid or ", "Unable to read ", "Private keys ", "Add one ", "This key ", "The signature ", "The signing ", "Unverified key ", "This challenge ", "Generate a new ", "Key not found.", "Unknown key action."} {
		if strings.HasPrefix(message, prefix) {
			return message
		}
	}
	return "Unable to save this change. Please try again."
}

func RenderWhoIsPGPKeys(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	person, err := findWhoIsPerson(ctx, strings.TrimSpace(mux.Vars(r)["speaker"]))
	if err != nil {
		http.Error(w, "Unable to load profile", http.StatusInternalServerError)
		return
	}
	if person == nil {
		http.NotFound(w, r)
		return
	}
	keys, err := getters.ListPersonPGPKeys(ctx, person.Speaker.ID, true)
	if err != nil {
		http.Error(w, "Unable to load public keys", http.StatusInternalServerError)
		return
	}
	writePublicPGPKeys(w, r, keys, mux.Vars(r)["fingerprint"], mux.Vars(r)["format"])
}

func writePublicPGPKeys(w http.ResponseWriter, r *http.Request, keys []*types.PersonPGPKey, fingerprint, format string) {
	var bundle bytes.Buffer
	for _, key := range keys {
		if key.VerifiedAt == nil || (fingerprint != "" && key.Fingerprint != strings.ToUpper(fingerprint)) {
			continue
		}
		parsed, err := pgpkeys.Parse(key.PublicKey, time.Now())
		if err == nil {
			bundle.Write(parsed.Binary)
		}
	}
	if bundle.Len() == 0 {
		http.NotFound(w, r)
		return
	}
	data := bundle.Bytes()
	if format == "asc" {
		armored, err := pgpkeys.Armor(data)
		if err != nil {
			http.Error(w, "Unable to export keys", http.StatusInternalServerError)
			return
		}
		data = []byte(armored)
	}
	// Do not let caches continue distributing a key after its owner removes it.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/pgp-keys")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="key.%s"`, format))
	_, _ = w.Write(data)
}
