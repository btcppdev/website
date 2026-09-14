package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/types"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func requireMerchAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	return auth.RequireRole(w, r, ctx, auth.Spec{Conf: "merch", Role: auth.RoleAdmin})
}

func merchSaleMail(ctx *config.AppContext, order *types.ShopOrder, recipient string) (*emails.Mail, error) {
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	data := struct {
		Order *types.ShopOrder
		URL   string
	}{order, ctx.Env.GetURI() + "/admin/merch/orders/" + order.ID}
	var html bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&html, "shop/admin_sale_email.tmpl", data); err != nil {
		return nil, err
	}
	var plain strings.Builder
	fmt.Fprintf(&plain, "New paid merch order %s\n\n", order.PublicID)
	for _, item := range order.Items {
		if item.VariantID != "" {
			fmt.Fprintf(&plain, "%d × %s · %s · %s\n", item.Quantity, item.ProductNameSnapshot, item.VariantLabelSnapshot, shopFulfillmentLabel(item.FulfillmentMethod))
		}
	}
	fmt.Fprintf(&plain, "\nOrder total: %s %s\nManage order: %s\n", merchMoney(order.TotalCents, nil), order.Currency, data.URL)
	key := fmt.Sprintf("merch-sale:%s:%x", order.ID, sha256.Sum256([]byte(recipient)))
	return &emails.Mail{JobKey: key, Email: recipient, Title: "New merch order · " + order.PublicID, SendAt: time.Now(), HTMLBody: html.Bytes(), TextBody: []byte(plain.String())}, nil
}

func processMerchSaleNotifications(ctx *config.AppContext) {
	if ctx.Env == nil || ctx.Env.MailOff {
		return
	}
	for i := 0; i < 10; i++ {
		id, err := getters.ClaimMerchSaleNotification(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			ctx.Err.Printf("claim merch sale notice: %s", err)
			return
		}
		if err := deliverMerchSaleNotification(ctx, id, emails.ComposeAndSendMail); err != nil {
			ctx.Err.Printf("merch sale notice %s: %s", id, err)
		}
	}
}

func deliverMerchSaleNotification(ctx *config.AppContext, id string, send func(*config.AppContext, *emails.Mail) error) error {
	order, err := getters.GetShopOrderByID(ctx, id)
	if err != nil {
		return err
	}
	recipients, err := getters.ListMerchAdminEmails(ctx)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return fmt.Errorf("no merch-admin has a verified email; notice will retry")
	}
	var failures []error
	for _, recipient := range recipients {
		mail, err := merchSaleMail(ctx, order, recipient)
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
	return getters.CompleteMerchSaleNotification(ctx, id)
}
