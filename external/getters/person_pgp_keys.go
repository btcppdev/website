package getters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/pgpkeys"
	"btcpp-web/internal/types"
)

func ListPersonPGPKeys(ctx *config.AppContext, personID string, verifiedOnly bool) ([]*types.PersonPGPKey, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT fingerprint, key_id, public_key, verified_at, challenge, challenge_expires_at FROM person_pgp_keys WHERE person_id=$1 AND (NOT $2 OR verified_at IS NOT NULL) ORDER BY created_at, fingerprint`, personID, verifiedOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]*types.PersonPGPKey, 0)
	for rows.Next() {
		key := &types.PersonPGPKey{}
		if err := rows.Scan(&key.Fingerprint, &key.KeyID, &key.PublicKey, &key.VerifiedAt, &key.Challenge, &key.ChallengeExpiresAt); err != nil {
			return nil, err
		}
		if verifiedOnly {
			// Expired/revoked certificates must disappear from every public surface.
			if _, err := pgpkeys.Parse(key.PublicKey, time.Now()); err != nil {
				continue
			}
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func AddPersonPGPKey(ctx *config.AppContext, personID, armored string) error {
	key, err := pgpkeys.Parse(armored, time.Now())
	if err != nil {
		return err
	}
	_, err = ctx.DB.Exec(ctx.DatabaseContext(), `INSERT INTO person_pgp_keys (person_id, fingerprint, key_id, public_key) VALUES ($1,$2,$3,$4) ON CONFLICT (person_id,fingerprint) DO NOTHING`, personID, key.Fingerprint, key.KeyID, key.Armored)
	if err != nil {
		return fmt.Errorf("save public PGP key: %w", err)
	}
	return nil
}

func StartPersonPGPChallenge(ctx *config.AppContext, personID, fingerprint string) error {
	expires := time.Now().Add(pgpkeys.ChallengeLifetime)
	challenge, err := pgpkeys.NewChallenge(personID, fingerprint, expires)
	if err != nil {
		return err
	}
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE person_pgp_keys SET challenge=$3, challenge_expires_at=$4 WHERE person_id=$1 AND fingerprint=$2 AND verified_at IS NULL`, personID, fingerprint, challenge, expires)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("Unverified key not found.")
	}
	return nil
}

func VerifyPersonPGPKey(ctx *config.AppContext, personID, fingerprint, signature string) error {
	// Lock the owned row through verification: concurrent refreshes, removals and
	// duplicate submissions cannot consume or replace a challenge under us.
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var armored, challenge string
	var expires *time.Time
	err = tx.QueryRow(ctx.DatabaseContext(), `SELECT public_key, challenge, challenge_expires_at FROM person_pgp_keys WHERE person_id=$1 AND fingerprint=$2 AND verified_at IS NULL FOR UPDATE`, personID, fingerprint).Scan(&armored, &challenge, &expires)
	if err != nil {
		return errors.New("Unverified key not found. Reload the page.")
	}
	now := time.Now()
	if challenge == "" || expires == nil || !now.Before(*expires) {
		return errors.New("This challenge expired. Generate a new challenge and sign it again.")
	}
	// Account merges must not transfer outstanding proofs to a different account.
	if !strings.Contains(challenge, "\nAccount: "+personID+"\n") {
		return errors.New("Generate a new challenge for this account.")
	}
	key, err := pgpkeys.Parse(armored, now)
	if err != nil {
		return err
	}
	if err := key.Verify(challenge, signature, now); err != nil {
		return err
	}
	result, err := tx.Exec(ctx.DatabaseContext(), `UPDATE person_pgp_keys SET verified_at=clock_timestamp(), challenge='', challenge_expires_at=NULL WHERE person_id=$1 AND fingerprint=$2 AND challenge_expires_at > clock_timestamp()`, personID, fingerprint)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("This challenge expired. Generate a new challenge and sign it again.")
	}
	return tx.Commit(ctx.DatabaseContext())
}

func RemovePersonPGPKey(ctx *config.AppContext, personID, fingerprint string) error {
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `DELETE FROM person_pgp_keys WHERE person_id=$1 AND fingerprint=$2`, personID, fingerprint)
	if err == nil && result.RowsAffected() != 1 {
		return errors.New("Key not found.")
	}
	return err
}
