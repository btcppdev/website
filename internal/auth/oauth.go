package auth

import (
	"context"
	"strings"

	"btcpp-web/internal/types"

	"golang.org/x/oauth2"
)

// OAuthProvider is the deliberately small contract shared by sign-in
// providers. Access and refresh tokens live only for the callback request and
// are never persisted by the application.
type OAuthProvider interface {
	Key() string
	Label() string
	Enabled() bool
	UsesPKCE() bool
	AuthorizationURL(state, codeChallenge string) (string, error)
	Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error)
	FetchIdentity(ctx context.Context, token *oauth2.Token) (*types.PersonOAuthIdentity, error)
}

func OAuthProviders(env *types.EnvConfig) []OAuthProvider {
	return []OAuthProvider{
		NewGitHubOAuthProvider(env),
		NewDiscordOAuthProvider(env),
		NewGitLabOAuthProvider(env),
		NewMLHOAuthProvider(env),
	}
}

func OAuthProviderByKey(env *types.EnvConfig, key string) OAuthProvider {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, provider := range OAuthProviders(env) {
		if provider.Key() == key {
			return provider
		}
	}
	return nil
}
