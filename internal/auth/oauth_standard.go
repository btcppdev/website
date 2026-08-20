package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"btcpp-web/internal/types"

	"golang.org/x/oauth2"
)

const (
	OAuthProviderDiscord = "discord"
	OAuthProviderGitLab  = "gitlab"
	OAuthProviderMLH     = "mlh"
)

type standardOAuthProvider struct {
	key             string
	label           string
	usesPKCE        bool
	config          oauth2.Config
	apiBaseURL      string
	userPath        string
	authorizeParams []oauth2.AuthCodeOption
	httpClient      *http.Client
	parseIdentity   func([]byte) (*types.PersonOAuthIdentity, error)
}

func (provider *standardOAuthProvider) Key() string    { return provider.key }
func (provider *standardOAuthProvider) Label() string  { return provider.label }
func (provider *standardOAuthProvider) UsesPKCE() bool { return provider.usesPKCE }

func (provider *standardOAuthProvider) Enabled() bool {
	return provider != nil && provider.config.ClientID != "" && provider.config.ClientSecret != ""
}

func (provider *standardOAuthProvider) AuthorizationURL(state, codeChallenge string) (string, error) {
	if !provider.Enabled() {
		return "", fmt.Errorf("%s authentication is not configured", provider.label)
	}
	if strings.TrimSpace(state) == "" {
		return "", errors.New("OAuth state is required")
	}
	options := append([]oauth2.AuthCodeOption(nil), provider.authorizeParams...)
	if provider.usesPKCE {
		if strings.TrimSpace(codeChallenge) == "" {
			return "", errors.New("PKCE challenge is required")
		}
		options = append(options,
			oauth2.SetAuthURLParam("code_challenge", codeChallenge),
			oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		)
	}
	return provider.config.AuthCodeURL(state, options...), nil
}

func (provider *standardOAuthProvider) Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error) {
	if !provider.Enabled() {
		return nil, fmt.Errorf("%s authentication is not configured", provider.label)
	}
	if strings.TrimSpace(code) == "" || (provider.usesPKCE && strings.TrimSpace(codeVerifier) == "") {
		return nil, errors.New("OAuth code and required verifier are missing")
	}
	if provider.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, provider.httpClient)
	}
	options := []oauth2.AuthCodeOption{}
	if provider.usesPKCE {
		options = append(options, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	}
	token, err := provider.config.Exchange(ctx, code, options...)
	if err != nil {
		return nil, fmt.Errorf("exchange %s OAuth code: %w", provider.label, err)
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("%s returned an empty access token", provider.label)
	}
	return token, nil
}

func (provider *standardOAuthProvider) FetchIdentity(ctx context.Context, token *oauth2.Token) (*types.PersonOAuthIdentity, error) {
	if provider == nil || token == nil || token.AccessToken == "" {
		return nil, errors.New("OAuth access token is required")
	}
	if provider.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, provider.httpClient)
	}
	base, err := url.Parse(provider.apiBaseURL)
	if err != nil {
		return nil, err
	}
	reference, err := url.Parse(provider.userPath)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(reference).String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "btcpp-web")
	client := provider.config.Client(ctx, token)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s identity: %w", provider.label, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		detail := oauthIdentityErrorDetail(response)
		if detail != "" {
			return nil, fmt.Errorf("%s identity endpoint returned %s (%s)", provider.label, response.Status, detail)
		}
		return nil, fmt.Errorf("%s identity endpoint returned %s", provider.label, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, githubMaxResponse))
	if err != nil {
		return nil, err
	}
	identity, err := provider.parseIdentity(body)
	if err != nil {
		return nil, fmt.Errorf("decode %s identity: %w", provider.label, err)
	}
	if identity == nil || identity.Provider != provider.key || strings.TrimSpace(identity.Subject) == "" || strings.TrimSpace(identity.Username) == "" {
		return nil, fmt.Errorf("%s returned an invalid user identity", provider.label)
	}
	identity.Email = strings.ToLower(strings.TrimSpace(identity.Email))
	identity.Username = strings.TrimSpace(identity.Username)
	identity.AvatarURL = strings.TrimSpace(identity.AvatarURL)
	return identity, nil
}

func oauthIdentityErrorDetail(response *http.Response) string {
	if response == nil || response.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return ""
	}
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
		Detail           string `json:"detail"`
	}
	_ = json.Unmarshal(body, &payload)
	parts := make([]string, 0, 3)
	for _, value := range []string{payload.Error, payload.ErrorDescription, payload.Message, payload.Detail, response.Header.Get("WWW-Authenticate")} {
		value = strings.Join(strings.Fields(value), " ")
		if value != "" && len(value) <= 500 {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ": ")
}

func NewDiscordOAuthProvider(env *types.EnvConfig) *standardOAuthProvider {
	config := types.OAuthProviderConfig{}
	if env != nil {
		config = env.OAuth.Discord
	}
	return &standardOAuthProvider{
		key: OAuthProviderDiscord, label: "Discord", usesPKCE: true,
		config: oauth2.Config{
			ClientID: strings.TrimSpace(config.ClientID), ClientSecret: strings.TrimSpace(config.ClientSecret),
			RedirectURL: oauthRedirectURL(env, OAuthProviderDiscord), Scopes: []string{"identify", "email"},
			Endpoint: oauth2.Endpoint{AuthURL: "https://discord.com/oauth2/authorize", TokenURL: "https://discord.com/api/oauth2/token"},
		},
		apiBaseURL: "https://discord.com", userPath: "/api/v10/users/@me", parseIdentity: parseDiscordIdentity,
	}
}

func parseDiscordIdentity(body []byte) (*types.PersonOAuthIdentity, error) {
	var user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		Verified bool   `json:"verified"`
		Avatar   string `json:"avatar"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}
	avatar := ""
	if user.ID != "" && user.Avatar != "" {
		avatar = "https://cdn.discordapp.com/avatars/" + url.PathEscape(user.ID) + "/" + url.PathEscape(user.Avatar) + ".png"
	}
	return &types.PersonOAuthIdentity{Provider: OAuthProviderDiscord, Subject: user.ID, Username: user.Username, Email: user.Email, EmailVerified: user.Verified && strings.TrimSpace(user.Email) != "", AvatarURL: avatar}, nil
}

func NewGitLabOAuthProvider(env *types.EnvConfig) *standardOAuthProvider {
	config := types.OAuthProviderConfig{}
	if env != nil {
		config = env.OAuth.GitLab
	}
	return &standardOAuthProvider{
		key: OAuthProviderGitLab, label: "GitLab", usesPKCE: true,
		config: oauth2.Config{
			ClientID: strings.TrimSpace(config.ClientID), ClientSecret: strings.TrimSpace(config.ClientSecret),
			RedirectURL: oauthRedirectURL(env, OAuthProviderGitLab), Scopes: []string{"read_user"},
			Endpoint: oauth2.Endpoint{AuthURL: "https://gitlab.com/oauth/authorize", TokenURL: "https://gitlab.com/oauth/token"},
		},
		apiBaseURL: "https://gitlab.com", userPath: "/api/v4/user",
		authorizeParams: []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("gl_auth_type", "login")},
		parseIdentity:   parseGitLabIdentity,
	}
}

func parseGitLabIdentity(body []byte) (*types.PersonOAuthIdentity, error) {
	var user struct {
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		Email       string `json:"email"`
		AvatarURL   string `json:"avatar_url"`
		ConfirmedAt string `json:"confirmed_at"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}
	subject := ""
	if user.ID > 0 {
		subject = strconv.FormatInt(user.ID, 10)
	}
	return &types.PersonOAuthIdentity{Provider: OAuthProviderGitLab, Subject: subject, Username: user.Username, Email: user.Email, EmailVerified: user.Email != "" && user.ConfirmedAt != "", AvatarURL: user.AvatarURL}, nil
}

func NewMLHOAuthProvider(env *types.EnvConfig) *standardOAuthProvider {
	config := types.OAuthProviderConfig{}
	if env != nil {
		config = env.OAuth.MLH
	}
	return &standardOAuthProvider{
		key: OAuthProviderMLH, label: "Major League Hacking", usesPKCE: false,
		config: oauth2.Config{
			ClientID: strings.TrimSpace(config.ClientID), ClientSecret: strings.TrimSpace(config.ClientSecret),
			RedirectURL: oauthRedirectURL(env, OAuthProviderMLH), Scopes: []string{"public", "user:read:profile", "user:read:email"},
			Endpoint: oauth2.Endpoint{AuthURL: "https://my.mlh.io/oauth/authorize", TokenURL: "https://my.mlh.io/oauth/token", AuthStyle: oauth2.AuthStyleInParams},
		},
		apiBaseURL: "https://api.mlh.com", userPath: "/v4/users/me", parseIdentity: parseMLHIdentity,
	}
}

func parseMLHIdentity(body []byte) (*types.PersonOAuthIdentity, error) {
	var user struct {
		ID        string `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name == "" {
		name = user.ID
	}
	return &types.PersonOAuthIdentity{Provider: OAuthProviderMLH, Subject: user.ID, Username: name, Email: user.Email}, nil
}

func oauthRedirectURL(env *types.EnvConfig, provider string) string {
	if env == nil {
		return ""
	}
	return strings.TrimRight(env.GetURI(), "/") + "/auth/oauth/" + provider + "/callback"
}
