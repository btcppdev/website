package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func ListPersonCanvasBadges(ctx *config.AppContext, personID string) ([]*types.BadgeCanvas, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT id::text,badge_name,badge_image_url,issuer_pubkey,badge_identifier,award_event_id FROM organization_badge_grants WHERE recipient_person_id=$1 AND state IN ('issued','accepted') AND award_event_id<>'' ORDER BY granted_at DESC,id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*types.BadgeCanvas, 0)
	for rows.Next() {
		var badge types.BadgeCanvas
		if err := rows.Scan(&badge.Reference, &badge.BadgeName, &badge.ArtworkURL, &badge.IssuerPubkey, &badge.BadgeIdentifier, &badge.AwardEventID); err != nil {
			return nil, err
		}
		result = append(result, &badge)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if ctx.Env == nil || ctx.Env.BadgeStudioURL == "" {
		return result, nil
	}
	studio, err := studioCanvasBadges(ctx.DatabaseContext(), ctx.Env.BadgeStudioURL, personID)
	if err != nil {
		return nil, err
	}
	// Local lifecycle changes can precede Studio synchronization.
	unavailable, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT award_event_id FROM organization_badge_grants WHERE recipient_person_id=$1 AND state IN ('revoked','canceled','corrected') AND award_event_id<>''`, personID)
	if err != nil {
		return nil, err
	}
	defer unavailable.Close()
	blocked := map[string]bool{}
	for unavailable.Next() {
		var event string
		if err := unavailable.Scan(&event); err != nil {
			return nil, err
		}
		blocked[event] = true
	}
	if err := unavailable.Err(); err != nil {
		return nil, err
	}
	// With Studio configured, its current issued list is authoritative. Local
	// grants may veto a stale remote award but cannot resurrect a revoked one.
	result = nil
	seen := map[string]bool{}
	for _, badge := range studio {
		if !seen[badge.AwardEventID] && !blocked[badge.AwardEventID] {
			result = append(result, badge)
			seen[badge.AwardEventID] = true
		}
	}
	return result, nil
}

// Validate inside the order transaction so ownership and issuance cannot be
// changed between the final check and saving the order's print specification.
func canvasBadgeForOrder(c context.Context, tx pgx.Tx, personID, grantID string) (*types.BadgeCanvas, error) {
	if personID == "" || grantID == "" {
		return nil, fmt.Errorf("choose an issued badge from your signed-in account")
	}
	var badge types.BadgeCanvas
	err := tx.QueryRow(c, `SELECT id::text,badge_name,badge_image_url,issuer_pubkey,badge_identifier,award_event_id FROM organization_badge_grants WHERE id=$1 AND recipient_person_id=$2 AND state IN ('issued','accepted') AND award_event_id<>'' FOR SHARE`, grantID, personID).Scan(&badge.Reference, &badge.BadgeName, &badge.ArtworkURL, &badge.IssuerPubkey, &badge.BadgeIdentifier, &badge.AwardEventID)
	if err != nil {
		return nil, fmt.Errorf("this badge is not available to print for your account")
	}
	return &badge, nil
}

// Badge Studio is the authority for awards linked to a Bitcoin++ account,
// including issuers that are not Bitcoin++ organizations. Do not substitute
// public /whois visibility or client-supplied artwork for this ownership lookup.
func studioCanvasBadges(c context.Context, baseURL, personID string) ([]*types.BadgeCanvas, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, nil
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/public/btcpp/people/" + url.PathEscape(personID) + "/badges"
	req, err := http.NewRequestWithContext(c, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	response, err := canvasStudioClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Badge Studio is unavailable; please try again")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Badge Studio is unavailable; please try again")
	}
	var profile struct {
		Issued []struct {
			Definition struct {
				IssuerPubkey string `json:"issuer_pubkey"`
				Identifier   string `json:"identifier"`
				Name         string `json:"name"`
				ImageURL     string `json:"image_url"`
			} `json:"definition"`
			Award struct {
				EventID    string          `json:"event_id"`
				Revocation json.RawMessage `json:"revocation"`
			} `json:"award"`
		} `json:"issued"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&profile); err != nil {
		return nil, fmt.Errorf("unable to read Badge Studio awards")
	}
	result := make([]*types.BadgeCanvas, 0, len(profile.Issued))
	for _, badge := range profile.Issued {
		if len(badge.Award.Revocation) > 0 && string(badge.Award.Revocation) != "null" {
			continue
		}
		event, err := hex.DecodeString(badge.Award.EventID)
		if err != nil || len(event) != 32 {
			continue
		}
		issuer, err := hex.DecodeString(badge.Definition.IssuerPubkey)
		if err != nil || len(issuer) != 32 {
			continue
		}
		artwork, err := url.Parse(badge.Definition.ImageURL)
		if err != nil || artwork.Host == "" || (artwork.Scheme != "https" && artwork.Scheme != "http") {
			continue
		}
		if badge.Definition.Name == "" || badge.Definition.Identifier == "" {
			continue
		}
		result = append(result, &types.BadgeCanvas{Reference: "studio:" + badge.Award.EventID, BadgeName: badge.Definition.Name, ArtworkURL: badge.Definition.ImageURL, IssuerPubkey: badge.Definition.IssuerPubkey, BadgeIdentifier: badge.Definition.Identifier, AwardEventID: badge.Award.EventID})
	}
	return result, nil
}

var canvasStudioClient = &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
