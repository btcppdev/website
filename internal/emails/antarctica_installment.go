package emails

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
)

var expeditionInstallmentTemplate = template.Must(template.New("installment").Parse(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0;background:#edf3f5;color:#063747;font:16px/1.6 Arial,sans-serif"><table role="presentation" width="100%"><tr><td align="center"><table role="presentation" width="640" style="max-width:640px;width:100%;background:white"><tr><td style="padding:28px"><p style="letter-spacing:2px;font-size:12px">COLD STORAGE EDITION</p><h1 style="font:36px/1.15 Georgia,serif">{{.Title}}</h1><p>{{.Intro}}</p><div style="background:#eaf3f5;padding:18px"><strong>Payment {{.Number}} of {{.Count}} · {{.Amount}} USD</strong><br>Due {{.Due}} · America/Chicago<br>Booking {{.Booking}}<br>January 24–February 5, 2028 expedition</div><p>{{.Body}}</p>{{if .Staff}}<p>Purchaser: {{.Name}} · {{.Recipient}}<br>Passengers: {{.Quantity}}<br>Payments retained: {{.Retained}} USD<br>Processed: {{.Occurred}}<br>Accepted policy: {{.Policy}}<br>Event: {{.Event}}</p><h2>Released berths</h2>{{range .Selections}}<p>{{.Quantity}} × {{.Berth}} · {{.Preference}}</p>{{end}}<h2>Payment records</h2><pre style="white-space:pre-wrap;overflow-wrap:anywhere">{{.Payments}}</pre><p>The customer notice is queued separately. Check delivery status in the staff booking record.</p>{{end}}<p><a href="{{.Link}}" style="display:inline-block;background:#eb7549;color:#063747;padding:12px 20px;text-decoration:none;font-weight:bold">{{.CTA}} →</a></p><p>Questions or a payment that hasn’t appeared? Reply to <a href="mailto:inbox@btcpp.dev">inbox@btcpp.dev</a>.</p><p style="font-size:12px">This is a transactional booking update, separate from newsletters.</p></td></tr></table></td></tr></table></body></html>`))

func AntarcticaInstallmentMail(j *getters.ExpeditionLifecycleMailJob, base string) (*Mail, error) {
	if j == nil || j.EventID == "" || (j.Kind != "reminder" && j.Kind != "abandoned") || (j.Audience != "customer" && j.Audience != "staff") || (j.Audience == "staff" && (j.Kind != "abandoned" || j.Recipient != "inbox@btcpp.dev")) {
		return nil, fmt.Errorf("invalid installment notice")
	}
	if _, err := mail.ParseAddress(j.Recipient); err != nil {
		return nil, err
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid booking URL")
	}
	s := j.Snapshot
	count := s.PaymentCount
	if count == 0 {
		count = 5
	} // Older immutable reminder snapshots used five payments.
	loc, err := time.LoadLocation(s.Zone)
	if err != nil {
		return nil, err
	}
	money := func(c int64) string { return fmt.Sprintf("$%d.%02d", c/100, c%100) }
	due := s.Due.UTC().Format("January 2, 2006")
	cutoff := s.ForfeitAt.In(loc).Add(-time.Second).Format("January 2, 2006 at 3:04 PM MST")
	title, intro := "Your Antarctica installment is coming up", "Your next payment is due in two weeks."
	switch {
	case s.Offset == -4:
		intro = "Your next payment is due in four days."
	case s.Offset == 0:
		title = "Your Antarctica installment is due today"
		intro = "Please complete your payment today."
	case s.Offset > 0:
		title = fmt.Sprintf("Your Antarctica installment is %d days overdue", s.Offset)
		intro = "We haven’t recorded this installment payment yet."
	}
	body := "Please pay using the payment method on your booking page. If this installment remains unpaid through " + cutoff + ", your reservation will be marked abandoned, your berths released, and all payments already made forfeited."
	link := strings.TrimRight(base, "/") + "/expeditions/bookings/" + url.PathEscape(s.BookingID) + "/installments/" + fmt.Sprint(s.Number)
	cta := "Review installment and pay"
	if j.Kind == "abandoned" {
		title = "Your Antarctica reservation has been forfeited"
		intro = "Your installment remained unpaid for 30 days after its deadline."
		body = "Your reservation has been marked abandoned, your berths released, and all payments already made retained under the booking terms. This is forfeiture for non-payment, distinct from a guest-requested cancellation."
		link = strings.TrimRight(base, "/") + "/expeditions/bookings/" + url.PathEscape(s.BookingID)
		cta = "View booking details"
	}
	if j.Audience == "staff" {
		title = "Antarctica booking abandoned / forfeited · " + s.BookingID
		intro = "Automatic forfeiture completed. The reserved berths have been released."
		link = strings.TrimRight(base, "/") + "/expeditions/admin/bookings/" + url.PathEscape(s.BookingID)
		cta = "Review booking and delivery status"
	}
	var pretty bytes.Buffer
	if len(s.Payments) > 0 {
		if err = json.Indent(&pretty, s.Payments, "", "  "); err != nil {
			return nil, err
		}
	}
	d := struct {
		Title, Intro, Body, Amount, Due, Booking, Link, CTA, Name, Recipient, Retained, Occurred, Policy, Event, Payments string
		Number, Quantity, Count                                                                                           int
		Staff                                                                                                             bool
		Selections                                                                                                        any
	}{title, intro, body, money(s.Cents), due, s.BookingID, link, cta, s.Name, s.Recipient, money(s.RetainedCents), s.OccurredAt.In(loc).Format(time.RFC3339), s.Policy, j.EventID, pretty.String(), s.Number, s.Quantity, count, j.Audience == "staff", s.Selections}
	var html bytes.Buffer
	if err = expeditionInstallmentTemplate.Execute(&html, d); err != nil {
		return nil, err
	}
	text := fmt.Sprintf("%s\n\n%s\nPayment %d of %d: %s USD\nDue: %s (America/Chicago, end of day)\nBooking: %s\n\n%s\n\n%s: %s\nQuestions: inbox@btcpp.dev\n", title, intro, s.Number, count, money(s.Cents), due, s.BookingID, body, cta, link)
	if j.Audience == "staff" {
		text += fmt.Sprintf("\nPurchaser: %s <%s>\nPassengers: %d\nRetained: %s USD\nProcessed: %s\nPolicy: %s\nEvent: %s\nAllocations: %+v\nPayments:\n%s\nCustomer notice queued separately; see delivery status at the staff link.\n", s.Name, s.Recipient, s.Quantity, money(s.RetainedCents), d.Occurred, s.Policy, j.EventID, s.Selections, pretty.String())
	}
	return &Mail{JobKey: "expedition-lifecycle:" + j.EventID + ":" + j.Audience, Email: j.Recipient, ReplyTo: "inbox@btcpp.dev", Title: title, SendAt: time.Now(), HTMLBody: html.Bytes(), TextBody: []byte(text)}, nil
}
