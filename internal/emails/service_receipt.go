package emails

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
)

//go:embed service_receipt.html
var serviceReceiptHTML string

var serviceReceiptTemplate = template.Must(template.New("service-receipt").Parse(serviceReceiptHTML))

// ServiceReceiptMail prepares a transactional receipt without sending it. The
// caller must obtain the recipient from the immutable, account-owned order and
// persist/retry delivery with this stable job key. Newsletter consent is unrelated.
func ServiceReceiptMail(order *getters.ServiceOrderView, recipient, baseURI string) (*Mail, error) {
	if order == nil || !order.PaymentVerified || order.Status != "paid" || order.Currency != "USD" || order.PublicID == "" || order.TotalCents < 0 || order.TaxCents < 0 || order.TaxCents > order.TotalCents || order.Quote.Quantity <= 0 {
		return nil, errors.New("verified paid service order required for receipt")
	}
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient || strings.ContainsAny(recipient, "\r\n") {
		return nil, errors.New("valid order email required for receipt")
	}
	base, err := url.Parse(baseURI)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1" || base.Hostname() == "::1"))) {
		return nil, errors.New("valid receipt site URL required")
	}
	receiptURL := strings.TrimRight(base.String(), "/") + "/services/orders/" + url.PathEscape(order.PublicID)
	money := func(cents int64) string { return fmt.Sprintf("$%d.%02d", cents/100, cents%100) }
	format := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return value.UTC().Format("January 2, 2006, 15:04")
	}
	data := struct {
		ID, Name, Total, Tax, Start, End, ReceiptURL, LogoURL string
		Quantity                                              int64
		Alerts                                                bool
	}{order.PublicID, order.Quote.Name, money(order.TotalCents), money(order.TaxCents), format(order.Quote.PeriodStart), format(order.Quote.PeriodEnd), receiptURL, strings.TrimRight(base.String(), "/") + "/static/img/receipt-wordmark.png", order.Quote.Quantity, order.Quote.ProductCode == "alerts.repository-slots"}
	var html bytes.Buffer
	if err = serviceReceiptTemplate.Execute(&html, data); err != nil {
		return nil, err
	}
	text := fmt.Sprintf("Payment received\n\nYour Bitcoin++ service payment is confirmed.\n\nOrder number: %s\nPurchased: %s\nQuantity: %d\nTotal (USD, including tax): %s\nIncluded tax: %s\n", data.ID, data.Name, data.Quantity, data.Total, data.Tax)
	if data.Start != "" {
		text += "Access starts (UTC): " + data.Start + "\n"
	}
	if data.End != "" {
		text += "Access ends (UTC): " + data.End + "\n"
	}
	text += "\nView or print your receipt (sign in to your Bitcoin++ account): " + receiptURL + "\n"
	if data.Alerts {
		text += "\nManage your Alerts subscriptions: https://alerts.btcpp.dev/alerts\n"
	}
	text += "\nThis is a payment receipt, separate from release notifications and newsletters.\n"
	return &Mail{JobKey: "service-receipt:" + order.PublicID, Email: recipient, Title: "Your bitcoin++ service receipt", SendAt: time.Now(), HTMLBody: html.Bytes(), TextBody: []byte(text)}, nil
}
