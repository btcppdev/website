package types

import "time"

type PersonPGPKey struct {
	Fingerprint        string     `json:"fingerprint"`
	KeyID              string     `json:"key_id"`
	PublicKey          string     `json:"public_key"`
	VerifiedAt         *time.Time `json:"verified_at"`
	Challenge          string     `json:"-"`
	ChallengeExpiresAt *time.Time `json:"-"`
}
