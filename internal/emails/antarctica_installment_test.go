package emails

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/expedition"
	"strings"
	"testing"
	"time"
)

func TestAntarcticaInstallmentMail(t *testing.T) {
	due := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	d, _ := expedition.InstallmentDeadline(due, "America/Chicago")
	j := &getters.ExpeditionLifecycleMailJob{EventID: "event", Audience: "customer", Recipient: "guest@example.test", Kind: "reminder", Snapshot: getters.ExpeditionLifecycleSnapshot{BookingID: "booking", Recipient: "guest@example.test", Number: 2, Offset: -14, Due: due, Deadline: d, ForfeitAt: d.AddDate(0, 0, 30), Zone: "America/Chicago", Cents: 334000}}
	m, err := AntarcticaInstallmentMail(j, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{string(m.HTMLBody), string(m.TextBody)} {
		for _, want := range []string{"$3340.00", "January 1, 2027", "America/Chicago", "January 31, 2027", "abandoned", "forfeited", "/expeditions/bookings/booking/installments/2", "inbox@btcpp.dev"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %s", want)
			}
		}
	}
	if m.Sub != "" || m.Missive != "" || m.JobKey != "expedition-lifecycle:event:customer" {
		t.Fatal("nontransactional or unstable identity")
	}
	j.Kind = "abandoned"
	j.Audience = "staff"
	j.Recipient = "inbox@btcpp.dev"
	staff, err := AntarcticaInstallmentMail(j, "https://btcpp.dev")
	if err != nil || !strings.Contains(string(staff.TextBody), "/expeditions/admin/bookings/booking") {
		t.Fatal("staff notice", err)
	}
	if staff.JobKey == m.JobKey {
		t.Fatal("staff and customer duplicate keys")
	}
	j.Recipient = "wrong@example.test"
	if _, err = AntarcticaInstallmentMail(j, "https://btcpp.dev"); err == nil {
		t.Fatal("staff recipient not constrained")
	}
}
