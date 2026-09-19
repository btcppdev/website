package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func buildInsiderRefund(original []byte, person string, job *getters.ServiceRefundJob) (insiderSettlementEvent, error) {
	var event insiderSettlementEvent
	if err := json.Unmarshal(original, &event); err != nil {
		return event, err
	}
	if job == nil || (job.AccessAction != "retain" && job.AccessAction != "revoke") ||
		event.EventKind != "settled" || event.EventID == "" || event.PersonID != person || person == "" ||
		(event.ProductCode != "insider.membership.monthly" && event.ProductCode != "insider.membership.annual" && event.ProductCode != "insider.article") ||
		(event.ProductCode == "insider.article" && event.ResourceID == "") ||
		event.ContributionPublicationID != "" || event.Provider != job.Refund.Provider ||
		event.ProviderReference != job.Refund.PaymentReference || event.Currency != job.Refund.Currency ||
		job.Refund.AmountCents <= 0 || job.Refund.AmountCents > event.AmountCents || job.Refund.RefundReference == "" {
		return insiderSettlementEvent{}, getters.ErrServiceRefund
	}
	event.ReversesEventID = event.EventID
	event.EventID = "refund:" + job.Refund.Provider + ":" + job.Refund.RefundReference
	event.EventKind = "reversed"
	event.AccessAction = job.AccessAction
	event.AccessStartsAt, event.AccessEndsAt = nil, nil
	event.OccurredAt = job.Refund.RefundedAt.UTC()
	event.AdjustmentReference = job.Refund.RefundReference
	event.AdjustmentAmountCents = job.Refund.AmountCents
	event.AdjustmentReason = job.Refund.Reason
	return event, nil
}

func processServiceRefund(ctx context.Context, app *config.AppContext, attemptID string, now time.Time) (bool, error) {
	if !serviceFulfillmentReady(app) {
		return false, errors.New("refund delivery not configured")
	}
	endpoint := strings.TrimSpace(app.Env.InsiderFulfillmentEndpoint)
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false, errors.New("invalid refund endpoint")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return false, errors.New("refund endpoint requires HTTPS")
	}
	job, err := getters.ClaimInsiderRefundDelivery(ctx, app, attemptID, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	finish := func(state string, cause error) (bool, error) {
		applied, err := getters.FinishServiceRefundDelivery(ctx, app, job, state, now)
		if err != nil {
			return true, err
		}
		if !applied {
			return true, errors.New("refund delivery lease superseded")
		}
		return true, cause
	}
	original, person, err := getters.InsiderRefundOriginal(ctx, app, job.Refund.AttemptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finish("review", err)
	}
	if err != nil {
		return finish("retry", err)
	}
	event, err := buildInsiderRefund(original, person, job)
	if err != nil {
		return finish("review", err)
	}
	if len(job.Payload) == 0 {
		payload, err := json.Marshal(event)
		if err != nil {
			return finish("review", err)
		}
		if err = getters.FreezeServiceRefundDelivery(ctx, app, job, payload); err != nil {
			return finish("retry", err)
		}
	}
	// Use saved bytes for retries, including after a process restart. A 202 is
	// not durable acknowledgment; only 200/204 release the next refund revision.
	if err = postServiceLifecycle(ctx, app, endpoint, event.EventID, job.Payload); err != nil {
		return finish("retry", err)
	}
	return finish("accepted", nil)
}
