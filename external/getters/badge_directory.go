package getters

import (
	"fmt"

	"btcpp-web/internal/config"
)

// ListVerifiedNostrPubkeys returns only signature-verified identities. It must
// not fall back to the legacy, user-entered people.nostr profile field.
func ListVerifiedNostrPubkeys(ctx *config.AppContext, personIDs []string) (map[string]string, error) {
	out := make(map[string]string)
	if len(personIDs) == 0 {
		return out, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT person_id::text, pubkey_hex
		FROM person_nostr_credentials
		WHERE person_id = ANY($1::uuid[]) AND verified_at IS NOT NULL
	`, personIDs)
	if err != nil {
		return nil, fmt.Errorf("list verified Nostr pubkeys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var personID, pubkey string
		if err := rows.Scan(&personID, &pubkey); err != nil {
			return nil, fmt.Errorf("scan verified Nostr pubkey: %w", err)
		}
		out[personID] = pubkey
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read verified Nostr pubkeys: %w", err)
	}
	return out, nil
}
