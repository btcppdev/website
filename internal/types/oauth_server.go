package types

import "time"

type OAuthClient struct {
	ID                      string
	ClientID                string
	Name                    string
	RedirectURIs            []string
	AllowedScopes           []string
	TokenEndpointAuthMethod string
	CreatedByPersonID       string
	CreatedAt               time.Time
	RevokedAt               *time.Time
	ClientSecretHash        []byte
}

type OAuthAuthorizationCode struct {
	ClientDBID    string
	PersonID      string
	RedirectURI   string
	Scopes        []string
	CodeChallenge string
	ExpiresAt     time.Time
}

type OAuthAccessToken struct {
	ID         string
	ClientDBID string
	ClientID   string
	PersonID   string
	Scopes     []string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
}

type OAuthConsent struct {
	ClientDBID string
	ClientID   string
	ClientName string
	Scopes     []string
	GrantedAt  time.Time
	UpdatedAt  time.Time
}

func (c *OAuthClient) PermissionOptions() []string {
	return append(append([]string{}, APITokenScopes...), "offline_access")
}

func (c *OAuthClient) AllowsScope(scope string) bool {
	for _, allowed := range c.AllowedScopes {
		if allowed == scope {
			return true
		}
	}
	return false
}
