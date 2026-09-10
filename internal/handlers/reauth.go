package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
)

type ReauthenticationMethodView struct {
	Key         string
	Label       string
	Description string
	URL         string
}

type ReauthenticationPage struct {
	Next             string
	CancelURL        string
	Strong           bool
	PersonName       string
	Email            string
	Preferred        *ReauthenticationMethodView
	PreferredWasLast bool
	Alternatives     []*ReauthenticationMethodView
	DevLoginEnabled  bool
	CSRF             string
	FlashError       string
	Year             uint
}

// Reauthenticate presents a deliberately small step-up prompt. The method
// most recently used for this session is shown first; other methods remain
// available behind a disclosure so the prompt stays understandable on mobile.
func Reauthenticate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	identity, err := auth.Resolve(r, ctx)
	next := auth.SafeNext(r.URL.Query().Get("next"), "/dashboard")
	if err != nil || identity == nil || identity.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	strong := r.URL.Query().Get("strength") == "strong"
	methods, err := reauthenticationMethods(ctx, identity, next, strong)
	if err != nil {
		ctx.Err.Printf("/reauth load methods for %s: %s", identity.PersonID, err)
		http.Error(w, "Unable to load your sign-in methods", http.StatusInternalServerError)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to start re-authentication", http.StatusInternalServerError)
		return
	}
	preferred, alternatives := preferredReauthenticationMethod(methods, identity.Method)
	cancelURL := "/dashboard"
	if strings.HasPrefix(next, "/dashboard/settings") {
		cancelURL = "/dashboard/settings"
	} else if strings.HasPrefix(next, "/signer/authorize/resume") && strings.TrimSpace(ctx.Env.SignerURL) != "" {
		cancelURL = strings.TrimRight(ctx.Env.SignerURL, "/") + "/?authorization=cancelled"
	}
	email := strings.TrimSpace(identity.PrimaryEmail)
	if email == "" {
		email = strings.TrimSpace(identity.LoginEmail)
	}
	personName := "your account"
	if identity.Speaker != nil && strings.TrimSpace(identity.Speaker.Name) != "" {
		personName = identity.Speaker.Name
	}
	page := &ReauthenticationPage{
		Next: next, CancelURL: cancelURL, Strong: strong, PersonName: personName, Email: email,
		Preferred: preferred, PreferredWasLast: preferred != nil && preferred.Key == string(identity.Method), Alternatives: alternatives,
		DevLoginEnabled: dashboardDevLoginEnabled(ctx), CSRF: csrf,
		FlashError: r.URL.Query().Get("error"), Year: helpers.CurrentYear(),
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "reauth.tmpl", page); err != nil {
		ctx.Err.Printf("/reauth render: %s", err)
		http.Error(w, "Unable to render re-authentication", http.StatusInternalServerError)
	}
}

func reauthenticationMethods(ctx *config.AppContext, identity *auth.Identity, next string, strong bool) ([]*ReauthenticationMethodView, error) {
	passkeys, err := getters.ListPersonPasskeyCredentials(ctx, identity.PersonID)
	if err != nil {
		return nil, err
	}
	nostrCredentials, err := getters.ListPersonNostrCredentials(ctx, identity.PersonID)
	if err != nil {
		return nil, err
	}
	methods := make([]*ReauthenticationMethodView, 0, 8)
	if len(passkeys) > 0 {
		methods = append(methods, &ReauthenticationMethodView{Key: "passkey", Label: "Use your passkey", Description: "Touch ID, Face ID, or a security key"})
	}
	for _, credential := range nostrCredentials {
		if credential != nil && strings.TrimSpace(credential.PubkeyHex) != "" {
			methods = append(methods, &ReauthenticationMethodView{Key: "nostr", Label: "Sign with Nostr", Description: "Use your verified NIP-07 key"})
			break
		}
	}
	if strong {
		return methods, nil
	}
	oauthIdentities, err := getters.ListPersonOAuthIdentities(ctx, identity.PersonID)
	if err != nil {
		return nil, err
	}
	for _, linked := range oauthIdentities {
		if linked == nil {
			continue
		}
		provider := auth.OAuthProviderByKey(ctx.Env, linked.Provider)
		if provider == nil || !provider.Enabled() {
			continue
		}
		methods = append(methods, &ReauthenticationMethodView{
			Key: linked.Provider, Label: "Continue with " + provider.Label(),
			Description: "Reconfirm with your connected " + provider.Label() + " account",
			URL:         "/auth/oauth/" + provider.Key() + "?reauth=1&next=" + url.QueryEscape(next),
		})
	}
	password, err := getters.GetPersonPasswordCredential(ctx, identity.PersonID)
	if err != nil {
		return nil, err
	}
	if password != nil {
		methods = append(methods, &ReauthenticationMethodView{Key: "password", Label: "Use your password", Description: "Enter your bitcoin++ account password"})
	}
	if strings.TrimSpace(identity.PrimaryEmail) != "" || strings.TrimSpace(identity.LoginEmail) != "" {
		methods = append(methods, &ReauthenticationMethodView{Key: "email_link", Label: "Email a sign-in link", Description: "Send a one-time link to your verified email"})
	}
	return methods, nil
}

func preferredReauthenticationMethod(methods []*ReauthenticationMethodView, last auth.Method) (*ReauthenticationMethodView, []*ReauthenticationMethodView) {
	if len(methods) == 0 {
		return nil, nil
	}
	index := 0
	for i, method := range methods {
		if method != nil && method.Key == string(last) {
			index = i
			break
		}
	}
	preferred := methods[index]
	alternatives := make([]*ReauthenticationMethodView, 0, len(methods)-1)
	alternatives = append(alternatives, methods[:index]...)
	alternatives = append(alternatives, methods[index+1:]...)
	return preferred, alternatives
}

func reauthenticationURL(next string, strong bool) string {
	values := url.Values{"next": {auth.SafeNext(next, "/dashboard")}}
	if strong {
		values.Set("strength", "strong")
	}
	return "/reauth?" + values.Encode()
}
