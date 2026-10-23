package emails

import (
	"btcpp-web/internal/ics"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/expedition"
)

type antarcticaMailSection struct{ Title, Body string }
type antarcticaMailData struct {
	Base, Image, BookingURL, ReceiptURL, Title, Intro, Summary, Payment, Tax string
	Welcome                                                                  bool
	Sections                                                                 []antarcticaMailSection
}

var antarcticaBookingTemplate = template.Must(template.New("antarctica-booking").Parse(`<!doctype html>
<html lang="en"><head><meta name="viewport" content="width=device-width,initial-scale=1"><style>@font-face{font-family:UbuntuBrand;src:url('{{.Base}}/static/fonts/Ubuntu-BoldItalic.ttf')}body{margin:0}a{color:#ad4123}</style></head>
<body style="margin:0;background:#edf3f5;color:#063747;font-family:Arial,sans-serif;line-height:1.6">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td align="center"><table role="presentation" width="640" cellspacing="0" cellpadding="0" style="width:100%;max-width:640px;background:#fff">
<tr><td style="padding:24px 28px"><span style="font-family:UbuntuBrand,Ubuntu,Arial,sans-serif;font-size:27px;font-weight:700;font-style:italic">bitcoin<span style="letter-spacing:-.19em;margin-right:.19em">++</span></span><br><span style="font-size:11px;letter-spacing:2px">COLD STORAGE EDITION</span></td></tr>
{{if .Welcome}}<tr><td><img src="{{.Image}}" alt="An expedition ship, explorers and penguins among Antarctic ice beneath an orange sky" width="640" style="display:block;width:100%;max-width:640px;height:auto;border:0"></td></tr>{{end}}
<tr><td style="padding:28px"><h1 style="font-family:Georgia,serif;font-size:38px;line-height:1.15;margin:0 0 20px">{{.Title}}</h1><p>{{.Intro}}</p>
<p style="padding:18px;background:#eaf3f5"><strong>{{.Summary}}</strong><br>January 24–February 5, 2028<br>Ushuaia, Argentina → Antarctica → Ushuaia</p>
{{if .Welcome}}<div style="border-top:2px solid #eb7549;border-bottom:1px solid #93b6c1;padding:14px 0;margin:20px 0"><p style="margin:6px 0"><strong>Arrive in Ushuaia: January 23, 2028 or earlier</strong></p><p style="margin:6px 0"><strong>Embark: January 24 · Disembark: February 5</strong></p><p style="margin:6px 0"><strong>Recommended earliest return flight: February 6, 2028</strong></p><p style="margin:6px 0;font-size:13px">All dates are local to Ushuaia. See travel guidance below.</p></div>{{end}}
<p>{{.Payment}}<br>{{.Tax}}</p><p><a href="{{.BookingURL}}" style="display:inline-block;padding:12px 20px;background:#eb7549;color:#002b38;text-decoration:none;font-weight:bold">View your booking &amp; payment schedule →</a></p><p><a href="{{.ReceiptURL}}">View your payment receipt</a> · Sign in to your account to view these details.</p><p><a href="{{.Base}}/dashboard/orders">Open your dashboard →</a> to find your expedition booking and payment history. To request changes to your trip details, email <a href="mailto:inbox@btcpp.dev">inbox@btcpp.dev</a>.</p>
{{range .Sections}}<h2 style="font-family:Georgia,serif;font-size:26px;line-height:1.2;margin-top:30px">{{.Title}}</h2><p>{{.Body}}</p>{{end}}
{{if .Welcome}}<p><a href="mailto:inbox@btcpp.dev?subject=Antarctica%20travel%20planning">Ask us about travel-booking help →</a></p><p><a href="https://oceanwide-expeditions.com/faq/what-type-of-insurance-do-you-recommend-for-the-voyages">Check Oceanwide’s insurance requirements</a></p><p>We can’t wait to share this adventure with you.<br>See you at the end of the world!</p>{{end}}
<p style="font-size:12px;color:#426674">Booking updates from your expedition team. Reply to inbox@btcpp.dev with questions.</p></td></tr></table></td></tr></table></body></html>`))

// AntarcticaBookingMail replaces the first payment receipt with a welcome only
// after successful berth fulfillment. Later payments get an expedition receipt.
// It retains the shared order job key so delivery retries remain idempotent.
func AntarcticaBookingMail(order *getters.ServiceOrderView, b *getters.ExpeditionBooking, recipient, baseURI string) (*Mail, error) {
	m, err := ServiceReceiptMail(order, recipient, baseURI)
	if err != nil {
		return nil, err
	}
	var f expedition.Fulfillment
	if order.RefundedCents > 0 || order.RefundedSatoshis > 0 || b == nil || b.State != "confirmed" || b.Quantity < 1 || order.Quote.ProductCode != expedition.ProductCode || json.Unmarshal(order.Quote.Fulfillment, &f) != nil || f.Version != 1 || f.ReservationID != b.ID || order.Quote.ResourceID != b.ID || f.Installment < 1 {
		return nil, errors.New("confirmed expedition booking required")
	}
	applied := false
	for _, p := range b.Payments {
		if p.RefundedCents > 0 || p.OrderStatus == "refunded" {
			return nil, errors.New("refunded booking requires review")
		}
		if p.Number == f.Installment && p.Applied && p.OrderPublicID == order.PublicID && p.OrderStatus == "paid" && p.Cents == order.TotalCents {
			applied = true
		}
	}
	if !applied {
		return nil, errors.New("payment has not been applied to this booking")
	}
	base := strings.TrimRight(baseURI, "/")
	d := antarcticaMailData{Base: base, Image: base + "/static/img/antarctica/leading.png", BookingURL: base + "/expeditions/bookings/" + url.PathEscape(b.ID), ReceiptURL: base + "/services/orders/" + url.PathEscape(order.PublicID), Welcome: f.Installment == 1}
	noun := "passengers"
	if b.Quantity == 1 {
		noun = "passenger"
	}
	d.Summary = fmt.Sprintf("%d %s · passage reserved", b.Quantity, noun)
	d.Payment = fmt.Sprintf("Payment received: $%.2f USD.", float64(order.TotalCents)/100)
	d.Tax = fmt.Sprintf("Included tax: $%.2f USD. Order: %s.", float64(order.TaxCents)/100, order.PublicID)
	d.Title = "Your expedition payment is in!"
	d.Intro = "Thank you! We’ve applied this payment to your Antarctica booking. Your latest balance and payment schedule are available below."
	m.Title = "Your Antarctica payment receipt"
	if d.Welcome {
		d.Title = "You’re going to Antarctica!"
		d.Intro = "You’re in! Your passage is reserved for an extraordinary adventure: thirteen days of ice, ocean, penguins, and conversations with your fellow bitcoiners. Cold Storage Edition is happening—and you’re part of it."
		m.Title = "You’re going to Antarctica! Your passage is reserved"
		if b.Plan == expedition.Installments {
			d.Payment += " Your first payment secures your reservation; the remaining installments are still due on your booking schedule."
		} else {
			d.Payment += " Your passage fare is paid in full."
		}
		m.Files = append(m.Files, &EmailFile{
			Name:        "bitcoin-plus-plus-antarctica-2028.ics",
			ContentType: "text/calendar; charset=utf-8; method=PUBLISH",
			Bytes: ics.Render(ics.Event{
				Method: ics.MethodPublish, UID: "antarctica-2028@btcpp.dev",
				AllDay: true, Start: time.Date(2028, time.January, 24, 0, 0, 0, 0, time.UTC),
				End:         time.Date(2028, time.February, 6, 0, 0, 0, 0, time.UTC),
				Summary:     "bitcoin++ Antarctica — Cold Storage Edition",
				Location:    "Ushuaia, Argentina → Antarctica → Ushuaia",
				Description: "Expedition: January 24–February 5, 2028. All dates are local to Ushuaia.\nArrive in Ushuaia by January 23, 2028 or earlier. Recommended earliest return flight: February 6, 2028; this is a planning buffer, not an operator-confirmed flight cutoff. Exact boarding details will follow.\nYou are responsible for travel to and from Ushuaia and required travel insurance. See your welcome email for details. We will send a packing list closer to departure.\nQuestions or paid travel-booking help: inbox@btcpp.dev",
			}),
		})
		d.Sections = []antarcticaMailSection{
			{"Add the expedition to your calendar", "Open the attached bitcoin-plus-plus-antarctica-2028.ics file to save January 24–February 5, 2028. The calendar notes include our arrival and return-flight guidance. Please reserve additional time for travel."},
			{"Your travel dates—save these now", "Arrive in Ushuaia by January 23, 2028, at the latest: the day before we embark on January 24. Earlier is even better for a little breathing room. We are scheduled to disembark in Ushuaia on February 5. The earliest return-flight date we recommend from Ushuaia is February 6, 2028, giving you an overnight buffer. This is our planning recommendation, not an operator-confirmed flight cutoff or a guarantee against delays. Contact us before booking a February 5 flight; we still need Oceanwide’s departure-specific disembarkation and earliest-flight guidance. All these dates are local to Ushuaia."},
			{"Getting to Argentina", "Plan your flights all the way to Ushuaia (USH), not just to Buenos Aires. If connecting through Buenos Aires, check whether your flights use Ezeiza (EZE) or Aeroparque (AEP), and allow time for any airport change, immigration and baggage collection. Arrange a hotel for the night before embarkation and, following our recommended return buffer, the night of February 5. Confirm your passport, visa and transit requirements for your nationality and route. We’ll send the exact meeting point and boarding instructions closer to departure."},
			{"Your travel is your responsibility", "You are responsible for arranging and paying for travel to and from Ushuaia, our departure and return point, including flights, pre- and post-cruise accommodation, and transfers not expressly included in the voyage. Leave room for flight disruption: the ship cannot wait for delayed passengers or luggage. Allow generous time for onward international connections, too."},
			{"Insurance: a must before we sail", "Arrange travel insurance covering medical treatment, accidents, medical evacuation and repatriation in Antarctica. Oceanwide currently requires at least EUR/USD 100,000 per person for medical evacuation and repatriation in Antarctica and the sub-Antarctic islands. Ask your insurer to confirm coverage for your planned activities and any pre-existing conditions. Insurance is not included in your fare. We strongly recommend cancellation and trip-interruption cover as well. We’ll follow up about proof of insurance and required traveler forms."},
			{"Your traveler form", "Each passenger must complete Oceanwide’s Personal Information Form (PIF), including relevant medical information. Oceanwide’s published deadline is within one month of booking. We’ll send your personal completion link and instructions separately; please don’t send passport or medical records by replying to this email. Boot sizing and activity forms may also be required."},
			{"Want a hand with flights and hotels?", "Reply to this email or contact inbox@btcpp.dev if you’d like help arranging your Argentina travel. Travel-booking assistance is available for an additional fee; we’ll explain the scope and quote the fee before you commit."},
			{"We’ll help you get ready", "We’ll send a packing list as the trip gets closer, along with boarding details and other expedition updates. For now: save the dates, start thinking about your travel, and get excited. If you booked for a group, please share this email with every passenger—each person needs their own travel arrangements, insurance and required forms. Cabin numbers and roommate preferences are not guaranteed."},
		}
	}
	var body bytes.Buffer
	if err := antarcticaBookingTemplate.Execute(&body, d); err != nil {
		return nil, err
	}
	var text strings.Builder
	fmt.Fprintf(&text, "%s\n\n%s\n\n%s\nJanuary 24–February 5, 2028\nUshuaia, Argentina → Antarctica → Ushuaia\n\n%s\n%s\n\nBooking and payment schedule (sign in): %s\nReceipt: %s\n", d.Title, d.Intro, d.Summary, d.Payment, d.Tax, d.BookingURL, d.ReceiptURL)
	fmt.Fprintf(&text, "\nDashboard: %s/dashboard/orders\nFind your expedition booking and payment history here. To request changes to your trip details, email inbox@btcpp.dev.\n", base)
	for _, s := range d.Sections {
		fmt.Fprintf(&text, "\n%s\n%s\n", s.Title, s.Body)
	}
	if d.Welcome {
		text.WriteString("\nInsurance requirements: https://oceanwide-expeditions.com/faq/what-type-of-insurance-do-you-recommend-for-the-voyages\n\nWe can’t wait to share this adventure with you. See you at the end of the world!\n")
	}
	text.WriteString("\nQuestions? Reply to inbox@btcpp.dev.\n")
	m.HTMLBody, m.TextBody, m.ReplyTo = body.Bytes(), []byte(text.String()), "inbox@btcpp.dev"
	return m, nil
}
