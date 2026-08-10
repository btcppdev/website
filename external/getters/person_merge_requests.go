package getters

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

const PersonMergeConfirmationTTL = 30 * time.Minute

func CreatePersonMergeRequest(ctx *config.AppContext, requesterPersonID, rawTargetEmail string) (*types.PersonMergeRequest, string, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, "", fmt.Errorf("database is not configured")
	}
	requesterPersonID = strings.TrimSpace(requesterPersonID)
	targetEmail := strings.ToLower(strings.TrimSpace(rawTargetEmail))
	if requesterPersonID == "" || targetEmail == "" {
		return nil, "", fmt.Errorf("requester and target email are required")
	}
	resolution, err := ResolvePersonByEmail(ctx, targetEmail)
	if err != nil {
		return nil, "", err
	}
	if resolution.IsConflict() {
		return nil, "", fmt.Errorf("that email belongs to unresolved duplicate profiles; a global admin must resolve it directly")
	}
	if resolution.Alias == nil || resolution.Person == nil {
		return nil, "", fmt.Errorf("no account was found for that email")
	}
	targetPersonID := resolution.Alias.PersonID
	if targetPersonID == requesterPersonID {
		return nil, "", fmt.Errorf("that email is already attached to your account")
	}
	requester, err := FetchSpeakerByID(ctx, requesterPersonID)
	if err != nil || requester == nil {
		return nil, "", fmt.Errorf("requesting account could not be loaded")
	}
	requesterEmail, err := GetPrimaryPersonEmail(ctx, requesterPersonID)
	if err != nil || requesterEmail == "" {
		return nil, "", fmt.Errorf("requesting account needs a primary email")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("generate merge confirmation token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Add(PersonMergeConfirmationTTL)

	var id string
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO person_merge_requests (
			requester_person_id, requester_name, requester_email,
			target_person_id, target_name, target_email,
			status, confirmation_token_hash, confirmation_expires_at
		)
		VALUES ($1::uuid, $2, $3::citext, $4::uuid, $5, $6::citext,
			'awaiting_confirmation', $7, $8)
		ON CONFLICT DO NOTHING
		RETURNING id::text
	`, requesterPersonID, requester.Name, requesterEmail,
		targetPersonID, resolution.Person.Name, targetEmail, tokenHash[:], expiresAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingRequester, existingStatus string
		err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
			SELECT id::text, requester_person_id::text, status
			FROM person_merge_requests
			WHERE status IN ('awaiting_confirmation', 'pending')
				AND least(requester_person_id, target_person_id) = least($1::uuid, $2::uuid)
				AND greatest(requester_person_id, target_person_id) = greatest($1::uuid, $2::uuid)
		`, requesterPersonID, targetPersonID).Scan(&id, &existingRequester, &existingStatus)
		if err != nil || existingRequester != requesterPersonID || (existingStatus != "awaiting_confirmation" && existingStatus != "pending") {
			return nil, "", fmt.Errorf("a merge request between these accounts is already active")
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			UPDATE person_merge_requests
			SET confirmation_token_hash = $2, confirmation_expires_at = $3,
				target_email = $4::citext, updated_at = now()
			WHERE id = $1::uuid AND status IN ('awaiting_confirmation', 'pending')
		`, id, tokenHash[:], expiresAt, targetEmail); err != nil {
			return nil, "", fmt.Errorf("refresh merge confirmation: %w", err)
		}
	}
	if err != nil {
		return nil, "", fmt.Errorf("create person merge request: %w", err)
	}
	request, err := GetPersonMergeRequest(ctx, id)
	return request, token, err
}

func GetPersonMergeRequestByConfirmationToken(ctx *config.AppContext, token string) (*types.PersonMergeRequest, error) {
	token = strings.TrimSpace(token)
	if ctx == nil || ctx.DB == nil || token == "" {
		return nil, fmt.Errorf("merge confirmation link is invalid")
	}
	tokenHash := sha256.Sum256([]byte(token))
	var request types.PersonMergeRequest
	var reviewedAt, confirmationExpiresAt, confirmedAt *time.Time
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), personMergeRequestSelect+`
		WHERE request.confirmation_token_hash = $1
	`, tokenHash[:]).Scan(personMergeRequestScanArgs(&request, &reviewedAt, &confirmationExpiresAt, &confirmedAt)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("merge confirmation link is invalid")
	}
	if err != nil {
		return nil, fmt.Errorf("load merge confirmation: %w", err)
	}
	request.ReviewedAt = reviewedAt
	request.ConfirmationExpiresAt = confirmationExpiresAt
	request.ConfirmedAt = confirmedAt
	return &request, nil
}

func ConfirmPersonMergeRequest(ctx *config.AppContext, token string) (*types.PersonMergeRequest, bool, error) {
	request, err := GetPersonMergeRequestByConfirmationToken(ctx, token)
	if err != nil {
		return nil, false, err
	}
	if request.Status == "pending" && request.ConfirmedAt != nil {
		return request, false, nil
	}
	if request.Status != "awaiting_confirmation" {
		return nil, false, fmt.Errorf("merge request is no longer awaiting confirmation")
	}
	if request.ConfirmationExpiresAt == nil || time.Now().After(*request.ConfirmationExpiresAt) {
		return nil, false, fmt.Errorf("merge confirmation link has expired; ask the other account to send a new request")
	}
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE person_merge_requests
		SET status = 'pending', confirmed_at = now(), updated_at = now()
		WHERE id = $1::uuid AND status = 'awaiting_confirmation'
			AND confirmation_token_hash = $2 AND confirmation_expires_at > now()
	`, request.ID, tokenHash[:])
	if err != nil {
		return nil, false, fmt.Errorf("confirm merge request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, false, fmt.Errorf("merge confirmation link has expired or was already used")
	}
	request, err = GetPersonMergeRequest(ctx, request.ID)
	return request, true, err
}

func GetPersonMergeRequest(ctx *config.AppContext, requestID string) (*types.PersonMergeRequest, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var request types.PersonMergeRequest
	var reviewedAt, confirmationExpiresAt, confirmedAt *time.Time
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), personMergeRequestSelect+`
		WHERE request.id = $1::uuid
	`, strings.TrimSpace(requestID)).Scan(personMergeRequestScanArgs(&request, &reviewedAt, &confirmationExpiresAt, &confirmedAt)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get person merge request: %w", err)
	}
	request.ReviewedAt = reviewedAt
	request.ConfirmationExpiresAt = confirmationExpiresAt
	request.ConfirmedAt = confirmedAt
	return &request, nil
}

func ListPersonMergeRequestsForPerson(ctx *config.AppContext, personID string) ([]*types.PersonMergeRequest, error) {
	return listPersonMergeRequests(ctx, personMergeRequestSelect+`
		WHERE request.requester_person_id = $1::uuid OR request.target_person_id = $1::uuid
		ORDER BY request.created_at DESC
	`, strings.TrimSpace(personID))
}

const personMergeRequestSelect = `
	SELECT request.id::text,
		request.requester_person_id::text, coalesce(requester.name, request.requester_name), request.requester_email::text,
		request.target_person_id::text, coalesce(target.name, merge_event.source_snapshot->>'name', request.target_name), request.target_email::text,
		request.status, coalesce(request.reviewed_by_person_id::text, ''), coalesce(reviewer.name, ''),
		coalesce(request.merge_event_id::text, ''), request.review_note,
		request.created_at, request.reviewed_at,
		request.confirmation_expires_at, request.confirmed_at
	FROM person_merge_requests request
	LEFT JOIN people requester ON requester.id = request.requester_person_id
	LEFT JOIN people target ON target.id = request.target_person_id
	LEFT JOIN people reviewer ON reviewer.id = request.reviewed_by_person_id
	LEFT JOIN person_merge_events merge_event ON merge_event.id = request.merge_event_id
`

func listPersonMergeRequests(ctx *config.AppContext, query string, args ...any) ([]*types.PersonMergeRequest, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("list person merge requests: %w", err)
	}
	defer rows.Close()
	var out []*types.PersonMergeRequest
	for rows.Next() {
		var request types.PersonMergeRequest
		var reviewedAt, confirmationExpiresAt, confirmedAt *time.Time
		if err := rows.Scan(personMergeRequestScanArgs(&request, &reviewedAt, &confirmationExpiresAt, &confirmedAt)...); err != nil {
			return nil, fmt.Errorf("scan person merge request: %w", err)
		}
		request.ReviewedAt = reviewedAt
		request.ConfirmationExpiresAt = confirmationExpiresAt
		request.ConfirmedAt = confirmedAt
		out = append(out, &request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate person merge requests: %w", err)
	}
	return out, nil
}

func personMergeRequestScanArgs(request *types.PersonMergeRequest, reviewedAt, confirmationExpiresAt, confirmedAt **time.Time) []any {
	return []any{
		&request.ID,
		&request.RequesterPersonID, &request.RequesterName, &request.RequesterEmail,
		&request.TargetPersonID, &request.TargetName, &request.TargetEmail,
		&request.Status, &request.ReviewedByPersonID, &request.ReviewedByName,
		&request.MergeEventID, &request.ReviewNote,
		&request.CreatedAt, reviewedAt, confirmationExpiresAt, confirmedAt,
	}
}
