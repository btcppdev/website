package getters

import (
	"btcpp-web/internal/config"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

var ErrTemporaryPasswordInvalid = errors.New("temporary password expired or was replaced; sign in again")

// IssueTemporaryPassword replaces the credential and revokes old sessions and
// reset links in the same transaction. Only a hash is persisted.
func IssueTemporaryPassword(ctx *config.AppContext, personID, hash string) error {
	q := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(q)
	if err != nil {
		return err
	}
	defer tx.Rollback(q)
	var exists bool
	if err := tx.QueryRow(q, `SELECT NOT is_deleted_account FROM people WHERE id=$1::uuid FOR UPDATE`, personID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrTemporaryPasswordInvalid
	}
	_, err = tx.Exec(q, `INSERT INTO person_password_credentials(person_id,password_hash,must_change_password,temporary_expires_at)
 VALUES($1::uuid,$2,true,now()+interval '72 hours') ON CONFLICT(person_id) DO UPDATE SET
 password_hash=EXCLUDED.password_hash,must_change_password=true,temporary_expires_at=EXCLUDED.temporary_expires_at,
 failed_attempts=0,locked_until=NULL,password_changed_at=now()`, personID, hash)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(q, `UPDATE password_reset_tokens SET consumed_at=now() WHERE person_id=$1::uuid AND consumed_at IS NULL`, personID); err != nil {
		return err
	}
	if _, err = incrementSessionVersion(q, tx, personID); err != nil {
		return err
	}
	return tx.Commit(q)
}

// ReplaceTemporaryPassword uses the exact credential proven during login.
// Reissue, expiry, reset, and concurrent redemption invalidate that proof.
func ReplaceTemporaryPassword(ctx *config.AppContext, personID, provenHash, newHash string) error {
	q := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(q)
	if err != nil {
		return err
	}
	defer tx.Rollback(q)
	var expires time.Time
	err = tx.QueryRow(q, `SELECT temporary_expires_at FROM person_password_credentials
 WHERE person_id=$1::uuid AND password_hash=$2 AND must_change_password AND temporary_expires_at>now()
 FOR UPDATE`, personID, provenHash).Scan(&expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTemporaryPasswordInvalid
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(q, `UPDATE person_password_credentials SET password_hash=$2,must_change_password=false,
 temporary_expires_at=NULL,failed_attempts=0,locked_until=NULL,password_changed_at=now() WHERE person_id=$1::uuid`, personID, newHash)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(q, `UPDATE password_reset_tokens SET consumed_at=now() WHERE person_id=$1::uuid AND consumed_at IS NULL`, personID); err != nil {
		return err
	}
	if _, err = incrementSessionVersion(q, tx, personID); err != nil {
		return err
	}
	return tx.Commit(q)
}
