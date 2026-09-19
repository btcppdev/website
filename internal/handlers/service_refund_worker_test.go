package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testInsiderRefundDelivery(t *testing.T, app *config.AppContext, attemptID string, original insiderSettlementEvent) {
	t.Helper()
	ctx := context.Background()
	var received [][]byte
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		var event insiderSettlementEvent
		if err != nil || json.Unmarshal(body, &event) != nil || r.Header.Get("Idempotency-Key") != event.EventID || r.Header.Get("Authorization") != "Bearer "+app.Env.InsiderFulfillmentToken {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received = append(received, body)
		switch len(received) {
		case 1:
			w.WriteHeader(503)
		case 2:
			w.WriteHeader(202)
		default:
			w.WriteHeader(204)
		}
	}))
	defer receiver.Close()
	app.Env.InsiderFulfillmentEndpoint = receiver.URL
	refund := getters.VerifiedServiceRefund{AttemptID: attemptID, Provider: original.Provider, PaymentReference: original.ProviderReference, RefundReference: "re_" + attemptID, AmountCents: 500, Currency: "USD", RefundedAt: original.OccurredAt.Add(time.Hour), Reason: "Exceptional admin adjustment"}
	if err := getters.RecordServiceRefund(ctx, app, refund); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Hour)
	if worked, err := processServiceRefund(ctx, app, attemptID, now); err != nil || worked {
		t.Fatal("refund sent without admin decision", worked, err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, original.PersonID); err != nil {
		t.Fatal(err)
	}
	if err := getters.RecordServiceRefundAccessDecision(ctx, app, original.PersonID, refund.Provider, refund.RefundReference, "retain", "Manual exception"); err != nil {
		t.Fatal(err)
	}
	second := refund
	second.RefundReference += "_second"
	second.AmountCents = 250
	if err := getters.RecordServiceRefund(ctx, app, second); err != nil {
		t.Fatal(err)
	}
	if err := getters.RecordServiceRefundAccessDecision(ctx, app, original.PersonID, second.Provider, second.RefundReference, "revoke", "Final manual adjustment"); err != nil {
		t.Fatal(err)
	}
	for index, offset := range []time.Duration{0, 2 * time.Minute, 5 * time.Minute, 6 * time.Minute} {
		worked, err := processServiceRefund(ctx, app, attemptID, now.Add(offset))
		if !worked || (index < 2 && err == nil) || (index >= 2 && err != nil) {
			t.Fatalf("delivery %d: worked=%v err=%v", index, worked, err)
		}
		if index < 2 {
			if worked, err := processServiceRefund(ctx, app, attemptID, now.Add(offset)); err != nil || worked {
				t.Fatal("retry backoff or revision order bypassed", worked, err)
			}
			app.Env.InsiderMerchantName = "Changed while retrying"
		}
	}
	if len(received) != 4 || string(received[0]) != string(received[1]) || string(received[1]) != string(received[2]) {
		t.Fatal("refund payload changed across retry")
	}
	for index, action := range []string{"retain", "revoke"} {
		var event insiderSettlementEvent
		if err := json.Unmarshal(received[index*3], &event); err != nil {
			t.Fatal(err)
		}
		if event.EventKind != "reversed" || event.AccessAction != action || event.ReversesEventID != original.EventID || event.AmountCents != original.AmountCents || event.TaxCents != original.TaxCents || event.MerchantName != original.MerchantName || event.SourceReference != original.SourceReference || event.AccessStartsAt != nil || event.AccessEndsAt != nil {
			t.Fatalf("incorrect adjustment: %+v", event)
		}
	}
	if worked, err := processServiceRefund(ctx, app, attemptID, now.Add(time.Hour)); err != nil || worked {
		t.Fatal("accepted refund sent twice", worked, err)
	}
}

func TestBuildInsiderRefundRejectsMismatchedEvidence(t *testing.T) {
	original := insiderSettlementEvent{EventKind: "settled", EventID: "stripe:paid", ProductCode: "insider.membership.monthly", PersonID: "reader", Provider: "stripe", ProviderReference: "cs_paid", AmountCents: 2275, TaxCents: 175, Currency: "USD"}
	job := getters.ServiceRefundJob{AccessAction: "retain", Refund: getters.VerifiedServiceRefund{Provider: "stripe", PaymentReference: "cs_paid", RefundReference: "re_test", AmountCents: 500, Currency: "USD", RefundedAt: time.Now()}}
	for _, tc := range []struct {
		name   string
		change func(*insiderSettlementEvent, *getters.ServiceRefundJob)
	}{
		{"person", func(e *insiderSettlementEvent, j *getters.ServiceRefundJob) { e.PersonID = "other" }},
		{"provider", func(e *insiderSettlementEvent, j *getters.ServiceRefundJob) { j.Refund.Provider = "opennode" }},
		{"payment", func(e *insiderSettlementEvent, j *getters.ServiceRefundJob) { j.Refund.PaymentReference = "cs_other" }},
		{"amount", func(e *insiderSettlementEvent, j *getters.ServiceRefundJob) { j.Refund.AmountCents = 2276 }},
		{"decision", func(e *insiderSettlementEvent, j *getters.ServiceRefundJob) { j.AccessAction = "" }},
		{"campaign", func(e *insiderSettlementEvent, j *getters.ServiceRefundJob) { e.ContributionPublicationID = "article" }},
		{"article without resource", func(e *insiderSettlementEvent, j *getters.ServiceRefundJob) { e.ProductCode = "insider.article" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, j := original, job
			tc.change(&e, &j)
			raw, _ := json.Marshal(e)
			if _, err := buildInsiderRefund(raw, "reader", &j); err == nil {
				t.Fatal("invalid refund accepted")
			}
		})
	}
}

func TestBuildInsiderArticleRefundPreservesReceipt(t *testing.T) {
	original := insiderSettlementEvent{EventKind: "settled", EventID: "stripe:article-paid", ProductCode: "insider.article", ResourceID: "article-one", PersonID: "reader", Provider: "stripe", ProviderReference: "cs_article", AmountCents: 227, TaxCents: 17, Currency: "USD", MerchantName: "Original merchant", SourceReference: "service-purchase:article"}
	raw, _ := json.Marshal(original)
	job := getters.ServiceRefundJob{AccessAction: "retain", Refund: getters.VerifiedServiceRefund{Provider: "stripe", PaymentReference: "cs_article", RefundReference: "re_article", AmountCents: 100, Currency: "USD", RefundedAt: time.Now()}}
	event, err := buildInsiderRefund(raw, "reader", &job)
	if err != nil || event.ResourceID != original.ResourceID || event.AmountCents != 227 || event.TaxCents != 17 || event.AdjustmentAmountCents != 100 || event.AccessAction != "retain" || event.ReversesEventID != original.EventID || event.MerchantName != original.MerchantName {
		t.Fatal("article adjustment changed original receipt", event, err)
	}
}
