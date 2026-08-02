package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
)

func Ticket(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	params := mux.Vars(r)
	ticket := params["ticket"]

	tixType, _ := helpers.GetSessionKey("type", r)
	confRef, _ := helpers.GetSessionKey("conf", r)

	/* make it pretty */
	if tixType == "genpop" {
		tixType = "general"
	}

	conf, err := getters.GetConfByRef(ctx, confRef)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ticket-pdf unable to load conf! %s", err)
		return
	}

	if conf == nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ticket-pdf unable to find conf! %s", confRef)
		return
	}

	dataURI, err := ticketQRCodeURI(ctx, ticket)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ticket-pdf unable to render qr for %s: %s", ticket, err)
		return
	}

	tix := &TicketTmpl{
		QRCodeURI: dataURI,
		CSS:       helpers.MiniCss(),
		Domain:    ctx.Env.GetDomain(),
		Type:      tixType,
		Conf:      conf,
	}

	err = ctx.TemplateCache.ExecuteTemplate(w, "emails/ticket.tmpl", tix)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Infos.Printf("/ticket-pdf ExecuteTemplate failed ! %s", err.Error())
	}
}

// TicketPDF renders the same ticket HTML view as /ticket/{ref} but pipes
// it through headless Chrome to produce a downloadable PDF. Used by the
// dashboard "Download ticket" button so users get a saveable file
// instead of a browser tab they have to print themselves.
func TicketPDF(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	ticket := mux.Vars(r)["ticket"]
	tixType := r.URL.Query().Get("type")
	confRef := r.URL.Query().Get("conf")

	// Build the internal URL chrome will fetch — same URL pattern as the
	// HTML view, just with auto-print disabled (the HTML page is public,
	// no auth needed).
	q := url.Values{}
	if tixType != "" {
		q.Set("type", tixType)
	}
	if confRef != "" {
		q.Set("conf", confRef)
	}
	internalURL := fmt.Sprintf("%s/ticket/%s?%s", ctx.Env.GetURI(), ticket, q.Encode())

	pdfBytes, err := helpers.BuildChromePdf(ctx, &helpers.PDFPage{
		URL:    internalURL,
		Width:  8.5,
		Height: 11,
	})
	if err != nil {
		http.Error(w, "Could not generate ticket PDF", http.StatusInternalServerError)
		ctx.Err.Printf("/ticket/%s/pdf chromedp failed: %s", ticket, err)
		return
	}

	// Friendly filename: ticket-{conf-tag}-{first8ofref}.pdf
	confName := "btcpp"
	if confRef != "" {
		if conf, _ := getters.GetConfByRef(ctx, confRef); conf != nil && conf.Tag != "" {
			confName = conf.Tag
		}
	}
	shortRef := ticket
	if len(shortRef) > 8 {
		shortRef = shortRef[:8]
	}
	filename := fmt.Sprintf("ticket-%s-%s.pdf", confName, shortRef)

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.Write(pdfBytes)
}

// SendCals fans the self-hosted ICS calendar pipeline across every
// scheduled talk for a conf. Replaces the previous Google Calendar
// API call with internal RFC-5545 generation; CalNotif is now the
// "UID:Sequence:Hashbytes" triple maintained by DispatchTalkICSForTalk.
//
// Idempotent: a re-click that doesn't change any talk's start/end/
// title hash will skip emails entirely (no SEQUENCE bump, no
// duplicate invitation in recipients' calendars). Re-running after
// a schedule edit fans out an UPDATE with seq+1.
func SendCals(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil || conf == nil {
		handle404(w, r, ctx)
		return
	}

	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to fetch talks: %s", err.Error())
		return
	}

	for _, talk := range talks {
		if talk.Sched == nil || talk.Sched.End == nil {
			ctx.Err.Printf("Can't send cals for %s talk: no end time??", talk.Name)
			continue
		}
		if err := DispatchTalkICSForTalk(ctx, talk, conf, kindRequest, false); err != nil {
			ctx.Err.Printf("send cals %q: %s", talk.Name, err)
		}
	}
}

func CheckIn(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	switch r.Method {
	case http.MethodGet:
		CheckInGet(w, r, ctx)
		return
	case http.MethodPost:
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		pin := r.Form.Get("pin")
		if pin != ctx.Env.RegistryPin {
			w.WriteHeader(http.StatusBadRequest)
			err := ctx.TemplateCache.ExecuteTemplate(w, "checkin.tmpl", &CheckInPage{
				NeedsPin: true,
				Msg:      "Wrong pin",
				Year:     helpers.CurrentYear(),
			})
			if err != nil {
				http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
				ctx.Err.Printf("/check-in ExecuteTemplate failed ! %s", err.Error())
				return
			}
			ctx.Err.Printf("/check-in wrong pin submitted! %s", pin)
			return
		}

		/* Set pin?? */
		ctx.Session.Put(r.Context(), "pin", pin)
		CheckInGet(w, r, ctx)
	}
}

func CheckInGet(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	/* Check for logged in */
	pin := ctx.Session.GetString(r.Context(), "pin")

	if pin == "" {
		w.Header().Set("x-missing-field", "pin")
		w.WriteHeader(http.StatusBadRequest)
		err := ctx.TemplateCache.ExecuteTemplate(w, "checkin.tmpl", &CheckInPage{
			NeedsPin: true,
			Year:     helpers.CurrentYear(),
		})
		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/check-in ExecuteTemplate failed ! %s", err.Error())
		}
		return
	}

	if pin != ctx.Env.RegistryPin {
		w.WriteHeader(http.StatusUnauthorized)
		err := ctx.TemplateCache.ExecuteTemplate(w, "checkin.tmpl", &CheckInPage{
			Msg:  "Wrong registration PIN",
			Year: helpers.CurrentYear(),
		})
		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/check-in ExecuteTemplate failed ! %s", err.Error())
		}
		return
	}

	params := mux.Vars(r)
	ticket := params["ticket"]

	tix_type, ok, err := getters.CheckIn(ctx, ticket)
	if !ok && err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to check-in %s: %s", ticket, err.Error())
		return
	}

	var msg string
	if err != nil {
		msg = err.Error()
		ctx.Infos.Println("check-in problem:", msg)
	}
	if flash := strings.TrimSpace(r.URL.Query().Get("msg")); flash != "" {
		if msg != "" {
			msg += " "
		}
		msg += flash
	}
	checkInDetails, detailsErr := getters.GetRegistrationCheckIn(ctx, ticket)
	if detailsErr != nil {
		ctx.Err.Printf("/check-in/%s registration details: %s", ticket, detailsErr)
	} else {
		tix_type = checkInDetails.TicketType
	}
	merchPickups, pickupErr := getters.ListShopPickupsForTicket(ctx, ticket)
	if pickupErr != nil {
		ctx.Err.Printf("/check-in/%s merch pickups: %s", ticket, pickupErr)
		msg = strings.TrimSpace(msg + " Could not load merch pickups.")
	}
	page := &CheckInPage{
		TicketType:   tix_type,
		TicketRef:    ticket,
		Msg:          msg,
		MerchPickups: merchPickups,
		Year:         helpers.CurrentYear(),
	}
	if checkInDetails != nil {
		page.AttendeeName = checkInDetails.AttendeeName
		page.AttendeeEmail = checkInDetails.Email
		page.TShirtSize = checkInDetails.TShirtSize
		page.ConferenceTag = checkInDetails.ConferenceTag
		page.ConferenceImage = confImagePath(checkInDetails.ConferenceTag, "leading")
		page.CheckInComplete = checkInDetails.CheckedInAt != nil && !checkInDetails.Revoked
		page.ShirtPickedUp = checkInDetails.ShirtPickedUpAt != nil
	}
	err = ctx.TemplateCache.ExecuteTemplate(w, "checkin.tmpl", page)

	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/check-in ExecuteTemplate failed ! %s", err.Error())
	}
}

func CheckInPickups(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	pin := ctx.Session.GetString(r.Context(), "pin")
	if pin == "" || pin != ctx.Env.RegistryPin {
		http.Error(w, "check-in pin required", http.StatusUnauthorized)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ticket := strings.TrimSpace(mux.Vars(r)["ticket"])
	itemIDs := r.Form["pickup_item_ids"]
	includeConferenceShirt := r.Form.Get("conference_shirt") == "1"
	if err := getters.MarkTicketPickups(ctx, ticket, itemIDs, includeConferenceShirt, "check-in"); err != nil {
		ctx.Err.Printf("/check-in/%s/pickups: %s", ticket, err)
		http.Redirect(w, r, "/check-in/"+url.PathEscape(ticket)+"?msg="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/check-in/"+url.PathEscape(ticket)+"?msg="+url.QueryEscape("Selected items marked picked up."), http.StatusSeeOther)
}

func CheckInMerchPickup(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	pin := ctx.Session.GetString(r.Context(), "pin")
	if pin == "" || pin != ctx.Env.RegistryPin {
		http.Error(w, "check-in pin required", http.StatusUnauthorized)
		return
	}
	ticket := strings.TrimSpace(mux.Vars(r)["ticket"])
	itemID := strings.TrimSpace(mux.Vars(r)["itemID"])
	if itemID == "" {
		http.Redirect(w, r, "/check-in/"+url.PathEscape(ticket), http.StatusSeeOther)
		return
	}
	if err := getters.MarkShopOrderItemPickedUpForTicket(ctx, ticket, itemID, "check-in", "QR check-in merch pickup"); err != nil {
		ctx.Err.Printf("/check-in/%s/merch/%s: %s", ticket, itemID, err)
		http.Redirect(w, r, "/check-in/"+url.PathEscape(ticket)+"?msg="+url.QueryEscape("Could not mark merch pickup."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/check-in/"+url.PathEscape(ticket)+"?msg="+url.QueryEscape("Merch pickup marked complete."), http.StatusSeeOther)
}

func DevCheckInPreviewIndex(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if ctx.Env.Prod {
		handle404(w, r, ctx)
		return
	}
	previews, err := getters.ListDevRegistrationCheckInPreviews(ctx)
	if err != nil {
		http.Error(w, "Unable to load check-in previews", http.StatusInternalServerError)
		ctx.Err.Printf("/dev/check-in previews: %s", err)
		return
	}
	rows := make([]*DevCheckInPreviewRow, 0, len(previews))
	for _, preview := range previews {
		pickups, pickupErr := getters.ListShopPickupsForTicket(ctx, preview.TicketRef)
		if pickupErr != nil {
			ctx.Err.Printf("/dev/check-in/%s pickup summary: %s", preview.TicketRef, pickupErr)
		}
		pending, completed := 0, 0
		if preview.TShirtSize != "" {
			if preview.ShirtPickedUpAt == nil {
				pending++
			} else {
				completed++
			}
		}
		for _, pickup := range pickups {
			if pickup.Status == types.ShopItemStatusFulfilled {
				completed++
			} else {
				pending++
			}
		}
		pickupSummary := "No pickup items"
		switch {
		case pending > 0 && completed > 0:
			pickupSummary = fmt.Sprintf("%d pending · %d complete", pending, completed)
		case pending > 0:
			pickupSummary = fmt.Sprintf("%d pending", pending)
		case completed > 0:
			pickupSummary = fmt.Sprintf("%d complete", completed)
		}
		checkInState := "Ready to scan"
		if preview.CheckedInAt != nil && !preview.Revoked {
			checkInState = "Checked in"
		}
		rows = append(rows, &DevCheckInPreviewRow{
			TicketRef:     preview.TicketRef,
			TicketType:    preview.TicketType,
			TicketLabel:   checkInTicketTypeLabel(preview.TicketType),
			TicketTheme:   checkInTicketTheme(preview.TicketType),
			AttendeeName:  preview.AttendeeName,
			TShirtSize:    preview.TShirtSize,
			PickupSummary: pickupSummary,
			ConferenceTag: preview.ConferenceTag,
			CheckInState:  checkInState,
		})
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dev/checkin_preview.tmpl", &DevCheckInPreviewPage{
		Rows: rows,
		Year: helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load check-in previews", http.StatusInternalServerError)
		ctx.Err.Printf("/dev/check-in template: %s", err)
	}
}

func DevCheckInPreview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if ctx.Env.Prod {
		handle404(w, r, ctx)
		return
	}
	ticket := strings.TrimSpace(mux.Vars(r)["ticket"])
	previews, err := getters.ListDevRegistrationCheckInPreviews(ctx)
	if err != nil {
		http.Error(w, "Unable to load check-in preview", http.StatusInternalServerError)
		ctx.Err.Printf("/dev/check-in/%s preview lookup: %s", ticket, err)
		return
	}
	allowed := false
	for _, preview := range previews {
		if preview.TicketRef == ticket {
			allowed = true
			break
		}
	}
	if !allowed {
		handle404(w, r, ctx)
		return
	}
	details, err := getters.GetRegistrationCheckIn(ctx, ticket)
	if err != nil || details.ConferenceTag == "" {
		handle404(w, r, ctx)
		return
	}
	pickups, err := getters.ListShopPickupsForTicket(ctx, ticket)
	if err != nil {
		http.Error(w, "Unable to load pickup preview", http.StatusInternalServerError)
		ctx.Err.Printf("/dev/check-in/%s merch pickups: %s", ticket, err)
		return
	}
	page := &CheckInPage{
		TicketType:      details.TicketType,
		TicketRef:       details.TicketRef,
		Msg:             "Development preview — no check-in or pickup changes will be recorded.",
		AttendeeName:    details.AttendeeName,
		AttendeeEmail:   details.Email,
		TShirtSize:      details.TShirtSize,
		ConferenceTag:   details.ConferenceTag,
		ConferenceImage: confImagePath(details.ConferenceTag, "leading"),
		CheckInComplete: details.CheckedInAt != nil && !details.Revoked,
		ShirtPickedUp:   details.ShirtPickedUpAt != nil,
		MerchPickups:    pickups,
		IsPreview:       true,
		Year:            helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "checkin.tmpl", page); err != nil {
		http.Error(w, "Unable to load check-in preview", http.StatusInternalServerError)
		ctx.Err.Printf("/dev/check-in/%s template: %s", ticket, err)
	}
}

func ticketMatch(tickets []string, desc string) bool {
	for _, tix := range tickets {
		if strings.Contains(desc, tix) {
			return true
		}
	}

	return false
}

func computeHash(key, id string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))
}

func validHash(key, id, msgMAC string) bool {
	actual := computeHash(key, id)
	return hmac.Equal([]byte(msgMAC), []byte(actual))
}

var decoder = newFormDecoder()

func OpenNodeCallback(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxWebhookBodyBytes)
	err := r.ParseForm()
	if err != nil {
		ctx.Err.Printf("Error reading request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var ev ChargeEvent
	decoder.IgnoreUnknownKeys(true)
	err = decoder.Decode(&ev, r.PostForm)
	if err != nil {
		ctx.Err.Printf("Unable to unmarshal: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	/* Check the hashed order is ok */
	if !validHash(ctx.Env.OpenNode.Key, ev.ID, ev.HashedOrder) {
		ctx.Err.Printf("Invalid request from opennode %s %s", ev.ID, ev.HashedOrder)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	/* Go get the actual event data */
	charge, err := GetCharge(ctx, ev.ID)
	if err != nil {
		var apiErr *openNodeHTTPError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Body, "checkout does not exist") {
			// OpenNode's webhook simulator signs a synthetic charge ID that
			// cannot be retrieved from the charge API. Acknowledge delivery so
			// the simulator does not retry, but never fulfill a ticket or order.
			ctx.Infos.Printf("Acknowledged OpenNode simulator callback for non-existent charge %s", ev.ID)
			w.WriteHeader(http.StatusOK)
			return
		}
		ctx.Err.Printf("Unable to fetch charge: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if charge.ID != "" && charge.ID != ev.ID {
		ctx.Err.Printf("OpenNode returned charge %s for callback %s", charge.ID, ev.ID)
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	if charge.Metadata == nil {
		ctx.Err.Printf("OpenNode charge %s has no metadata", ev.ID)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// The callback form is only a notification. Trust the charge fetched
	// directly from OpenNode as the authoritative payment state.
	if charge.Status != "paid" {
		ctx.Infos.Printf("User did not complete charge. charge-id: %s callback-status: %s charge-status: %s email: %s conf-ref: %s", ev.ID, ev.Status, charge.Status, charge.Metadata.Email, charge.Metadata.ConfRef)
		w.WriteHeader(http.StatusOK)
		return
	}

	ctx.Infos.Println("opennode charge!", charge)
	if charge.Metadata.ShopOrderID != "" && charge.Metadata.ConfRef == "" {
		if err := markOpenNodeShopOrderPaid(ctx, charge); err != nil {
			ctx.Err.Printf("opennode callback: unable to mark shop order %s paid: %s", charge.Metadata.ShopOrderID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	entry := types.Entry{
		ID:          charge.ID,
		ConfRef:     charge.Metadata.ConfRef,
		Total:       int64(charge.FiatVal * 100),
		Currency:    charge.Metadata.Currency,
		Created:     time.Unix(int64(charge.CreatedAt), 0),
		Email:       charge.Metadata.Email,
		DiscountRef: charge.Metadata.DiscountRef,
	}

	tixType := types.TicketTypeGeneral
	if charge.Metadata.TicketKind != "" {
		tixType = charge.Metadata.TicketKind
	} else if charge.Metadata.TixLocal {
		tixType = types.TicketTypeLocal
	}
	entry.Items, entry.Total, err = openNodeTicketItems(charge, tixType)
	if err != nil {
		ctx.Err.Printf("Invalid OpenNode ticket charge %s: %s", charge.ID, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if len(entry.Items) == 0 {
		ctx.Infos.Println("No valid items bought")
		w.WriteHeader(http.StatusOK)
		return
	}

	existingTickets, err := getters.ListRegistrationsByCheckoutID(ctx, entry.ID)
	if err != nil {
		ctx.Err.Printf("Unable to check existing OpenNode tickets %s: %v", entry.ID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	firstFulfillment := len(existingTickets) == 0
	err = getters.AddTickets(ctx, &entry, "opennode")

	if err != nil {
		ctx.Err.Printf("!!! Unable to add ticket %s: %v", err, entry)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if charge.Metadata.ShopOrderID != "" {
		transitioned, err := getters.MarkShopOrderPaid(ctx, charge.Metadata.ShopOrderID, "opennode", charge.ID, 0, fiatValueCents(charge.FiatVal))
		if err != nil {
			ctx.Err.Printf("opennode callback: unable to mark shop order %s paid: %s", charge.Metadata.ShopOrderID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := finalizeShopTaxTransaction(ctx, charge.Metadata.ShopOrderID); err != nil {
			ctx.Err.Printf("opennode callback finalize tax %s: %s", charge.Metadata.ShopOrderID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if transitioned {
			order, err := getters.GetShopOrderByID(ctx, charge.Metadata.ShopOrderID)
			if err != nil {
				ctx.Err.Printf("opennode callback load receipt order %s: %s", charge.Metadata.ShopOrderID, err)
			} else if err := sendShopReceiptEmail(ctx, order); err != nil {
				ctx.Err.Printf("opennode callback receipt %s: %s", charge.Metadata.ShopOrderID, err)
			}
		}
	}
	if !firstFulfillment {
		ctx.Infos.Printf("OpenNode callback replay already fulfilled charge %s", entry.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	/* Add to mailing list + schedule mails */
	conf, err := getters.GetConfByRef(ctx, entry.ConfRef)
	if err != nil {
		ctx.Err.Printf("opennode callback: unable to load conf! %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if conf == nil {
		ctx.Err.Printf("opennode callback: unable to find conf %s", entry.ConfRef)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !types.IsSponsoredTicketType(tixType) {
		err = missives.NewTicketSub(ctx, entry.Email, conf.Tag, tixType, charge.Metadata.Subscribe)
		if err != nil {
			ctx.Err.Printf("!!! Unable to subscribe to newsletter %s: %v", err, entry)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	// Increment discount usage counter
	if entry.DiscountRef != "" {
		err = getters.IncrementDiscountUses(ctx, entry.DiscountRef, uint(len(entry.Items)))
		if err != nil {
			ctx.Err.Printf("Failed to increment discount uses: %s", err)
		}
		preDiscountStr := ""
		if charge.Metadata.PreDiscountCents > 0 {
			preDiscountStr = strconv.FormatInt(charge.Metadata.PreDiscountCents, 10)
		}
		recordAffiliateUsageFromCheckout(ctx, conf, &entry, preDiscountStr)
	}

	ctx.Infos.Println("Added ticket!", entry.ID)
	w.WriteHeader(http.StatusOK)
}

func openNodeTicketItems(charge *Charge, ticketType string) ([]types.Item, int64, error) {
	if charge == nil || charge.Metadata == nil {
		return nil, 0, fmt.Errorf("charge metadata is required")
	}
	quantity := charge.Metadata.Quantity
	if quantity <= 0 || quantity > 100 || math.Trunc(quantity) != quantity {
		return nil, 0, fmt.Errorf("invalid ticket quantity %v", quantity)
	}
	ticketTotal := charge.Metadata.TicketTotalCents
	if ticketTotal == 0 {
		ticketTotal = int64(fiatValueCents(charge.FiatVal)) - charge.Metadata.AddOnCents
	}
	if ticketTotal < 0 {
		return nil, 0, fmt.Errorf("add-on amount exceeds charge total")
	}
	count := int64(quantity)
	items := make([]types.Item, 0, count)
	for i := int64(0); i < count; i++ {
		items = append(items, types.Item{
			Total: stripePerTicketAmount(ticketTotal, count, i),
			Desc:  charge.Description,
			Type:  ticketType,
		})
	}
	return items, ticketTotal, nil
}

func markOpenNodeShopOrderPaid(ctx *config.AppContext, charge *Charge) error {
	if charge == nil || charge.Metadata == nil || charge.Metadata.ShopOrderID == "" {
		return fmt.Errorf("missing shop order metadata")
	}
	transitioned, err := getters.MarkShopOrderPaid(ctx, charge.Metadata.ShopOrderID, "opennode", charge.ID, 0, fiatValueCents(charge.FiatVal))
	if err != nil {
		return err
	}
	if !transitioned {
		return finalizeShopTaxTransaction(ctx, charge.Metadata.ShopOrderID)
	}
	if err := finalizeShopTaxTransaction(ctx, charge.Metadata.ShopOrderID); err != nil {
		return err
	}
	order, err := getters.GetShopOrderByID(ctx, charge.Metadata.ShopOrderID)
	if err != nil {
		return err
	}
	return sendShopReceiptEmail(ctx, order)
}

func fiatValueCents(v float64) uint {
	if v <= 0 {
		return 0
	}
	return uint(v*100 + 0.5)
}

func getPrice(pricestr string) (uint, error) {
	price, err := strconv.ParseUint(pricestr, 10, 32)
	return uint(price), err
}

func checkoutDefaultPaymentMethod(r *http.Request) string {
	if r == nil {
		return "btc"
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("payment"))) {
	case "card", "fiat", "stripe":
		return "card"
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("pay"))) {
	case "card", "fiat", "stripe":
		return "card"
	}
	return "btc"
}

func validateCheckoutDiscountPrice(ctx *config.AppContext, conf *types.Conf, tixPrice uint, effectiveCode string, submittedDiscountPrice uint) (string, uint, error) {
	if strings.TrimSpace(effectiveCode) == "" {
		if submittedDiscountPrice != tixPrice {
			return "", tixPrice, fmt.Errorf("checkout price changed; please refresh and try again")
		}
		return "", tixPrice, nil
	}
	currentDiscountPrice, discount, err := getters.CalcDiscount(ctx, conf.Ref, effectiveCode, tixPrice, 1)
	if err != nil {
		return "", tixPrice, err
	}
	if currentDiscountPrice != submittedDiscountPrice {
		return "", currentDiscountPrice, fmt.Errorf("checkout price changed; please refresh and try again")
	}
	if discount == nil {
		return "", currentDiscountPrice, nil
	}
	return discount.Ref, currentDiscountPrice, nil
}

func HandleDiscount(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	params := mux.Vars(r)
	tixSlug := params["tix"]

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	discountCode := r.Form.Get("Discount")
	affiliateCode := r.Form.Get("AffiliateCode")
	count, err := getPrice(r.Form.Get("Count"))
	if err != nil || count < 1 {
		count = 1
	}
	discountPrice, err := getPrice(r.Form.Get("DiscountPrice"))
	if err != nil {
		ctx.Err.Printf("/tix/%s/apply-discount massively blew up: %s", tixSlug, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if tixSlug == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	conf, tix, tixPrice, ticketKind, err := determineTixPrice(ctx, tixSlug)
	if err != nil {
		/* FIXME: have this return an error message, not a status code error */
		ctx.Err.Printf("/tix/%s/apply-discount unable to determine tix price: %s", tixSlug, err)
		http.NotFound(w, r)
		return
	}

	// Effective code: typed wins over silent affiliate. The HMAC
	// + recorded discount-ref both follow the effective code.
	effectiveCode := effectiveDiscountCode(discountCode, affiliateCode)
	var discountRef string
	errStr := ""
	if effectiveCode != "" {
		var discount *types.DiscountCode
		discountPrice, discount, err = getters.CalcDiscount(ctx, conf.Ref, effectiveCode, tixPrice, 1)
		if discount != nil {
			discountRef = discount.Ref
		}
		if err != nil {
			ctx.Err.Printf("/tix/%s/apply-discount discount not available: %s", tixSlug, err)
			// Silent affiliate codes that fail validation
			// stay invisible — drop them and proceed at full
			// price rather than surfacing an error the buyer
			// didn't trigger.
			if effectiveCode == affiliateCode && discountCode == "" {
				affiliateCode = ""
				discountPrice = tixPrice
			} else {
				errStr = err.Error()
			}
		}
	} else {
		discountPrice = tixPrice
	}

	w.Header().Set("Content-Type", "text/html")
	err = ctx.TemplateCache.ExecuteTemplate(w, "tix_details.tmpl", &TixFormPage{
		Conf:            conf,
		Tix:             tix,
		TixSlug:         tixSlug,
		TixPrice:        tixPrice,
		Discount:        discountCode,
		AffiliateCode:   affiliateCode,
		DiscountPrice:   discountPrice,
		CardPrice:       cardSurchargePrice(discountPrice, tix.CardSurchargeBPS),
		CardSurcharge:   cardSurchargePrice(discountPrice, tix.CardSurchargeBPS) - discountPrice,
		DiscountRef:     discountRef,
		TicketKind:      ticketKind,
		SponsorCheckout: ticketKind == types.TicketTypeSponsored,
		Err:             errStr,
		HMAC:            calcTixHMAC(ctx, conf, tixPrice, discountPrice, effectiveCode),
		Count:           count,
		Year:            helpers.CurrentYear(),
	})

	if err != nil {
		http.Error(w, "Unable to load template, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/tix/%s/apply-discount templ exec failed %s", tixSlug, err.Error())
		return
	}
}

func HandleCheckout(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	params := mux.Vars(r)
	tixSlug := params["tix"]

	if tixSlug == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	conf, tix, tixPrice, ticketKind, err := determineTixPrice(ctx, tixSlug)
	if err != nil {
		ctx.Err.Printf("/tix/%s/checkout unable to determine tix price: %s", tixSlug, err)
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:

		// `?q=` on the checkout URL takes precedence (admin debug
		// flow); otherwise fall back to two parallel session
		// slots that get populated when a visitor lands via a
		// shared link:
		//
		//   - disc:{tag}  → buyer-facing discount code, pre-fills
		//     the visible Discount input + drives the price.
		//   - aff:{tag}   → silent (`%0`) affiliate referral; the
		//     visible Discount stays empty and no "saved $X" line
		//     shows, but a hidden AffiliateCode field carries the
		//     code through the form so checkout still credits the
		//     affiliate.
		//
		// The visible code wins at submit time — see HandleDiscount
		// + the POST branch's effective-code resolution.
		discountCode, _ := helpers.GetSessionKey("q", r)
		if discountCode == "" {
			discountCode = ctx.Session.GetString(r.Context(), discountSessionKey(conf.Tag))
		}
		affiliateCode := ctx.Session.GetString(r.Context(), affiliateSessionKey(conf.Tag))

		discountPrice := tixPrice
		var errStr string
		var discountRef string
		// The "effective" code for this render is the visible one
		// when present, else the silent affiliate one. CalcDiscount
		// runs against either — both produce a valid, finalized
		// price (a `%0` affiliate just leaves the price unchanged).
		effective := discountCode
		if effective == "" {
			effective = affiliateCode
		}
		if effective != "" {
			var discount *types.DiscountCode
			discountPrice, discount, err = getters.CalcDiscount(ctx, conf.Ref, effective, tixPrice, 1)
			if err != nil {
				ctx.Err.Printf("/tix/%s/checkout discount not available: %s", tixSlug, err)
				// Silent affiliate codes that fail validation
				// shouldn't show a buyer-facing error — drop
				// the affiliate ref and proceed at full price.
				if effective == affiliateCode && discountCode == "" {
					affiliateCode = ""
					discountPrice = tixPrice
				} else {
					errStr = err.Error()
				}
			}
			if discount != nil {
				discountRef = discount.Ref
			}
		}
		err = ctx.TemplateCache.ExecuteTemplate(w, "collect-email.tmpl", &TixFormPage{
			Conf:            conf,
			Tix:             tix,
			TixSlug:         tixSlug,
			TixPrice:        tixPrice,
			Discount:        discountCode,
			AffiliateCode:   affiliateCode,
			DiscountPrice:   discountPrice,
			CardPrice:       cardSurchargePrice(discountPrice, tix.CardSurchargeBPS),
			CardSurcharge:   cardSurchargePrice(discountPrice, tix.CardSurchargeBPS) - discountPrice,
			DiscountRef:     discountRef,
			TicketKind:      ticketKind,
			SponsorCheckout: ticketKind == types.TicketTypeSponsored,
			Err:             errStr,
			HMAC:            calcTixHMAC(ctx, conf, tixPrice, discountPrice, effective),
			Count:           uint(1),
			Year:            helpers.CurrentYear(),
			PaymentMethod:   checkoutDefaultPaymentMethod(r),
			AddOnProducts:   ticketCheckoutAddOnProducts(ctx, conf),
		})
		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/tix/%s/checkout templ exec failed %s", tixSlug, err.Error())
			return
		}
		return
	case http.MethodPost:
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		dec := newFormDecoder()
		var form types.TixForm
		err = dec.Decode(&form, r.PostForm)
		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/tix/%s/checkout unable to decode form %s", tixSlug, err)
			return
		}

		if form.Email == "" || form.Count < 1 {
			http.Redirect(w, r, fmt.Sprintf("/tix/%s/checkout", tixSlug), http.StatusSeeOther)
			return
		}

		// Resolve the effective discount code: the typed one wins
		// over the silent affiliate carry-through. Anyone typing a
		// different code at checkout drops the prior affiliate's
		// credit because the discount-ref written to Stripe
		// metadata follows whichever code is actually applied.
		effectiveCode := effectiveDiscountCode(form.Discount, form.AffiliateCode)

		/*  Validate HMAC over the effective code (matches the
		 *  HMAC computed on render, which signed over the same
		 *  effective code — typed vs. silent — that resolves on
		 *  this submit). */
		expectedHMAC := calcTixHMAC(ctx, conf, tixPrice, form.DiscountPrice, effectiveCode)
		if !hmac.Equal([]byte(expectedHMAC), []byte(form.HMAC)) {
			ctx.Err.Printf("/tix/%s/checkout hmac mismatch. %s != %s", tixSlug, expectedHMAC, form.HMAC)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		discountRef, currentDiscountPrice, err := validateCheckoutDiscountPrice(ctx, conf, tixPrice, effectiveCode, form.DiscountPrice)
		if err != nil {
			ctx.Err.Printf("/tix/%s/checkout discount revalidation failed: %s", tixSlug, err)
			err = ctx.TemplateCache.ExecuteTemplate(w, "collect-email.tmpl", &TixFormPage{
				Conf:            conf,
				Tix:             tix,
				TixSlug:         tixSlug,
				TixPrice:        tixPrice,
				Discount:        form.Discount,
				AffiliateCode:   form.AffiliateCode,
				DiscountPrice:   currentDiscountPrice,
				CardPrice:       cardSurchargePrice(currentDiscountPrice, tix.CardSurchargeBPS),
				CardSurcharge:   cardSurchargePrice(currentDiscountPrice, tix.CardSurchargeBPS) - currentDiscountPrice,
				DiscountRef:     "",
				TicketKind:      ticketKind,
				SponsorCheckout: ticketKind == types.TicketTypeSponsored,
				Err:             err.Error(),
				HMAC:            calcTixHMAC(ctx, conf, tixPrice, currentDiscountPrice, effectiveCode),
				Count:           form.Count,
				Year:            helpers.CurrentYear(),
				PaymentMethod:   form.PaymentMethod,
				AddOnProducts:   ticketCheckoutAddOnProducts(ctx, conf),
			})
			if err != nil {
				http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
				ctx.Err.Printf("/tix/%s/checkout stale discount templ exec failed %s", tixSlug, err.Error())
			}
			return
		}
		form.DiscountRef = discountRef
		// Keep form.Discount in sync with the effective code so
		// downstream Stripe metadata + entry.DiscountRef agree.
		form.Discount = effectiveCode

		ctx.Session.Put(r.Context(), checkoutEmailSessionKey(conf.Tag), strings.ToLower(strings.TrimSpace(form.Email)))

		addOns, addOnTotalCents := selectedTicketAddOns(ctx, conf, r)
		var shopOrderID string
		var addOnTaxCents uint
		if len(addOns) > 0 {
			taxQuote, taxErr := ticketCheckoutTaxQuote(ctx, conf, addOns)
			if taxErr != nil {
				ctx.Err.Printf("/tix/%s/checkout calculate add-on tax failed: %s", tixSlug, taxErr)
				http.Error(w, "Unable to calculate sales tax for event pickup", http.StatusUnprocessableEntity)
				return
			}
			addOnTaxCents = taxQuote.SalesTaxAmountCents
			order, err := createTicketAddOnOrder(ctx, conf, tix, &form, ticketKind, form.PaymentMethod, addOns, addOnTotalCents, addOnTaxCents)
			if err != nil {
				ctx.Err.Printf("/tix/%s/checkout create mixed add-on order failed: %s", tixSlug, err)
				http.Error(w, "Unable to create add-on order", http.StatusInternalServerError)
				return
			}
			if order != nil {
				shopOrderID = order.ID
				taxQuote.OrderID = order.ID
				if err := getters.CreateTaxQuote(ctx, *taxQuote); err != nil {
					ctx.Err.Printf("/tix/%s/checkout persist add-on tax failed: %s", tixSlug, err)
					_ = getters.CancelShopOrder(ctx, order.ID, "", "ticket add-on tax quote could not be saved")
					http.Error(w, "Unable to save sales tax calculation", http.StatusInternalServerError)
					return
				}
			}
		}

		if form.PaymentMethod == "card" {
			cardPrice := cardSurchargePrice(form.DiscountPrice, tix.CardSurchargeBPS)
			StripeInitWithDiscount(w, r, ctx, conf, tix, cardPrice, tixPrice, form.DiscountPrice, &form, ticketKind, addOns, addOnTaxCents, shopOrderID)
		} else {
			OpenNodeInit(w, r, ctx, conf, tix, form.DiscountPrice, tixPrice, &form, ticketKind, addOnTotalCents+addOnTaxCents, shopOrderID)
		}
		return
	default:
		http.NotFound(w, r)
		return
	}
}

func OpenNodeInit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, tix *types.ConfTicket, tixPrice, preDiscountPrice uint, tixForm *types.TixForm, ticketKind string, addOnTotalCents uint, shopOrderID string) {
	payment, err := getters.InitOpenNodeCheckout(ctx, tixPrice, preDiscountPrice, tix, conf, ticketKind, tixForm.Count, tixForm.Email, tixForm.DiscountRef, tixForm.Subscribe, addOnTotalCents, shopOrderID)

	if err != nil {
		if shopOrderID != "" {
			if cancelErr := getters.CancelShopOrder(ctx, shopOrderID, "", "OpenNode checkout could not start"); cancelErr != nil {
				ctx.Err.Printf("release mixed OpenNode order %s: %s", shopOrderID, cancelErr)
			}
		}
		http.Error(w, "unable to init btc payment", http.StatusInternalServerError)
		ctx.Err.Printf("opennode payment init failed: %s", err.Error())
		return
	}

	/* FIXME: v2: implement on-site btc checkout */
	/* for now we go ahead and just redirect to opennode, see you latrrr */
	http.Redirect(w, r, payment.HostedCheckoutURL, http.StatusSeeOther)
}

func cardSurchargePrice(basePrice, surchargeBPS uint) uint {
	if basePrice == 0 {
		return 0
	}
	if surchargeBPS == 0 {
		surchargeBPS = 1000
	}
	return uint((uint64(basePrice)*uint64(10000+surchargeBPS) + 9999) / 10000)
}

func stripePerTicketAmount(lineTotal int64, quantity int64, index int64) int64 {
	if quantity <= 0 {
		return lineTotal
	}
	amount := lineTotal / quantity
	remainder := lineTotal % quantity
	if remainder > 0 && index < remainder {
		amount++
	}
	return amount
}

func ticketStripeTaxCode(tix *types.ConfTicket) string {
	if tix == nil || strings.TrimSpace(tix.StripeTaxCode) == "" {
		return types.StripeTaxCodeNontaxable
	}
	return strings.TrimSpace(tix.StripeTaxCode)
}

func StripeInitWithDiscount(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, tix *types.ConfTicket, tixPrice, preDiscountPrice, discountedBasePrice uint, form *types.TixForm, ticketKind string, addOns []*shopCartItem, salesTaxCents uint, shopOrderID string) {
	if ticketKind == "" {
		ticketKind = types.TicketTypeGeneral
	}
	domain := ctx.Env.GetURI()
	priceAsCents := int64(tixPrice * 100)
	confDesc := fmt.Sprintf("%d ticket(s) for %s", form.Count, conf.Desc)
	metadata := make(map[string]string)
	metadata["conf-tag"] = conf.Tag
	metadata["conf-ref"] = conf.Ref
	metadata["tix-id"] = tix.ID
	metadata["discount-ref"] = form.DiscountRef
	metadata["subscribe"] = fmt.Sprintf("%t", form.Subscribe)
	// Pre-discount per-ticket price in cents — webhook reads this
	// to compute originalCents (× ticket count) for the affiliate
	// math. Sourced from the original tier price (USD / BTC / Local)
	// the buyer selected, NOT the `tixPrice` arg, because callers pass
	// tixPrice = form.DiscountPrice (the *post*-discount value).
	metadata["pre-discount-cents"] = strconv.FormatInt(int64(preDiscountPrice)*100, 10)
	metadata["discounted-base-cents"] = strconv.FormatInt(int64(discountedBasePrice)*100, 10)
	metadata["card-surcharge-cents"] = strconv.FormatInt((int64(tixPrice)-int64(discountedBasePrice))*100, 10)
	metadata["sales-tax-cents"] = strconv.FormatUint(uint64(salesTaxCents), 10)
	metadata["payment-method"] = "card"
	metadata["ticket-kind"] = ticketKind
	if shopOrderID != "" {
		metadata["checkout-kind"] = types.ShopCheckoutKindMixed
		metadata["shop-order-id"] = shopOrderID
	}
	if ticketKind == types.TicketTypeLocal {
		metadata["tix-local"] = "yes"
	}
	ticketProductMetadata := make(map[string]string, len(metadata)+1)
	for key, value := range metadata {
		ticketProductMetadata[key] = value
	}
	ticketProductMetadata["line-kind"] = "ticket"
	lineItems := []*stripe.CheckoutSessionLineItemParams{
		{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Description: stripe.String(confDesc),
					Name:        stripe.String(conf.Desc),
					Metadata:    ticketProductMetadata,
					TaxCode:     stripe.String(ticketStripeTaxCode(tix)),
				},
				TaxBehavior: stripe.String("exclusive"),
				UnitAmount:  stripe.Int64(priceAsCents),
				Currency:    stripe.String(tix.Currency),
			},
			Quantity: stripe.Int64(int64(form.Count)),
		},
	}
	for _, item := range addOns {
		if item == nil || item.Product == nil || item.Variant == nil {
			continue
		}
		productData := &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
			Name:        stripe.String(item.Product.Name),
			Description: stripe.String("MERCH:" + item.Variant.SKU),
			Metadata: map[string]string{
				"line-kind":     "merch",
				"shop-order-id": shopOrderID,
				"variant-id":    item.Variant.ID,
			},
			TaxCode: stripe.String(firstNonEmpty(item.Product.StripeTaxCode, types.StripeTaxCodeTangibleGood)),
		}
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				ProductData: productData,
				TaxBehavior: stripe.String("exclusive"),
				UnitAmount:  stripe.Int64(int64(item.UnitPriceCents)),
				Currency:    stripe.String(strings.ToLower(tix.Currency)),
			},
			Quantity: stripe.Int64(int64(item.Qty)),
		})
	}
	if salesTaxCents > 0 {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name:        stripe.String("Sales tax"),
					Description: stripe.String("Calculated for event pickup"),
					Metadata:    map[string]string{"line-kind": "tax", "shop-order-id": shopOrderID},
				},
				UnitAmount: stripe.Int64(int64(salesTaxCents)),
				Currency:   stripe.String(strings.ToLower(tix.Currency)),
			},
			Quantity: stripe.Int64(1),
		})
	}
	params := &stripe.CheckoutSessionParams{
		CustomerEmail: stripe.String(form.Email),
		LineItems:     lineItems,
		Metadata:      metadata,
		Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:    stripe.String(domain + "/" + conf.Tag + "/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:     stripe.String(domain + "/" + conf.Tag),
		AutomaticTax:  &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(false)},
		ExpiresAt:     stripe.Int64(time.Now().Add(types.ShopCheckoutSessionTTL).Unix()),
	}

	s, err := session.New(params)
	if err != nil {
		if shopOrderID != "" {
			if cancelErr := getters.CancelShopOrder(ctx, shopOrderID, "", "Stripe checkout could not start"); cancelErr != nil {
				ctx.Err.Printf("release mixed Stripe order %s: %s", shopOrderID, cancelErr)
			}
		}
		ctx.Err.Printf("!!! Unable to create stripe session: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}

func StripeInit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, tix *types.ConfTicket, tixPrice uint) {

	domain := ctx.Env.GetURI()
	priceAsCents := int64(tixPrice * 100)
	confDesc := fmt.Sprintf("1 ticket for the %s", conf.Desc)
	metadata := make(map[string]string)
	metadata["conf-tag"] = conf.Tag
	metadata["conf-ref"] = conf.Ref
	metadata["tix-id"] = tix.ID
	metadata["pre-discount-cents"] = strconv.FormatInt(priceAsCents, 10)
	if tixPrice == tix.Local {
		metadata["tix-local"] = "yes"
	}
	params := &stripe.CheckoutSessionParams{
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Description: stripe.String(confDesc),
						Name:        stripe.String(conf.Desc),
						Metadata:    metadata,
						TaxCode:     stripe.String(ticketStripeTaxCode(tix)),
					},
					TaxBehavior: stripe.String("exclusive"),
					UnitAmount:  stripe.Int64(priceAsCents),
					Currency:    stripe.String(tix.Currency),
				},
				Quantity: stripe.Int64(1),
			}},
		Metadata:            metadata,
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:          stripe.String(domain + "/" + conf.Tag + "/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:           stripe.String(domain + "/" + conf.Tag),
		AutomaticTax:        &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(false)},
		AllowPromotionCodes: stripe.Bool(true),
	}

	s, err := session.New(params)
	if err != nil {
		ctx.Err.Printf("!!! Unable to create stripe session: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}

func stripeCheckoutShopAmounts(checkout *stripe.CheckoutSession) (uint, uint) {
	if checkout == nil {
		return 0, 0
	}
	tax := uint(0)
	if checkout.TotalDetails != nil {
		tax = stripeAmountToUint(checkout.TotalDetails.AmountTax)
	}
	return tax, stripeAmountToUint(checkout.AmountTotal)
}

func stripeCheckoutShippingAddress(checkout *stripe.CheckoutSession) *types.ShopAddress {
	if checkout == nil || checkout.ShippingDetails == nil || checkout.ShippingDetails.Address == nil {
		return nil
	}
	address := checkout.ShippingDetails.Address
	return &types.ShopAddress{
		Name:       checkout.ShippingDetails.Name,
		Line1:      address.Line1,
		Line2:      address.Line2,
		City:       address.City,
		Region:     address.State,
		PostalCode: address.PostalCode,
		Country:    address.Country,
		Phone:      checkout.ShippingDetails.Phone,
	}
}

func stripeAmountToUint(amount int64) uint {
	if amount <= 0 {
		return 0
	}
	return uint(amount)
}

func stripeCheckoutReadyForFulfillment(checkout *stripe.CheckoutSession) bool {
	if checkout == nil {
		return false
	}
	return checkout.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid ||
		checkout.PaymentStatus == stripe.CheckoutSessionPaymentStatusNoPaymentRequired
}

func stripeCheckoutShouldFulfill(eventType stripe.EventType, checkout *stripe.CheckoutSession) bool {
	if checkout == nil {
		return false
	}
	switch eventType {
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		return true
	case stripe.EventTypeCheckoutSessionCompleted:
		return stripeCheckoutReadyForFulfillment(checkout)
	default:
		return false
	}
}

func stripeCheckoutEmail(checkout *stripe.CheckoutSession) string {
	if checkout == nil {
		return ""
	}
	if checkout.CustomerDetails != nil {
		if email := strings.TrimSpace(checkout.CustomerDetails.Email); email != "" {
			return email
		}
	}
	return strings.TrimSpace(checkout.CustomerEmail)
}

func parseStripeCheckout(event stripe.Event) (*stripe.CheckoutSession, error) {
	var checkout stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &checkout); err != nil {
		return nil, fmt.Errorf("parse Stripe checkout session: %w", err)
	}
	return &checkout, nil
}

func cancelStripeCheckout(ctx *config.AppContext, checkout *stripe.CheckoutSession, reason string) error {
	if checkout == nil {
		return nil
	}
	orderID := strings.TrimSpace(checkout.Metadata["shop-order-id"])
	if orderID == "" {
		return nil
	}
	order, err := getters.GetShopOrderByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("load shop order %s: %w", orderID, err)
	}
	if order.Status != types.ShopOrderStatusPending {
		return nil
	}
	if err := getters.CancelShopOrder(ctx, orderID, "", reason); err != nil {
		return fmt.Errorf("cancel shop order %s: %w", orderID, err)
	}
	return nil
}

func fulfillStripeShopOrder(ctx *config.AppContext, checkout *stripe.CheckoutSession) (bool, error) {
	if checkout == nil {
		return false, fmt.Errorf("Stripe checkout is required")
	}
	orderID := strings.TrimSpace(checkout.Metadata["shop-order-id"])
	if orderID == "" {
		return false, nil
	}
	salesTaxAmount, total := stripeCheckoutShopAmounts(checkout)
	if address := stripeCheckoutShippingAddress(checkout); address != nil {
		if err := getters.UpsertShopOrderShippingAddress(ctx, orderID, address); err != nil {
			return false, fmt.Errorf("save shop order %s shipping address: %w", orderID, err)
		}
	}
	transitioned, err := getters.MarkShopOrderPaid(ctx, orderID, "stripe", checkout.ID, salesTaxAmount, total)
	if err != nil {
		return false, fmt.Errorf("mark shop order %s paid: %w", orderID, err)
	}
	if err := finalizeShopTaxTransaction(ctx, orderID); err != nil {
		return false, fmt.Errorf("finalize shop order %s tax: %w", orderID, err)
	}
	if transitioned {
		order, err := getters.GetShopOrderByID(ctx, orderID)
		if err != nil {
			ctx.Err.Printf("Stripe checkout load receipt order %s: %s", orderID, err)
		} else if err := sendShopReceiptEmail(ctx, order); err != nil {
			ctx.Err.Printf("Stripe checkout receipt %s: %s", orderID, err)
		}
	}
	return transitioned, nil
}

func stripeCheckoutLineItems(checkoutID string) ([]*stripe.LineItem, error) {
	itemParams := &stripe.CheckoutSessionListLineItemsParams{
		Session: stripe.String(checkoutID),
	}
	itemParams.AddExpand("data.price.product")

	items := session.ListLineItems(itemParams)
	var lineItems []*stripe.LineItem
	for items.Next() {
		lineItems = append(lineItems, items.LineItem())
	}
	if err := items.Err(); err != nil {
		return nil, fmt.Errorf("list Stripe checkout line items: %w", err)
	}
	return lineItems, nil
}

func stripeCheckoutTicketType(checkout *stripe.CheckoutSession) string {
	ticketType := checkout.Metadata["ticket-kind"]
	if ticketType != "" {
		return ticketType
	}
	if _, isLocal := checkout.Metadata["tix-local"]; isLocal {
		return types.TicketTypeLocal
	}
	return types.TicketTypeGeneral
}

func fulfillStripeCheckout(ctx *config.AppContext, checkout *stripe.CheckoutSession) error {
	if checkout == nil {
		return fmt.Errorf("Stripe checkout is required")
	}

	checkoutKind := checkout.Metadata["checkout-kind"]
	shopOrderID := strings.TrimSpace(checkout.Metadata["shop-order-id"])
	if checkoutKind == types.ShopCheckoutKindMerch {
		if shopOrderID == "" {
			return fmt.Errorf("Stripe merch checkout missing shop-order-id")
		}
		if _, err := fulfillStripeShopOrder(ctx, checkout); err != nil {
			return err
		}
		ctx.Infos.Printf("Marked merch order %s paid via Stripe", shopOrderID)
		return nil
	}

	confRef := strings.TrimSpace(checkout.Metadata["conf-ref"])
	if confRef == "" {
		return fmt.Errorf("Stripe checkout missing conf-ref")
	}
	conf, err := getters.GetConfByRef(ctx, confRef)
	if err != nil {
		return fmt.Errorf("load conference %s: %w", confRef, err)
	}
	if conf == nil {
		return fmt.Errorf("conference %s not found", confRef)
	}

	if shopOrderID != "" {
		if _, err := fulfillStripeShopOrder(ctx, checkout); err != nil {
			return err
		}
	}

	lineItems, err := stripeCheckoutLineItems(checkout.ID)
	if err != nil {
		return err
	}
	ticketType := stripeCheckoutTicketType(checkout)
	ticketItems, _ := stripeTicketItems(lineItems, ticketType)
	if len(ticketItems) == 0 {
		ctx.Infos.Printf("Stripe checkout %s contained no ticket items", checkout.ID)
		return nil
	}

	entry := types.Entry{
		ID:          checkout.ID,
		ConfRef:     conf.Ref,
		Currency:    string(checkout.Currency),
		Created:     time.Unix(checkout.Created, 0).UTC(),
		Email:       stripeCheckoutEmail(checkout),
		Items:       ticketItems,
		DiscountRef: strings.TrimSpace(checkout.Metadata["discount-ref"]),
	}
	insertedItems, err := getters.AddPaymentTickets(ctx, &entry, "stripe")
	if err != nil {
		return fmt.Errorf("add Stripe tickets: %w", err)
	}
	if len(insertedItems) == 0 {
		ctx.Infos.Printf("Stripe checkout %s was already fulfilled", checkout.ID)
		return nil
	}

	entry.Items = insertedItems
	for _, item := range insertedItems {
		entry.Total += item.Total
	}
	ctx.Infos.Printf("Added %d Stripe tickets", len(insertedItems))

	if entry.DiscountRef != "" {
		affiliateEntry := entry
		if discountedBaseCents, err := strconv.ParseInt(strings.TrimSpace(checkout.Metadata["discounted-base-cents"]), 10, 64); err == nil && discountedBaseCents > 0 {
			affiliateEntry.Total = discountedBaseCents * int64(len(insertedItems))
		}
		recordAffiliateUsageFromCheckout(ctx, conf, &affiliateEntry, checkout.Metadata["pre-discount-cents"])
	}

	if !types.IsSponsoredTicketType(ticketType) {
		if err := missives.NewTicketSub(ctx, entry.Email, conf.Tag, ticketType, false); err != nil {
			ctx.Err.Printf("Unable to subscribe Stripe ticket buyer %s: %v", entry.Email, err)
		}
	}
	return nil
}

func StripeCallback(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		ctx.Err.Printf("Error reading request body: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	event, err := webhook.ConstructEvent(payload, r.Header.Get("Stripe-Signature"), ctx.Env.StripeEndpointSec)

	if err != nil {
		ctx.Err.Println("Error verifying webhook sig", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted, stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		checkout, err := parseStripeCheckout(event)
		if err != nil {
			ctx.Err.Printf("Stripe webhook: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !stripeCheckoutShouldFulfill(event.Type, checkout) {
			ctx.Infos.Printf("Stripe checkout %s is awaiting payment (%s)", checkout.ID, checkout.PaymentStatus)
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := fulfillStripeCheckout(ctx, checkout); err != nil {
			ctx.Err.Printf("Stripe checkout %s fulfillment failed: %v", checkout.ID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case stripe.EventTypeCheckoutSessionExpired, stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		checkout, err := parseStripeCheckout(event)
		if err != nil {
			ctx.Err.Printf("Stripe webhook: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reason := "Stripe checkout session expired"
		if event.Type == stripe.EventTypeCheckoutSessionAsyncPaymentFailed {
			reason = "Stripe asynchronous payment failed"
		}
		if err := cancelStripeCheckout(ctx, checkout, reason); err != nil {
			ctx.Err.Printf("Stripe checkout %s cancellation failed: %v", checkout.ID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		ctx.Infos.Printf("Unhandled event type: %s", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

func stripeTicketItems(lineItems []*stripe.LineItem, ticketType string) ([]types.Item, int64) {
	var ticketItems []types.Item
	var ticketTotal int64
	for _, line := range lineItems {
		kind := stripeLineItemKind(line)
		if line == nil || (kind != "" && kind != "ticket") {
			continue
		}
		ticketTotal += line.AmountTotal
		for i := int64(0); i < line.Quantity; i++ {
			ticketItems = append(ticketItems, types.Item{
				Total: stripePerTicketAmount(line.AmountTotal, line.Quantity, i),
				Desc:  line.Description,
				Type:  ticketType,
			})
		}
	}
	return ticketItems, ticketTotal
}

func stripeLineItemKind(line *stripe.LineItem) string {
	if line != nil && line.Price != nil && line.Price.Product != nil {
		if kind := strings.ToLower(strings.TrimSpace(line.Price.Product.Metadata["line-kind"])); kind != "" {
			return kind
		}
		if strings.HasPrefix(line.Price.Product.Description, "MERCH:") {
			return "merch"
		}
	}
	if line != nil && strings.HasPrefix(line.Description, "MERCH:") {
		return "merch"
	}
	// Older ticket Checkout Sessions predate line-kind metadata. Unknown
	// lines remain tickets for backward compatibility; current merch lines
	// are identified by their expanded Product metadata above.
	return "ticket"
}

type EmailForm struct {
	Email string
}
