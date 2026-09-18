package handlers

import "testing"

func TestValidInsiderTransactionalKind(t *testing.T) {
	for _, kind := range []string{
		"insider.subscription_confirmation",
		"insider.editorial_test",
		"insider.receipt",
		"insider.receipt_adjustment",
	} {
		if !validInsiderTransactionalKind(kind) {
			t.Fatalf("rejected supported Insider transactional kind %q", kind)
		}
	}
	for _, kind := range []string{"", "insider.campaign", "receipt", "insider.receipt.adjustment"} {
		if validInsiderTransactionalKind(kind) {
			t.Fatalf("accepted unsupported Insider transactional kind %q", kind)
		}
	}
}
