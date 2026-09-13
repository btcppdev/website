package handlers

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
)

type posReceiptItem struct {
	Name, Label string
	Quantity    int
	Unit, Total string
}
type posReceipt struct {
	AccountView         bool
	Sale                *types.POSSale
	Event, Total, Local string
	Items               []posReceiptItem
}

func posReceiptData(conf *types.Conf, sale *types.POSSale) posReceipt {
	data := posReceipt{Sale: sale, Event: conf.Location, Total: fmt.Sprintf("%d sats", sale.TotalSats), Local: fmt.Sprintf("≈ %.2f %s", float64(sale.TotalSats)*sale.LocalPerBTC/1e8, sale.Currency)}
	for _, item := range sale.Items {
		data.Items = append(data.Items, posReceiptItem{Name: item.Name, Label: item.Label, Quantity: item.Quantity, Unit: fmt.Sprintf("%d sats", item.PriceSats), Total: fmt.Sprintf("%d sats", item.PriceSats*int64(item.Quantity))})
	}
	return data
}

func composePOSReceipt(app *config.AppContext, conf *types.Conf, sale *types.POSSale, recipient, requestID string) (*emails.Mail, error) {
	if sale == nil || sale.Status != "paid" {
		return nil, fmt.Errorf("receipts are available only after payment is confirmed")
	}
	recipient = strings.TrimSpace(recipient)
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient || len(recipient) > 254 || !strings.Contains(recipient, "@") {
		return nil, fmt.Errorf("enter a valid email address")
	}
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, fmt.Errorf("reload the sale and try again")
	}
	data := posReceiptData(conf, sale)
	var plain strings.Builder
	fmt.Fprintf(&plain, "bitcoin++ %s merch receipt\nSale %s\nPaid with Lightning\n", conf.Location, sale.ID)
	for _, row := range data.Items {
		fmt.Fprintf(&plain, "%d × %s (%s) at %s each: %s\n", row.Quantity, row.Name, row.Label, row.Unit, row.Total)
	}
	fmt.Fprintf(&plain, "Total paid: %s\n%s (estimate at purchase)\nSave this receipt for your records.\n", data.Total, data.Local)
	var html bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&html, "shop/pos_receipt.tmpl", data); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(recipient))
	return &emails.Mail{JobKey: fmt.Sprintf("pos-receipt:%s:%x:%s", sale.ID, sum[:12], requestID), Email: recipient, Title: "Your bitcoin++ merch receipt", SendAt: time.Now(), HTMLBody: html.Bytes(), TextBody: []byte(plain.String())}, nil
}

func posEmailReceipt(w http.ResponseWriter, r *http.Request, app *config.AppContext, conf *types.Conf, csrf string) {
	saleID := r.PostForm.Get("sale")
	if _, err := uuid.Parse(saleID); err != nil {
		http.Error(w, "Invalid sale", 400)
		return
	}
	sale, err := getters.POSGetSale(app, saleID, conf.Ref)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := &posPage{Conf: conf, CSRF: csrf, Sale: sale, RequestID: r.PostForm.Get("receipt_request"), ReceiptEmail: r.PostForm.Get("email")}
	message, err := composePOSReceipt(app, conf, sale, p.ReceiptEmail, p.RequestID)
	if err == nil {
		if linkErr := getters.POSLinkVerifiedBuyer(app, saleID, conf.Ref, message.Email); linkErr != nil {
			err = fmt.Errorf("could not save receipt details; please retry")
		}
	}
	if err == nil {
		if app.Env.MailOff {
			err = fmt.Errorf("email delivery is disabled here; no receipt was sent")
		} else if sendErr := emails.ComposeAndSendMail(app, message); sendErr != nil {
			err = fmt.Errorf("could not queue the receipt; please retry. Payment and handover are unchanged")
		}
	}
	if err != nil {
		p.Error = err.Error()
		posRender(w, app, p)
		return
	}
	http.Redirect(w, r, r.URL.Path+"?sale="+url.QueryEscape(saleID)+"&receipt=queued", http.StatusSeeOther)
}
