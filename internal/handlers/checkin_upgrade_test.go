package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type checkInMockProvider struct {
	sync.Mutex
	charges  map[string]*getters.CheckInUpgradeCharge
	creates  int
	onCreate func(*getters.CheckInUpgradeCharge) error
}

func newCheckInMockProvider() (*httptest.Server, *checkInMockProvider) {
	state := &checkInMockProvider{charges: map[string]*getters.CheckInUpgradeCharge{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/v1/charges" {
			var in types.OpenNodeRequest
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				w.WriteHeader(400)
				return
			}
			state.creates++
			id := uuid.NewString()
			state.charges[id] = &getters.CheckInUpgradeCharge{ID: id, OrderID: in.OrderID, Status: "unpaid", Currency: in.Currency, Fiat: in.Amount, Sats: 250000}
			if state.onCreate != nil {
				if err := state.onCreate(state.charges[id]); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": types.OpenNodePayment{ID: id, OrderID: in.OrderID, Currency: in.Currency, SourceFiatValue: in.Amount, Amount: 250000, ChainInvoice: types.OpenNodeChainInvoice{BTCAddress: "bcrt1qtestonlydonotpay"}, LNInvoice: types.OpenNodeLightningInvoice{Invoice: "lnbcrt2500u1testonlydonotpay", ExpiresAt: uint64(time.Now().Add(10 * time.Minute).Unix())}}})
			return
		}
		charge := state.charges[strings.TrimPrefix(r.URL.Path, "/v2/charge/")]
		if charge == nil {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": charge})
	}))
	return server, state
}

func TestCheckInUpgradeFlow(t *testing.T) {
	app := wizardTestApp(t)
	app.Env.RegistryPin = "checkin-test"
	provider, state := newCheckInMockProvider()
	defer provider.Close()
	app.Env.OpenNode = types.OpenNodeConfig{Endpoint: provider.URL + "/v1", Key: "checkin-mock-key"}
	c := context.Background()
	id, err := getters.CreatePlannedEvent(app, eventplan.Basics{Tag: "checkin-" + uuid.NewString()[:20], Name: "Check-in test", Edition: "global", Timezone: "UTC"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM registrations WHERE conference_id=$1`, id)
		app.DB.Exec(c, `DELETE FROM conferences WHERE id=$1`, id)
	})
	conf, _ := getters.GetConfByRef(app, id)
	raw, _ := json.Marshal(eventplan.TicketSettings{Tickets: []eventplan.Ticket{{Tier: "Standard", Price: 200, Capacity: 200, Currency: "EUR", SalesEnd: "2099-01-01T10:00", TaxCode: "txcd_20030000"}}, Offers: []eventplan.CheckoutOffer{{Label: "Eurocrypt ticket holder", Restrictions: "Show a Eurocrypt ticket.", Percent: 15, Enabled: true}}})
	if _, err = getters.SaveEventPlanning(app, conf, 0, "", "tickets", raw); err != nil {
		t.Fatal(err)
	}
	offers, _ := getters.ListCheckoutOffers(app, id, true)
	insert := func(kind, discount string, paid any) string {
		t.Helper()
		ref := uuid.NewString()
		_, err := app.DB.Exec(c, `INSERT INTO registrations(ref_id,conference_id,email,type,discount_id,amount_paid,currency) VALUES($1,$2,'checkin@example.test',$3,nullif($4,'')::uuid,$5,'EUR')`, ref, id, kind, discount, paid)
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}
	student := insert("student", "", 50)
	local := insert("local", "", 100)
	offer := insert("genpop", offers[0].ID, 170)
	supporter := insert("supporter", "", 500)
	root := mux.NewRouter()
	registerCheckInUpgradeRoutes(root, app)
	root.HandleFunc("/check-in/{ticket}", func(w http.ResponseWriter, r *http.Request) { CheckIn(w, r, app) })
	call := func(path string, values url.Values, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		method := "GET"
		if values != nil {
			method = "POST"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth {
				app.Session.Put(r.Context(), "pin", app.Env.RegistryPin)
			}
			app.Session.Put(r.Context(), authMethodsCSRFKey, "checkin-csrf")
			root.ServeHTTP(w, r)
		})).ServeHTTP(w, r)
		return w
	}
	values := url.Values{"csrf": {"checkin-csrf"}, "revision": {"0"}}
	page := call("/check-in/"+student, nil, true)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Check student ID.") || !strings.Contains(page.Body.String(), "Upgrade to full ticket · EUR 150.00") {
		t.Fatal("student proof page", page.Code, page.Body.String())
	}
	details, _ := getters.GetRegistrationCheckIn(app, student)
	if details.CheckedInAt != nil {
		t.Fatal("scan admitted student before proof")
	}
	if n, e := getters.BulkCheckInRegistrations(app, id, []string{"checkin@example.test"}); e != nil || n != 1 {
		t.Fatal("bulk check-in bypassed proof", n, e)
	}
	page = call("/check-in/"+supporter, nil, true)
	if !strings.Contains(page.Body.String(), "Supporter · helps fund student tickets") {
		t.Fatal("supporter note missing")
	}
	if e := getters.ConfirmCheckInProof(app, local, "staff", 0); e != nil {
		t.Fatal(e)
	}
	details, _ = getters.GetRegistrationCheckIn(app, local)
	if details.CheckedInAt == nil {
		t.Fatal("verified local not admitted")
	}
	if _, err = app.DB.Exec(c, `UPDATE discounts SET checkout_restrictions='Changed requirement' WHERE id=$1`, offers[0].ID); err != nil {
		t.Fatal(err)
	}
	page = call("/check-in/"+offer, nil, true)
	if !strings.Contains(page.Body.String(), "Show a Eurocrypt ticket.") || strings.Contains(page.Body.String(), "Changed requirement") {
		t.Fatal("offer snapshot changed")
	}
	if w := call("/check-in/"+student+"/upgrade", values, false); w.Code != 401 {
		t.Fatal("unauthorized payment", w.Code)
	}
	if w := call("/check-in/"+student+"/upgrade", url.Values{"csrf": {"bad"}, "revision": {"0"}}, true); w.Code != 403 {
		t.Fatal("CSRF accepted", w.Code)
	}
	start := func(ref string) string {
		t.Helper()
		w := call("/check-in/"+ref+"/upgrade", values, true)
		var body map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal("start", w.Code, w.Body.String())
		}
		return body["id"].(string)
	}
	upgradeID := start(student)
	if second := start(student); second != upgradeID || state.creates != 1 {
		t.Fatal("duplicate invoice", second, state.creates)
	}
	if w := call("/check-in/"+student+"/proof", url.Values{"csrf": {"bad"}, "revision": {"0"}}, true); w.Code != 403 {
		t.Fatal("proof CSRF accepted", w.Code)
	}
	u, err := getters.GetCheckInUpgrade(app, upgradeID, student)
	if err != nil || u.AmountCents != 15000 || u.FullPriceCents != 20000 || u.Status != "pending" {
		t.Fatal("wrong quote", u, err)
	}
	base := "/check-in/" + student + "/upgrade/" + upgradeID
	for _, network := range []string{"lightning", "onchain"} {
		w := call(base+"/qr?network="+network, nil, true)
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
			t.Fatal("QR", network, w.Code, w.Body.String())
		}
	}
	if w := call("/check-in/"+offer+"/upgrade/"+upgradeID, nil, true); w.Code != 404 {
		t.Fatal("foreign ticket can read upgrade", w.Code)
	}
	state.Lock()
	charge := state.charges[u.ChargeID]
	charge.Status = "processing"
	state.Unlock()
	if w := call(base, nil, true); w.Code != 200 || !strings.Contains(w.Body.String(), "processing") {
		t.Fatal(w.Code, w.Body.String())
	}
	details, _ = getters.GetRegistrationCheckIn(app, student)
	if details.CheckedInAt != nil {
		t.Fatal("unconfirmed on-chain payment admitted")
	}
	state.Lock()
	charge.Status = "paid"
	charge.Fiat = 1
	state.Unlock()
	if w := call(base, nil, true); w.Code != 503 {
		t.Fatal("wrong amount accepted", w.Code)
	}
	state.Lock()
	charge.Fiat = 150
	state.Unlock()
	mac := hmac.New(sha256.New, []byte(app.Env.OpenNode.Key))
	mac.Write([]byte(u.ChargeID))
	callback := url.Values{"id": {u.ChargeID}, "hashed_order": {hex.EncodeToString(mac.Sum(nil))}, "status": {"unpaid"}}
	for i := 0; i < 2; i++ {
		if w := call("/callback/opennode-ticket-upgrade/"+u.ID, callback, false); w.Code != 204 {
			t.Fatal("callback", w.Code, w.Body.String())
		}
	}
	details, _ = getters.GetRegistrationCheckIn(app, student)
	if details.CheckedInAt == nil || details.TicketType != "genpop" {
		t.Fatal("confirmed payment not applied", details)
	}
	u, _ = getters.GetCheckInUpgrade(app, u.ID, student)
	if u.Status != "paid" {
		t.Fatal("payment status", u)
	}
	var count int
	app.DB.QueryRow(c, `SELECT count(*) FROM registrations WHERE ref_id=$1`, student).Scan(&count)
	if count != 1 {
		t.Fatal("upgrade created another ticket")
	}
	// A transfer while the QR is open never admits the new owner on an old quote.
	transferred := insert("student", "", 50)
	transferID := start(transferred)
	transfer, _ := getters.GetCheckInUpgrade(app, transferID, transferred)
	app.DB.Exec(c, `UPDATE registrations SET email='newowner@example.test' WHERE ref_id=$1`, transferred)
	state.Lock()
	state.charges[transfer.ChargeID].Status = "paid"
	state.Unlock()
	if err = reconcileCheckInUpgrade(app, transfer); err != nil {
		t.Fatal(err)
	}
	transfer, _ = getters.GetCheckInUpgrade(app, transferID, transferred)
	if transfer.Status != "review" {
		t.Fatal("transferred payment granted", transfer.Status)
	}
	if err = getters.ConfirmCheckInProof(app, transferred, "staff", 0); err == nil {
		t.Fatal("stale proof accepted after transfer")
	}
	// An unpaid webhook can race the creation response; retain its QR details.
	state.Lock()
	state.onCreate = func(p *getters.CheckInUpgradeCharge) error {
		return getters.ApplyCheckInUpgradePayment(app, p.OrderID, p)
	}
	state.Unlock()
	racing := insert("student", "", 50)
	racingID := start(racing)
	raced, _ := getters.GetCheckInUpgrade(app, racingID, racing)
	if raced.Invoice == "" || raced.ChainAddress == "" || raced.Status != "pending" {
		t.Fatal("callback race lost QR", raced)
	}
	state.Lock()
	state.onCreate = nil
	state.charges[raced.ChargeID].Status = "underpaid"
	state.Unlock()
	if err = reconcileCheckInUpgrade(app, raced); err != nil {
		t.Fatal(err)
	}
	details, _ = getters.GetRegistrationCheckIn(app, racing)
	if details.CheckedInAt != nil {
		t.Fatal("underpayment admitted")
	}
	state.Lock()
	state.charges[raced.ChargeID].Status = "expired"
	state.Unlock()
	if err = reconcileCheckInUpgrade(app, raced); err != nil {
		t.Fatal(err)
	}
	state.Lock()
	state.charges[raced.ChargeID].Status = "paid"
	state.Unlock()
	raced, _ = getters.GetCheckInUpgrade(app, racingID, racing)
	if err = reconcileCheckInUpgrade(app, raced); err != nil {
		t.Fatal(err)
	}
	raced, _ = getters.GetCheckInUpgrade(app, racingID, racing)
	if raced.Status != "review" {
		t.Fatal("late payment silently granted", raced.Status)
	}
	// Zero balance upgrades require an explicit staff action but no invoice.
	covered := insert("local", "", 250)
	zeroID := start(covered)
	zero, _ := getters.GetCheckInUpgrade(app, zeroID, covered)
	if zero.Status != "paid" || zero.AmountCents != 0 || zero.ChargeID != "" {
		t.Fatal("zero-balance upgrade", zero)
	}
	missing := insert("student", "", nil)
	if w := call("/check-in/"+missing+"/upgrade", values, true); w.Code != 409 {
		t.Fatal("unknown paid amount charged", w.Code)
	}
	// Special-offer standard tickets use the same price-difference calculation.
	offerID := start(offer)
	special, _ := getters.GetCheckInUpgrade(app, offerID, offer)
	if special.AmountCents != 3000 {
		t.Fatal("offer price difference", special)
	}
	// No payment QR or mutation is available after ticket revocation.
	app.DB.Exec(c, `UPDATE registrations SET revoked=true WHERE ref_id=$1`, offer)
	if w := call("/check-in/"+offer+"/upgrade/"+offerID+"/qr", nil, true); w.Code != 409 {
		t.Fatal("revoked QR", w.Code)
	}
}

func installCheckInUpgradePreview(t *testing.T, app *config.AppContext, root *mux.Router, conf *types.Conf) {
	t.Helper()
	provider, _ := newCheckInMockProvider()
	t.Cleanup(provider.Close)
	app.Env.OpenNode = types.OpenNodeConfig{Endpoint: provider.URL + "/v1", Key: "local-preview-only"}
	app.Env.RegistryPin = "preview"
	// Mock charges disappear on restart; discard only this fixture's invoices.
	if _, err := app.DB.Exec(app.DatabaseContext(), `DELETE FROM ticket_checkin_upgrades WHERE ticket_ref IN ('dev-upgrade-student','dev-upgrade-local','dev-upgrade-supporter','dev-upgrade-offer')`); err != nil {
		t.Fatal(err)
	}
	registerCheckInUpgradeRoutes(root, app)
	root.HandleFunc("/check-in/{ticket}", func(w http.ResponseWriter, r *http.Request) { CheckIn(w, r, app) }).Methods("GET", "POST")
	root.HandleFunc("/preview/check-in/{kind}", func(w http.ResponseWriter, r *http.Request) {
		kind := mux.Vars(r)["kind"]
		paid := 50
		ticketType := kind
		discount := ""
		switch kind {
		case "student":
		case "local":
			paid = 100
		case "supporter":
			paid = 500
		case "offer":
			paid = 170
			ticketType = "genpop"
			offers, _ := getters.ListCheckoutOffers(app, conf.Ref, true)
			if len(offers) > 0 {
				discount = offers[0].ID
			}
		default:
			http.NotFound(w, r)
			return
		}
		ref := "dev-upgrade-" + kind
		_, err := app.DB.Exec(app.DatabaseContext(), `INSERT INTO registrations(ref_id,conference_id,type,email,attendee_name,attendee_tshirt,amount_paid,currency,discount_id,platform) VALUES($1,$2,$3,'checkin-preview@example.test','Demo attendee','MM',$4,'EUR',nullif($5,'')::uuid,'dev-checkin-preview') ON CONFLICT(ref_id) DO NOTHING`, ref, conf.Ref, ticketType, paid, discount)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		app.Session.Put(r.Context(), "pin", app.Env.RegistryPin)
		http.Redirect(w, r, "/check-in/"+ref+"?msg="+url.QueryEscape("Local demo · test QR only; no real payment."), 303)
	})
}
