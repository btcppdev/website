package getters

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

const VolunteerApplicationConfirmationTTL = 30 * time.Minute

var ErrVolunteerAlreadyApplied = errors.New("you already have a volunteer application for this event; contact the volunteer coordinator if it needs to be reopened")

type volunteerApplicationPayload struct {
	Name          string   `json:"name"`
	Phone         string   `json:"phone"`
	Signal        string   `json:"signal"`
	Availability  []string `json:"availability"`
	ContactAt     string   `json:"contact_at"`
	Comments      string   `json:"comments"`
	DiscoveredVia string   `json:"discovered_via"`
	ConferenceID  string   `json:"conference_id"`
	OtherEvents   []string `json:"other_events"`
	WorkYes       []string `json:"work_yes"`
	WorkNo        []string `json:"work_no"`
	FirstEvent    bool     `json:"first_event"`
	Hometown      string   `json:"hometown"`
	Twitter       string   `json:"twitter"`
	Nostr         string   `json:"nostr"`
	Shirt         string   `json:"shirt"`
	Subscribe     bool     `json:"subscribe"`
}

func CreateVolunteerApplicationRequest(ctx *config.AppContext, vol *types.Volunteer) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	if vol == nil {
		return "", fmt.Errorf("volunteer application is required")
	}
	normalizeVolunteerInput(vol)
	vol.Email = strings.ToLower(vol.Email)
	vol.Shirt = types.ValidShirtSizeCode(vol.Shirt)
	if vol.Email == "" || vol.Name == "" || vol.Shirt == "" || len(vol.ScheduleFor) == 0 || vol.ScheduleFor[0] == nil || strings.TrimSpace(vol.ScheduleFor[0].Ref) == "" {
		return "", fmt.Errorf("name, email, event, and a valid shirt size are required")
	}
	parsedEmail, err := mail.ParseAddress(vol.Email)
	if err != nil || !strings.EqualFold(parsedEmail.Address, vol.Email) {
		return "", fmt.Errorf("a valid email address is required")
	}
	payload := volunteerApplicationPayload{
		Name: vol.Name, Phone: vol.Phone, Signal: vol.Signal,
		Availability: vol.Availability, ContactAt: vol.ContactAt, Comments: vol.Comments,
		DiscoveredVia: vol.DiscoveredVia, ConferenceID: strings.TrimSpace(vol.ScheduleFor[0].Ref),
		OtherEvents: volunteerConferenceRefs(vol.OtherEvents), WorkYes: volunteerJobRefs(vol.WorkYes),
		WorkNo: volunteerJobRefs(vol.WorkNo), FirstEvent: vol.FirstEvent, Hometown: vol.Hometown,
		Twitter: vol.Twitter.Handle, Nostr: vol.Nostr, Shirt: vol.Shirt, Subscribe: vol.Subscribe,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode volunteer application: %w", err)
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", fmt.Errorf("generate volunteer confirmation token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return "", fmt.Errorf("begin volunteer application request: %w", err)
	}
	defer tx.Rollback(dbctx)
	if _, err := tx.Exec(dbctx, `
		DELETE FROM volunteer_application_requests
		WHERE expires_at <= now() AND consumed_at IS NULL
	`); err != nil {
		return "", fmt.Errorf("clean expired volunteer applications: %w", err)
	}
	if _, err := tx.Exec(dbctx, `
		DELETE FROM volunteer_application_requests
		WHERE email = $1::citext AND payload->>'conference_id' = $2
			AND consumed_at IS NULL
	`, vol.Email, payload.ConferenceID); err != nil {
		return "", fmt.Errorf("replace pending volunteer application: %w", err)
	}
	if _, err := tx.Exec(dbctx, `
		INSERT INTO volunteer_application_requests (email, payload, token_hash, expires_at)
		VALUES ($1::citext, $2::jsonb, $3, $4)
	`, vol.Email, payloadJSON, tokenHash[:], time.Now().UTC().Add(VolunteerApplicationConfirmationTTL)); err != nil {
		return "", fmt.Errorf("create volunteer application request: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return "", fmt.Errorf("commit volunteer application request: %w", err)
	}
	return token, nil
}

func GetPendingVolunteerApplication(ctx *config.AppContext, token string) (*types.Volunteer, error) {
	if ctx == nil || ctx.DB == nil || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("volunteer confirmation link is invalid")
	}
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	var email string
	var payloadJSON []byte
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT email::text, payload
		FROM volunteer_application_requests
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
	`, tokenHash[:]).Scan(&email, &payloadJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("volunteer confirmation link is invalid, expired, or already used")
	}
	if err != nil {
		return nil, fmt.Errorf("load volunteer application request: %w", err)
	}
	var payload volunteerApplicationPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("decode volunteer application request: %w", err)
	}
	return volunteerFromApplicationPayload(email, payload), nil
}

func ConfirmVolunteerApplication(ctx *config.AppContext, token string) (*types.Volunteer, error) {
	vol, err := GetPendingVolunteerApplication(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := registerConfirmedVolunteer(ctx, vol); err != nil {
		return nil, err
	}
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE volunteer_application_requests
		SET consumed_at = now()
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
	`, tokenHash[:])
	if err != nil {
		return nil, fmt.Errorf("consume volunteer application request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("volunteer confirmation link is invalid, expired, or already used")
	}
	return vol, nil
}

func volunteerFromApplicationPayload(email string, payload volunteerApplicationPayload) *types.Volunteer {
	return &types.Volunteer{
		Name: payload.Name, Email: strings.ToLower(strings.TrimSpace(email)), Phone: payload.Phone,
		Signal: payload.Signal, Availability: payload.Availability, ContactAt: payload.ContactAt,
		Comments: payload.Comments, DiscoveredVia: payload.DiscoveredVia,
		ScheduleFor: []*types.Conf{{Ref: payload.ConferenceID}}, OtherEvents: volunteerConfRefsToTypes(payload.OtherEvents),
		WorkYes: volunteerJobRefsToTypes(payload.WorkYes), WorkNo: volunteerJobRefsToTypes(payload.WorkNo),
		FirstEvent: payload.FirstEvent, Hometown: payload.Hometown, Twitter: types.ParseTwitter(payload.Twitter),
		Nostr: payload.Nostr, Shirt: payload.Shirt, Subscribe: payload.Subscribe, Captcha: 5,
	}
}

func volunteerConferenceRefs(confs []*types.Conf) []string {
	refs := make([]string, 0, len(confs))
	for _, conf := range confs {
		if conf != nil && strings.TrimSpace(conf.Ref) != "" {
			refs = append(refs, strings.TrimSpace(conf.Ref))
		}
	}
	return refs
}

func volunteerJobRefs(jobs []*types.JobType) []string {
	refs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job != nil && strings.TrimSpace(job.Ref) != "" {
			refs = append(refs, strings.TrimSpace(job.Ref))
		}
	}
	return refs
}

func volunteerConfRefsToTypes(refs []string) []*types.Conf {
	out := make([]*types.Conf, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) != "" {
			out = append(out, &types.Conf{Ref: strings.TrimSpace(ref)})
		}
	}
	return out
}

func volunteerJobRefsToTypes(refs []string) []*types.JobType {
	out := make([]*types.JobType, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) != "" {
			out = append(out, &types.JobType{Ref: strings.TrimSpace(ref)})
		}
	}
	return out
}
