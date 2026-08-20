package getters

import (
	"fmt"
	"time"

	"btcpp-web/internal/config"
)

const authRateLimitCleanupBatch = 256

// ConsumeAuthRateLimit atomically records an attempt in a database-backed
// fixed window. It is shared by every application instance.
func ConsumeAuthRateLimit(ctx *config.AppContext, keyHash []byte, maximum int, window time.Duration) (bool, error) {
	if len(keyHash) != 32 || maximum < 1 || window <= 0 {
		return false, fmt.Errorf("invalid auth rate limit")
	}
	var allowed bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		WITH expired AS (
			SELECT key_hash
			FROM auth_rate_limits
			WHERE expires_at <= now()
			ORDER BY expires_at
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		), deleted AS (
			DELETE FROM auth_rate_limits AS limits
			USING expired
			WHERE limits.key_hash = expired.key_hash
		)
		INSERT INTO auth_rate_limits (key_hash, window_started_at, attempt_count, updated_at, expires_at)
		VALUES ($1, now(), 1, now(), now() + $2::interval)
		ON CONFLICT (key_hash) DO UPDATE SET
			window_started_at = CASE
				WHEN auth_rate_limits.expires_at <= now() THEN now()
				ELSE auth_rate_limits.window_started_at
			END,
			attempt_count = CASE
				WHEN auth_rate_limits.expires_at <= now() THEN 1
				ELSE auth_rate_limits.attempt_count + 1
			END,
			updated_at = now(),
			expires_at = CASE
				WHEN auth_rate_limits.expires_at <= now() THEN now() + $2::interval
				ELSE auth_rate_limits.expires_at
			END
		RETURNING attempt_count <= $3
	`, keyHash, postgresInterval(window), maximum, authRateLimitCleanupBatch).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("consume auth rate limit: %w", err)
	}
	return allowed, nil
}
