package getters

import (
	"btcpp-web/internal/types"
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestDatabaseSmokeRegistrationLocale(t *testing.T) {
	app := databaseSmokeContext(t)
	conf, _ := insertSmokeConference(t, app)
	for _, tc := range []struct{ input, want string }{{"ko", "ko"}, {"", "en"}, {"xx", "en"}} {
		entry := &types.Entry{ID: uuid.NewString(), ConfRef: conf, Email: "locale-" + uuid.NewString() + "@example.test", Locale: tc.input, Created: time.Now(), Currency: "USD", Items: []types.Item{{Desc: "Pass", Type: "general", Total: 38900}}}
		if err := AddTickets(app, entry, "stripe"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.DB.Exec(context.Background(), `DELETE FROM registrations WHERE checkout_id=$1`, entry.ID) })
		// Replayed webhooks must continue to produce one ticket with its preference.
		if err := AddTickets(app, entry, "stripe"); err != nil {
			t.Fatal(err)
		}
		regs, err := ListRegistrationsByCheckoutID(app, entry.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(regs) != 1 || regs[0].Locale != tc.want {
			t.Fatalf("locale round trip: %+v", regs)
		}
	}
}
