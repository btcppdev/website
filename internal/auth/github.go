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
	"golang.org/x/oauth2/github"
)

const (
	OAuthProviderGitHub = "github"
	githubAPIBaseURL    = "https://api.github.com"
	githubAPIVersion    = "2022-11-28"
	githubMaxResponse   = 1 << 20
)

type GitHubOAuthProvider struct {
	config     oauth2.Config
	apiBaseURL string
	httpClient *http.Client
}

func NewGitHubOAuthProvider(env *types.EnvConfig) *GitHubOAuthProvider {
	provider := &GitHubOAuthProvider{apiBaseURL: githubAPIBaseURL}
	if env == nil {
		return provider
	}
	provider.config = oauth2.Config{
		ClientID:     strings.TrimSpace(env.OAuth.GitHub.ClientID),
		ClientSecret: strings.TrimSpace(env.OAuth.GitHub.ClientSecret),
		RedirectURL:  strings.TrimRight(env.GetURI(), "/") + "/auth/oauth/github/callback",
		Scopes:       []string{"read:user", "user:email"},
		Endpoint:     github.Endpoint,
	}
	return provider
}

func (provider *GitHubOAuthProvider) Key() string    { return OAuthProviderGitHub }
func (provider *GitHubOAuthProvider) Label() string  { return "GitHub" }
func (provider *GitHubOAuthProvider) UsesPKCE() bool { return true }

func (provider *GitHubOAuthProvider) Enabled() bool {
	return provider != nil && provider.config.ClientID != "" && provider.config.ClientSecret != ""
}

func (provider *GitHubOAuthProvider) AuthorizationURL(state, codeChallenge string) (string, error) {
	if !provider.Enabled() {
		return "", errors.New("GitHub authentication is not configured")
	}
	if strings.TrimSpace(state) == "" || strings.TrimSpace(codeChallenge) == "" {
		return "", errors.New("OAuth state and PKCE challenge are required")
	}
	return provider.config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), nil
}

func (provider *GitHubOAuthProvider) Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error) {
	if !provider.Enabled() {
		return nil, errors.New("GitHub authentication is not configured")
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(codeVerifier) == "" {
		return nil, errors.New("OAuth code and PKCE verifier are required")
	}
	ctx = provider.clientContext(ctx)
	token, err := provider.config.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return nil, fmt.Errorf("exchange GitHub OAuth code: %w", err)
	}
	if token.AccessToken == "" {
		return nil, errors.New("GitHub returned an empty access token")
	}
	return token, nil
}

func (provider *GitHubOAuthProvider) FetchIdentity(ctx context.Context, token *oauth2.Token) (*types.PersonOAuthIdentity, error) {
	if provider == nil || token == nil || token.AccessToken == "" {
		return nil, errors.New("GitHub access token is required")
	}
	ctx = provider.clientContext(ctx)
	client := provider.config.Client(ctx, token)

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := provider.getJSON(ctx, client, "/user", &user); err != nil {
		return nil, err
	}
	if user.ID <= 0 || strings.TrimSpace(user.Login) == "" {
		return nil, errors.New("GitHub returned an invalid user identity")
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := provider.getJSON(ctx, client, "/user/emails?per_page=100", &emails); err != nil {
		return nil, err
	}
	email := ""
	for _, candidate := range emails {
		if !candidate.Verified || strings.TrimSpace(candidate.Email) == "" {
			continue
		}
		if email == "" || candidate.Primary {
			email = strings.ToLower(strings.TrimSpace(candidate.Email))
		}
		if candidate.Primary {
			break
		}
	}

	return &types.PersonOAuthIdentity{
		Provider:      OAuthProviderGitHub,
		Subject:       strconv.FormatInt(user.ID, 10),
		Username:      strings.TrimSpace(user.Login),
		Email:         email,
		EmailVerified: email != "",
		AvatarURL:     strings.TrimSpace(user.AvatarURL),
	}, nil
}

func (provider *GitHubOAuthProvider) clientContext(ctx context.Context) context.Context {
	if provider.httpClient == nil {
		return ctx
	}
	return context.WithValue(ctx, oauth2.HTTPClient, provider.httpClient)
}

func (provider *GitHubOAuthProvider) getJSON(ctx context.Context, client *http.Client, path string, destination any) error {
	base, err := url.Parse(provider.apiBaseURL)
	if err != nil {
		return err
	}
	reference, err := url.Parse(path)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(reference).String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	request.Header.Set("User-Agent", "btcpp-web")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request GitHub API %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, githubMaxResponse))
		return fmt.Errorf("GitHub API %s returned %s", path, response.Status)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, githubMaxResponse)).Decode(destination); err != nil {
		return fmt.Errorf("decode GitHub API %s: %w", path, err)
	}
	return nil
}
