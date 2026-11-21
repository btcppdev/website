package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// These fixtures use the real templates, synthetic identities and invalid tokens.
// The opt-in gallery has no database, session, email or authorization handlers.
type accountFlowExample struct {
	Slug, Group, Title, Template string
	Data                         any
}

func accountFlowExamples() []accountFlowExample {
	const csrf = "preview-only-not-a-valid-token"
	const email = "alex@example.test"
	var examples []accountFlowExample
	add := func(slug, group, title, name string, data any) {
		examples = append(examples, accountFlowExample{slug, group, title, name + ".tmpl", data})
	}
	for _, state := range []string{"default", "sent", "error", "passkey"} {
		p := LoginPage{Next: "/dashboard", CSRF: csrf, Year: 2026, OAuthProviders: []*OAuthProviderView{{Key: "github", Label: "GitHub", Enabled: true, LinkURL: "#"}}}
		if state == "passkey" {
			p.PreferredMethod = "passkey"
		}
		if state == "sent" {
			p.FlashMessage = "Check your email for a sign-in link."
		}
		if state == "error" {
			p.FlashError = "That sign-in link expired. Please try again."
		}
		add("login-"+state, "Sign in", "Sign in · "+state, "login", p)
	}
	add("oauth", "Sign in", "OAuth · Example app", "oauth_authorize", OAuthAuthorizePage{Client: &types.OAuthClient{Name: "Example app"}, ClientID: "preview-only", Scopes: []string{"profile:self:read", "talks:read", "offline_access"}, ScopeDescriptions: oauthScopeDescriptions(), RedirectURI: "https://example.invalid/callback", CSRF: csrf, State: "preview", CodeChallenge: "preview", Year: 2026})
	for _, state := range []string{"valid", "expired", "invalid"} {
		add("email-link-"+state, "Email links", "Email sign-in · "+state, "magic_login", MagicLinkPage{Token: csrf, TokenValid: state == "valid", CanResend: state == "expired", CSRF: csrf, Year: 2026})
	}
	for _, sent := range []bool{false, true} {
		state := "verify"
		if sent {
			state = "sent"
		}
		add("oauth-email-"+state, "Email links", "Linking email · "+state, "oauth_email_link", OAuthEmailLinkPage{ProviderKey: "github", ProviderName: "GitHub", Email: email, Sent: sent, CSRF: csrf, Year: 2026})
	}
	for _, state := range []string{"default", "sent"} {
		p := PasswordResetPage{CSRF: csrf, Year: 2026}
		if state == "sent" {
			p.FlashMessage = "If that email is on your account, a reset link is on its way."
		}
		add("forgot-"+state, "Passwords", "Forgot password · "+state, "forgot_password", p)
	}
	for _, state := range []string{"valid", "expired", "error"} {
		p := PasswordResetPage{Token: csrf, CSRF: csrf, TokenValid: state != "expired", Year: 2026}
		if state == "error" {
			p.FlashError = "The passwords don’t match. Please try again."
		}
		add("reset-"+state, "Passwords", "Reset password · "+state, "reset_password", p)
	}
	for _, state := range []string{"choose", "error", "issue", "issued"} {
		p := TemporaryPasswordPage{CSRF: csrf, Email: email, Admin: state == "issue" || state == "issued", Year: 2026}
		if state == "error" {
			p.Error = "Choose a different password of at least 12 characters."
		}
		if state == "issued" {
			p.Password = "sample-only-not-a-real-password"
			p.Message = "Temporary password created."
		}
		add("temporary-"+state, "Passwords", "Temporary password · "+state, "temporary_password", p)
	}
	add("link-account", "Account linking", "Confirm GitHub account", "oauth_confirm", OAuthConfirmPage{Speaker: &types.Speaker{Name: "Alex Rivera"}, Identity: &types.PersonOAuthIdentity{Username: "alexrivera", Subject: "12345678", Email: email, EmailVerified: true}, ProviderKey: "github", ProviderName: "GitHub", CSRF: csrf, Year: 2026})
	for _, state := range []string{"confirm", "expired"} {
		p := PersonEmailVerificationPage{Email: "alex.work@example.test", RequesterEmail: email, Token: csrf, Year: 2026}
		if state == "expired" {
			p.Error = "This verification link has expired. Request another from your account settings."
		}
		add("add-email-"+state, "Account linking", "Add email · "+state, "person_email_verification", p)
	}
	passkey := &ReauthenticationMethodView{Key: "passkey", Label: "Use your passkey", Description: "Touch ID, Face ID, or a security key"}
	password := &ReauthenticationMethodView{Key: "password", Label: "Use your password"}
	for _, state := range []string{"passkey", "password", "error", "unavailable"} {
		p := ReauthenticationPage{Next: "/dashboard/settings", CancelURL: "/dashboard", PersonName: "Alex Rivera", Email: email, Preferred: passkey, PreferredWasLast: true, CSRF: csrf, Year: 2026, Alternatives: []*ReauthenticationMethodView{password, {Key: "github", Label: "Continue with GitHub", Description: "alexrivera", URL: "#"}, {Key: "email_link", Label: "Email a sign-in link", Description: email}, {Key: "nostr", Label: "Use your Nostr key", Description: "Confirm in your Nostr extension"}}}
		if state == "password" {
			p.Preferred = password
			p.Alternatives = []*ReauthenticationMethodView{passkey}
		}
		if state == "error" {
			p.FlashError = "Your confirmation expired. Please sign in again."
		}
		if state == "unavailable" {
			p.Strong = true
			p.Preferred = nil
			p.Alternatives = nil
		}
		add("reauth-"+state, "Security check", "Confirm it’s you · "+state, "reauth", p)
	}
	add("vaults", "Signer", "Choose a key vault", "managed_signer_vaults", managedSignerVaultsPage{PersonName: "Alex Rivera", Year: 2026, Vaults: []managedSignerVaultChoice{{Name: "Alex Rivera", Kind: "Personal", URL: "#"}, {Name: "Example organization", Kind: "Organization", URL: "#"}}})
	for _, action := range []string{"create_identity", "connect_login", "sign_event", "rotate_identity", "recover_identity", "error"} {
		p := ManagedSignerAuthorizationPage{PersonName: "Alex Rivera", Tenant: "organization", TenantID: "preview-org", TenantName: "Example organization", Role: "Owner", Action: action, ActionLabel: strings.ReplaceAll(action, "_", " "), CSRF: csrf, ReturnTo: "/preview/account/return", Year: 2026}
		if action == "connect_login" {
			p.ApplicationName = "Badge Studio"
			p.ApplicationURL = "https://badges.example.test"
			p.Permissions = []string{"sign_event:27235", "sign_event:30009", "sign_event:8", "sign_event_batch:8", "sign_event:5", "sign_event:10008"}
		}
		if action == "sign_event" {
			p.ActionLabel = "Award badges to a group"
			p.EventKind = 8
			p.RecipientCount = 3
			p.VerifiedGrants = 3
			p.EventHash = strings.Repeat("a", 64)
			p.BadgeName = "Hackathon builder"
			p.BadgeIdentifier = "builders-2026"
			p.ApplicationName = "Badge Studio"
			p.ApplicationURL = "https://badges.example.test"
			for _, key := range []string{"a", "b", "c"} {
				p.BatchRecipients = append(p.BatchRecipients, ManagedSignerBatchRecipient{Pubkey: strings.Repeat(key, 64), GrantID: "preview-grant"})
			}
		}
		if action == "error" {
			p.Action = "sign_event"
			p.ActionLabel = ""
			p.Error = "This request expired. Return to the signer and try again."
		}
		add("signer-"+action, "Signer", "Signer · "+strings.ReplaceAll(action, "_", " "), "managed_signer_authorize", p)
	}
	add("return", "Signer", "Return to signer", "managed_signer_continue", ManagedSignerAuthorizationPage{ReturnTo: "/preview/account/return", Token: csrf, Year: 2026})
	return examples
}

func accountFlowTemplates() (*template.Template, error) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "127.0.0.1", Port: "8138", MailOff: true}}
	if err := loadTemplates(ctx); err != nil {
		return nil, err
	}
	return ctx.TemplateCache, nil
}

func TestAccountFlowPagesRender(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir(findRepoRoot(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	tpl, err := accountFlowTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range accountFlowExamples() {
		t.Run(example.Slug, func(t *testing.T) {
			var out bytes.Buffer
			if err := tpl.ExecuteTemplate(&out, example.Template, example.Data); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "auth-shell--simple") {
				t.Fatal("account panel missing")
			}
		})
	}
}

// Run with BTCPP_ACCOUNT_PREVIEW=1 go test ./internal/handlers -run '^TestAccountFlowGallery$' -timeout 0 -v.
func TestAccountFlowGallery(t *testing.T) {
	if os.Getenv("BTCPP_ACCOUNT_PREVIEW") != "1" {
		t.Skip("local visual review only")
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(findRepoRoot(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	gallery := template.Must(template.ParseFiles("ops/account-flow-gallery.html"))
	examples := accountFlowExamples()
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/preview/account", func(w http.ResponseWriter, r *http.Request) {
		if err := gallery.Execute(w, examples); err != nil {
			t.Log(err)
		}
	})
	mux.HandleFunc("/preview/account/", func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimPrefix(r.URL.Path, "/preview/account/")
		for _, example := range examples {
			if example.Slug == slug {
				tpl, err := accountFlowTemplates()
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				var out bytes.Buffer
				if err = tpl.ExecuteTemplate(&out, example.Template, example.Data); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				// Pause automatic return only in this non-functional visual fixture.
				html := strings.ReplaceAll(out.String(), `<script src="/static/js/managed-signer-continue.js" defer></script>`, "")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Content-Security-Policy", "form-action 'none'")
				if strings.HasPrefix(example.Template, "managed_signer_") {
					w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'self'; form-action 'none'")
				}
				fmt.Fprint(w, html)
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/preview/account", http.StatusSeeOther)
			return
		}
		http.NotFound(w, r)
	})
	server := &http.Server{Addr: "127.0.0.1:8138", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Visual preview only; forms are disabled.", http.StatusMethodNotAllowed)
			return
		}
		mux.ServeHTTP(w, r)
	})}
	t.Log("Account gallery: http://127.0.0.1:8138/preview/account")
	t.Fatal(server.ListenAndServe())
}
