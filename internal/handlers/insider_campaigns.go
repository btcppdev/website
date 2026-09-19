package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/missives"

	"github.com/gorilla/mux"
)

type insiderAudienceRequest struct {
	SchemaVersion  int    `json:"schema_version"`
	PublicationID  string `json:"publication_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Candidates     []struct {
		SubscriberID string `json:"subscriber_id"`
		Email        string `json:"email"`
	} `json:"candidates"`
}

type insiderTransactionalEmailRequest struct {
	SchemaVersion  int    `json:"schema_version"`
	Kind           string `json:"kind"`
	IdempotencyKey string `json:"idempotency_key"`
	Recipient      struct {
		Email string `json:"email"`
	} `json:"recipient"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

type insiderSubscriberProjectionRequest struct {
	PersonID         string    `json:"person_id"`
	SubscriberTier   string    `json:"subscriber_tier"`
	NewsletterStatus string    `json:"newsletter_status"`
	Version          int64     `json:"version"`
	AsOf             time.Time `json:"as_of"`
}

type insiderCampaignRequest struct {
	PublicationID    string                            `json:"publication_id"`
	IdempotencyKey   string                            `json:"idempotency_key"`
	Series           string                            `json:"series"`
	Access           string                            `json:"access"`
	Subject          string                            `json:"subject"`
	PreviewText      string                            `json:"preview_text"`
	BodyMarkdown     string                            `json:"body_markdown"`
	CanonicalURL     string                            `json:"canonical_url"`
	Streams          []string                          `json:"streams"`
	DeliveryMode     string                            `json:"delivery_mode"`
	SendAt           time.Time                         `json:"send_at"`
	AudienceFrozenAt time.Time                         `json:"audience_frozen_at"`
	SuppressedCount  int                               `json:"suppressed_count"`
	Recipients       []insiderCampaignRecipientRequest `json:"recipients"`
}

type insiderCampaignRecipientRequest struct {
	SubscriberID    string `json:"subscriber_id"`
	Email           string `json:"email"`
	SharedContactID string `json:"shared_contact_id"`
	ArticleURL      string `json:"article_url"`
	PreferencesURL  string `json:"preferences_url"`
	OpenTrackingURL string `json:"open_tracking_url"`
}

type insiderCampaignResponse struct {
	CampaignID     string `json:"campaign_id"`
	Status         string `json:"status"`
	RecipientCount int    `json:"accepted_recipient_count"`
	Created        bool   `json:"created"`
}

func AcceptInsiderCampaign(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), ctx.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="btcpp-insider"`)
		writeInsiderCampaignError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request insiderCampaignRequest
	if err := decoder.Decode(&request); err != nil {
		writeInsiderCampaignError(w, http.StatusBadRequest, "The campaign request is invalid.")
		return
	}
	recipients := make([]getters.InsiderCampaignRecipientInput, 0, len(request.Recipients))
	for _, recipient := range request.Recipients {
		recipients = append(recipients, getters.InsiderCampaignRecipientInput{
			SubscriberID: recipient.SubscriberID, Email: recipient.Email, SharedContactID: recipient.SharedContactID,
			ArticleURL: recipient.ArticleURL, PreferencesURL: recipient.PreferencesURL, OpenTrackingURL: recipient.OpenTrackingURL,
		})
	}
	campaign, created, err := getters.AcceptInsiderCampaign(ctx, getters.InsiderCampaignInput{
		PublicationID: request.PublicationID, IdempotencyKey: request.IdempotencyKey,
		Series: request.Series, Access: request.Access, Subject: request.Subject,
		PreviewText: request.PreviewText, BodyMarkdown: request.BodyMarkdown,
		CanonicalURL: request.CanonicalURL, Streams: request.Streams,
		DeliveryMode: request.DeliveryMode, SendAt: request.SendAt,
		AudienceFrozenAt: request.AudienceFrozenAt, SuppressedCount: request.SuppressedCount, Recipients: recipients,
	})
	if err != nil {
		ctx.Err.Printf("accept Insider campaign: %s", err)
		writeInsiderCampaignError(w, http.StatusUnprocessableEntity, "The campaign could not be accepted.")
		return
	}
	if campaign.Status == "accepted" || campaign.Status == "failed" {
		if _, err := missives.QueueInsiderCampaignByUID(ctx, campaign.ID, campaign.MissiveUID); err != nil {
			ctx.Err.Printf("queue Insider campaign %s: %s", campaign.ID, err)
			writeInsiderCampaignError(w, http.StatusInternalServerError, "The campaign was saved but could not be queued.")
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(insiderCampaignResponse{
		CampaignID: campaign.ID, Status: campaign.Status,
		RecipientCount: campaign.RecipientCount, Created: created,
	})
}

func InsiderCampaignStats(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), ctx.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", "Bearer realm=\"btcpp-insider\"")
		writeInsiderCampaignError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	stats, err := getters.GetInsiderCampaignStats(ctx, mux.Vars(r)["id"])
	if err != nil {
		ctx.Err.Printf("get Insider campaign stats: %s", err)
		writeInsiderCampaignError(w, http.StatusNotFound, "The campaign was not found.")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"campaign_id": stats.CampaignID, "publication_id": stats.PublicationID,
		"status": stats.Status, "accepted_recipient_count": stats.RecipientCount,
		"scheduled_count": stats.ScheduledCount, "sent_count": stats.SentCount,
		"delivered_count": stats.DeliveredCount, "bounced_count": stats.BouncedCount,
		"complained_count":       stats.ComplainedCount,
		"estimated_unique_opens": stats.EstimatedUniqueOpen,
		"estimated_total_opens":  stats.EstimatedTotalOpen,
		"last_error":             stats.LastError,
	})
}

func ResolveInsiderAudience(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), ctx.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="btcpp-insider"`)
		writeInsiderCampaignError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request insiderAudienceRequest
	if err := decoder.Decode(&request); err != nil || request.SchemaVersion != 1 || strings.TrimSpace(request.PublicationID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeInsiderCampaignError(w, http.StatusBadRequest, "The audience request is invalid.")
		return
	}
	candidates := make([]getters.DeliveryAudienceCandidate, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		candidates = append(candidates, getters.DeliveryAudienceCandidate{SubscriberID: candidate.SubscriberID, Email: candidate.Email})
	}
	resolved, err := getters.ResolveDeliveryAudience(ctx, candidates)
	if err != nil {
		ctx.Err.Printf("resolve Insider delivery audience: %s", err)
		writeInsiderCampaignError(w, http.StatusUnprocessableEntity, "The audience could not be resolved.")
		return
	}
	type responseRecipient struct {
		SubscriberID    string `json:"subscriber_id"`
		Email           string `json:"email"`
		SharedContactID string `json:"shared_contact_id"`
	}
	response := struct {
		Recipients []responseRecipient `json:"recipients"`
	}{Recipients: make([]responseRecipient, 0, len(resolved))}
	for _, recipient := range resolved {
		response.Recipients = append(response.Recipients, responseRecipient{SubscriberID: recipient.SubscriberID, Email: recipient.Email, SharedContactID: recipient.SharedContactID})
	}
	_ = json.NewEncoder(w).Encode(response)
}

func AcceptInsiderTransactionalEmail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), ctx.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="btcpp-insider"`)
		writeInsiderCampaignError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request insiderTransactionalEmailRequest
	if err := decoder.Decode(&request); err != nil {
		writeInsiderCampaignError(w, http.StatusBadRequest, "The transactional email request is invalid.")
		return
	}
	address, addressErr := mail.ParseAddress(strings.TrimSpace(request.Recipient.Email))
	requestKey := strings.TrimSpace(request.IdempotencyKey)
	if request.SchemaVersion != 1 || !validInsiderTransactionalKind(request.Kind) || addressErr != nil || address.Address != strings.TrimSpace(request.Recipient.Email) ||
		requestKey == "" || len(requestKey) > 200 || requestKey != strings.TrimSpace(r.Header.Get("Idempotency-Key")) || strings.TrimSpace(request.Subject) == "" || len(request.Subject) > 200 ||
		request.Text == "" || len(request.Text) > 1<<20 || request.HTML == "" || len(request.HTML) > 1<<20 {
		writeInsiderCampaignError(w, http.StatusUnprocessableEntity, "The transactional email request is invalid.")
		return
	}
	if err := emails.ComposeAndSendMail(ctx, &emails.Mail{
		JobKey: "insider-transactional:" + requestKey, Email: strings.ToLower(address.Address),
		FromAddr: "insider@btcpp.dev", FromName: "Bitcoin++ Insider Edition",
		Title: request.Subject, TextBody: []byte(request.Text), HTMLBody: []byte(request.HTML), SendAt: time.Now().UTC(),
	}); err != nil {
		ctx.Err.Printf("send Insider transactional email: %s", err)
		writeInsiderCampaignError(w, http.StatusInternalServerError, "The email could not be queued.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func validInsiderTransactionalKind(kind string) bool {
	switch kind {
	case "insider.subscription_confirmation", "insider.editorial_test", "insider.receipt", "insider.receipt_adjustment", "insider.subscription_cancellation":
		return true
	default:
		return false
	}
}

func ApplyInsiderSubscriberProjection(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), ctx.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="btcpp-insider"`)
		writeInsiderCampaignError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request insiderSubscriberProjectionRequest
	if err := decoder.Decode(&request); err != nil {
		writeInsiderCampaignError(w, http.StatusBadRequest, "The subscriber projection is invalid.")
		return
	}
	applied, err := getters.ApplyInsiderSubscriberProjection(ctx, getters.InsiderSubscriberProjection{
		PersonID: request.PersonID, SubscriberTier: request.SubscriberTier,
		NewsletterStatus: request.NewsletterStatus, Version: request.Version, AsOf: request.AsOf,
	})
	if err != nil {
		ctx.Err.Printf("apply Insider subscriber projection: %s", err)
		writeInsiderCampaignError(w, http.StatusUnprocessableEntity, "The subscriber projection could not be applied.")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"applied": applied})
}

func validInsiderServiceAuthorization(header, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(secret)) == 1
}

func writeInsiderCampaignError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message}})
}
