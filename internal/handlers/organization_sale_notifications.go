package handlers

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"github.com/jackc/pgx/v5"
)

func organizationSaleMail(ctx *config.AppContext, notice *getters.OrganizationSaleNotice, recipient string) (*emails.Mail, error) {
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	data := struct {
		*getters.OrganizationSaleNotice
		URL, Earnings string
	}{OrganizationSaleNotice: notice, URL: ctx.Env.GetURI() + "/dashboard/orgs/" + url.PathEscape(notice.OrganizationID) + "/shop?tab=orders"}
	if notice.HasSupplierAmount {
		if notice.Currency == "sats" {
			data.Earnings = fmt.Sprintf("%d sats", notice.SupplierAmount)
		} else {
			data.Earnings = fmt.Sprintf("%d.%02d %s", notice.SupplierAmount/100, notice.SupplierAmount%100, notice.Currency)
		}
	}
	var html bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&html, "shop/organization_sale_email.tmpl", data); err != nil {
		return nil, err
	}
	var plain strings.Builder
	fmt.Fprintf(&plain, "Payment confirmed — %s\n\nPurchase: %s\n", notice.OrganizationName, notice.Reference)
	if notice.Event != "" {
		fmt.Fprintf(&plain, "Event: %s\n", notice.Event)
	}
	for _, item := range notice.Items {
		fmt.Fprintf(&plain, "\n%d × %s · %s · %s\n", item.Quantity, item.Name, item.Variant, shopFulfillmentLabel(item.Fulfillment))
	}
	if data.Earnings != "" {
		fmt.Fprintf(&plain, "\nYour organization's share: %s\nAfter the bitcoin++ fee, excluding shipping and tax. This records the sale, not a payout; refunds may change the amount due.\n", data.Earnings)
	}
	fmt.Fprintf(&plain, "\nView purchases and shipping: %s\n\nYou're receiving this because you're an owner or manager of %s.\n", data.URL, notice.OrganizationName)
	key := fmt.Sprintf("organization-sale:%s:%x", notice.ID, sha256.Sum256([]byte(recipient)))
	return &emails.Mail{JobKey: key, Email: recipient, Title: "Payment confirmed · " + notice.OrganizationName, SendAt: time.Now(), HTMLBody: html.Bytes(), TextBody: []byte(plain.String())}, nil
}

func processOrganizationSaleNotifications(ctx *config.AppContext) {
	if ctx == nil || ctx.Env == nil || ctx.Env.MailOff {
		return
	}
	for i := 0; i < 10; i++ {
		id, err := getters.ClaimOrganizationSaleNotification(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			ctx.Err.Printf("claim organization sale notice: %s", err)
			return
		}
		if err := deliverOrganizationSaleNotification(ctx, id, emails.ComposeAndSendMail); err != nil {
			ctx.Err.Printf("organization sale notice %s: %s", id, err)
		}
	}
}

func deliverOrganizationSaleNotification(ctx *config.AppContext, id string, send func(*config.AppContext, *emails.Mail) error) error {
	notice, err := getters.GetOrganizationSaleNotice(ctx, id)
	if err != nil {
		return err
	}
	recipients, err := getters.ListOrganizationSaleEmails(ctx, notice.OrganizationID)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return fmt.Errorf("no organization owner or manager has a verified email; notice will retry")
	}
	var failures []error
	for _, recipient := range recipients {
		mail, err := organizationSaleMail(ctx, notice, recipient)
		if err == nil {
			err = send(ctx, mail)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return getters.CompleteOrganizationSaleNotification(ctx, id)
}
