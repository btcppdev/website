package main

import (
	"errors"
	"io"
	"testing"
)

func TestBackfillDryRunNeverSends(t *testing.T) {
	got := backfill(io.Discard, []recipient{{email: "a@example.com", role: "owner"}, {role: "member"}}, false, func(recipient) error { t.Fatal("dry run sent mail"); return nil })
	if got.eligible != 1 || got.queued != 0 || got.missingEmail != 1 {
		t.Fatalf("unexpected totals: %+v", got)
	}
}

func TestBackfillContinuesAfterFailureAndCoversEveryRole(t *testing.T) {
	var roles []string
	got := backfill(io.Discard, []recipient{{email: "a@example.com", role: "member"}, {email: "b@example.com", role: "manager"}, {email: "c@example.com", role: "owner"}, {role: "member"}}, true, func(r recipient) error {
		roles = append(roles, r.role)
		if r.role == "manager" {
			return errors.New("mailer unavailable")
		}
		return nil
	})
	if len(roles) != 3 || got.queued != 2 || got.failed != 1 || got.missingEmail != 1 {
		t.Fatalf("roles=%v totals=%+v", roles, got)
	}
}
