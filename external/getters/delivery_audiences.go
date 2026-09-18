package getters

import (
	"fmt"
	"net/mail"
	"strings"

	"btcpp-web/internal/config"
)

type DeliveryAudienceCandidate struct {
	SubscriberID string
	Email        string
}

type ResolvedDeliveryRecipient struct {
	SubscriberID    string
	Email           string
	SharedContactID string
}

// ResolveDeliveryAudience applies only shared, provider-level suppressions and
// ensures a reusable delivery contact exists. Insider has already applied its
// product consent and is the only service allowed to choose the candidates.
func ResolveDeliveryAudience(ctx *config.AppContext, candidates []DeliveryAudienceCandidate) ([]ResolvedDeliveryRecipient, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if len(candidates) > 50_000 {
		return nil, fmt.Errorf("delivery audience is too large")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, fmt.Errorf("begin delivery audience resolution: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	seenIDs, seenEmails := map[string]bool{}, map[string]bool{}
	resolved := make([]ResolvedDeliveryRecipient, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.SubscriberID = strings.TrimSpace(candidate.SubscriberID)
		candidate.Email = strings.ToLower(strings.TrimSpace(candidate.Email))
		address, parseErr := mail.ParseAddress(candidate.Email)
		if parseErr != nil || strings.ToLower(address.Address) != candidate.Email || candidate.SubscriberID == "" || seenIDs[candidate.SubscriberID] || seenEmails[candidate.Email] {
			return nil, fmt.Errorf("delivery audience candidate is invalid or duplicated")
		}
		seenIDs[candidate.SubscriberID], seenEmails[candidate.Email] = true, true
		var suppressed bool
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			SELECT EXISTS(SELECT 1 FROM email_delivery_suppressions WHERE email=$1 AND lifted_at IS NULL)
		`, candidate.Email).Scan(&suppressed); err != nil {
			return nil, fmt.Errorf("check global email suppression: %w", err)
		}
		if suppressed {
			continue
		}
		var contactID string
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO subscribers(email) VALUES($1)
			ON CONFLICT(email) DO UPDATE SET email=excluded.email
			RETURNING id::text
		`, candidate.Email).Scan(&contactID); err != nil {
			return nil, fmt.Errorf("ensure shared delivery contact: %w", err)
		}
		resolved = append(resolved, ResolvedDeliveryRecipient{
			SubscriberID: candidate.SubscriberID, Email: candidate.Email, SharedContactID: contactID,
		})
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, fmt.Errorf("commit delivery audience resolution: %w", err)
	}
	return resolved, nil
}
