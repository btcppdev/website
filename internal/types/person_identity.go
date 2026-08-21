package types

import (
	"encoding/json"
	"time"
)

const APITokenVersion = "btcpp_v1"

var APITokenScopes = []string{
	"identity:self:read",
	"profile:self:read",
	"profile:self:write",
	"talks:read",
	"talks:write",
	"schedule:write",
	"recordings:write",
}

func ValidAPITokenScopes(scopes []string) bool {
	if len(scopes) == 0 {
		return false
	}
	allowed := make(map[string]bool, len(APITokenScopes))
	for _, scope := range APITokenScopes {
		allowed[scope] = true
	}
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if !allowed[scope] || seen[scope] {
			return false
		}
		seen[scope] = true
	}
	return true
}

type PersonEmail struct {
	ID                 string
	PersonID           string
	Email              string
	IsPrimary          bool
	VerifiedAt         time.Time
	OriginMergeEventID string
	RemovalLocked      bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PersonOAuthIdentity struct {
	ID            string
	PersonID      string
	Provider      string
	Subject       string
	Username      string
	Email         string
	EmailVerified bool
	AvatarURL     string
	LinkedAt      time.Time
	LastLoginAt   *time.Time
	UpdatedAt     time.Time
}

type PersonNostrCredential struct {
	ID          string
	PersonID    string
	PubkeyHex   string
	LegacyValue string
	VerifiedAt  *time.Time
	LinkedAt    time.Time
	LastLoginAt *time.Time
	UpdatedAt   time.Time
}

type PersonPasswordCredential struct {
	PersonID          string
	PasswordHash      string
	FailedAttempts    int
	LockedUntil       *time.Time
	CreatedAt         time.Time
	PasswordChangedAt time.Time
	UpdatedAt         time.Time
}

type PersonPasskeyCredential struct {
	ID             string
	PersonID       string
	CredentialID   []byte
	CredentialJSON json.RawMessage
	DisplayName    string
	CreatedAt      time.Time
	LastUsedAt     *time.Time
	UpdatedAt      time.Time
}

type PersonAPIToken struct {
	ID         string
	PersonID   string
	Name       string
	Prefix     string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  time.Time
	Expired    bool
	RevokedAt  *time.Time
	UpdatedAt  time.Time
}

type AuthAuditEvent struct {
	PersonID      string
	Method        string
	Event         string
	RemoteAddress string
	UserAgent     string
	Metadata      map[string]any
}

type PersonEmailConflict struct {
	Email      string
	PersonID   string
	PersonName string
	DetectedAt time.Time
}

type PersonEmailResolution struct {
	Email             string
	Person            *Speaker
	Alias             *PersonEmail
	ConflictPersonIDs []string
}

type PersonMergeRequest struct {
	ID                    string
	RequesterPersonID     string
	RequesterName         string
	RequesterEmail        string
	TargetPersonID        string
	TargetName            string
	TargetEmail           string
	Status                string
	ReviewedByPersonID    string
	ReviewedByName        string
	MergeEventID          string
	ReviewNote            string
	ConfirmationExpiresAt *time.Time
	ConfirmedAt           *time.Time
	CreatedAt             time.Time
	ReviewedAt            *time.Time
}

func (resolution *PersonEmailResolution) IsConflict() bool {
	return resolution != nil && len(resolution.ConflictPersonIDs) > 1
}
