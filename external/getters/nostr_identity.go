package getters

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/nbd-wtf/go-nostr/nip19"
)

var ErrNostrPubkeyConflict = errors.New("nostr public key belongs to multiple people")

// FindPersonByNostrPubkey resolves the normalized hex key against profile
// npubs. Profile values predate authentication and may be npub, nostr:npub,
// an njump URL, or bare hex, so matching is deliberately normalized in Go.
func FindPersonByNostrPubkey(ctx *config.AppContext, pubkey string) (*types.Speaker, error) {
	pubkey, err := NormalizeNostrPubkey(pubkey)
	if err != nil {
		return nil, err
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, nostr
		FROM people
		WHERE btrim(nostr) <> ''
	`)
	if err != nil {
		return nil, fmt.Errorf("list Nostr profiles: %w", err)
	}
	defer rows.Close()
	var personID string
	for rows.Next() {
		var candidateID, candidateValue string
		if err := rows.Scan(&candidateID, &candidateValue); err != nil {
			return nil, err
		}
		candidate, err := NormalizeNostrPubkey(candidateValue)
		if err != nil || candidate != pubkey {
			continue
		}
		if personID != "" && personID != candidateID {
			return nil, ErrNostrPubkeyConflict
		}
		personID = candidateID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if personID == "" {
		return nil, nil
	}
	return FetchSpeakerByID(ctx, personID)
}

func NormalizeNostrPubkey(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "nostr:")
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = strings.Trim(strings.TrimSpace(parsed.Path), "/")
		if slash := strings.LastIndex(value, "/"); slash >= 0 {
			value = value[slash+1:]
		}
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return strings.ToLower(value), nil
	}
	prefix, decoded, err := nip19.Decode(value)
	if err != nil || prefix != "npub" {
		return "", errors.New("Nostr public key must be an npub or 32-byte hex key")
	}
	pubkey, ok := decoded.(string)
	if !ok {
		return "", errors.New("invalid npub payload")
	}
	if raw, err := hex.DecodeString(pubkey); err != nil || len(raw) != 32 {
		return "", errors.New("invalid npub public key")
	}
	return strings.ToLower(pubkey), nil
}
