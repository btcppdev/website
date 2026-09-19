package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestFindExistingCheckoutRequiresCompleteUniqueMatch(t *testing.T) {
	for _, scenario := range []string{"unique second page", "no match", "duplicate match", "wrong account", "repeated page", "empty continuation", "late provider failure", "match then failure"} {
		t.Run(scenario, func(t *testing.T) {
			in := insiderIntent("insider.membership.monthly")
			match := &stripe.CheckoutSession{ID: "cs_match", Mode: "subscription", Created: in.CreatedAt.Unix(), ClientReferenceID: in.ID, Metadata: map[string]string{"account_id": in.PersonID, "checkout_intent": in.ID, "product": in.ProductCode}}
			calls := 0
			client := CheckoutClient{List: func(p *stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
				calls++
				if p.Limit == nil || *p.Limit != 100 || !p.Single || p.Context == nil || p.CreatedRange == nil {
					t.Fatal("unbounded lookup")
				}
				if calls == 1 {
					if scenario == "match then failure" {
						return &stripe.CheckoutSessionList{ListMeta: stripe.ListMeta{HasMore: true}, Data: []*stripe.CheckoutSession{match}}, nil
					}
					return &stripe.CheckoutSessionList{ListMeta: stripe.ListMeta{HasMore: true}, Data: []*stripe.CheckoutSession{{ID: "cs_unrelated"}}}, nil
				}
				cursor := "cs_unrelated"
				if scenario == "match then failure" {
					cursor = "cs_match"
				}
				if p.StartingAfter == nil || *p.StartingAfter != cursor {
					t.Fatal("pagination cursor missing")
				}
				switch scenario {
				case "no match":
					return &stripe.CheckoutSessionList{}, nil
				case "duplicate match":
					other := *match
					other.ID = "cs_second"
					return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{match, &other}}, nil
				case "wrong account":
					match.Metadata["account_id"] = "other"
				case "repeated page":
					return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{{ID: "cs_unrelated"}}}, nil
				case "empty continuation":
					return &stripe.CheckoutSessionList{ListMeta: stripe.ListMeta{HasMore: true}}, nil
				case "late provider failure", "match then failure":
					return nil, context.DeadlineExceeded
				}
				return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{match}}, nil
			}}
			id, err := client.FindExistingCheckout(context.Background(), in)
			if scenario == "unique second page" {
				if err != nil || id != "cs_match" {
					t.Fatal(id, err)
				}
			} else if err == nil || id != "" {
				t.Fatal("incomplete or ambiguous lookup accepted", id, err)
			}
		})
	}
}
