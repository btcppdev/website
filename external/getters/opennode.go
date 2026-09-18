package getters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/i18n"
	"btcpp-web/internal/types"
)

const CHARGES_ENDPOINT = "/charges"

var openNodeHTTPClient = &http.Client{Timeout: 15 * time.Second}

func InitOpenNodeCheckout(ctx *config.AppContext, tixPrice, preDiscountPrice uint, tix *types.ConfTicket, conf *types.Conf, ticketKind string, count uint, email string, discountRef string, subNewsletter bool, addOnCents uint, shopOrderID string, locale string) (*types.OpenNodePayment, error) {
	if ticketKind == "" {
		ticketKind = types.TicketTypeGeneral
	}

	metadata := &types.OpenNodeMetadata{
		Locale:      types.RegistrationLocale(locale),
		Email:       email,
		Quantity:    float64(count),
		ConfRef:     conf.Ref,
		TixLocal:    ticketKind == types.TicketTypeLocal,
		TicketKind:  ticketKind,
		DiscountRef: discountRef,
		/* We have to save it b/c OpenNode doesnt */
		Currency:         tix.Currency,
		Subscribe:        subNewsletter,
		ShopOrderID:      shopOrderID,
		AddOnCents:       int64(addOnCents),
		TicketTotalCents: int64(tixPrice) * int64(count) * 100,
		// Pre-discount per-ticket price in the buyer's selected
		// currency (BTC / USD / Local). tixPrice here is the post-
		// discount form value; preDiscountPrice is the original tier.
		PreDiscountCents: int64(preDiscountPrice) * 100,
	}

	domain := ctx.Env.GetURI()
	onReq := &types.OpenNodeRequest{
		Amount:        float64(tixPrice*count) + (float64(addOnCents) / 100),
		Description:   conf.Desc,
		Currency:      strings.ToUpper(strings.TrimSpace(tix.Currency)),
		CallbackURL:   domain + "/callback/opennode",
		SuccessURL:    i18n.CheckoutURL(locale, domain+"/"+conf.Tag+"/success"),
		AutoSettle:    false,
		TTL:           uint(types.ShopCheckoutSessionTTL / time.Minute),
		Metadata:      metadata,
		NotifEmail:    email,
		CustomerEmail: email,
	}

	payload, err := json.Marshal(onReq)
	if err != nil {
		return nil, err
	}

	chargesURL := ctx.Env.OpenNode.Endpoint + CHARGES_ENDPOINT
	req, err := http.NewRequest("POST", chargesURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", ctx.Env.OpenNode.Key)
	req.Header.Add("Content-Type", "application/json")

	resp, err := openNodeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("error returned from opennode %d: %s", resp.StatusCode, body)
	}

	var onresp types.OpenNodeResponse
	if err := json.Unmarshal(body, &onresp); err != nil {
		return nil, fmt.Errorf("decode opennode response: %w", err)
	}
	if onresp.Data == nil {
		return nil, fmt.Errorf("opennode response is missing charge data")
	}

	return onresp.Data, nil
}

func InitOpenNodeShopCheckout(ctx *config.AppContext, order *types.ShopOrder) (*types.OpenNodePayment, error) {
	if order == nil {
		return nil, fmt.Errorf("missing shop order")
	}

	metadata := &types.OpenNodeMetadata{
		Email:       order.BuyerEmail,
		Quantity:    0,
		Currency:    order.Currency,
		ShopOrderID: order.ID,
	}

	domain := ctx.Env.GetURI()
	onReq := &types.OpenNodeRequest{
		Amount:        float64(order.TotalCents) / 100,
		Description:   "bitcoin++ merch order " + order.PublicID,
		Currency:      strings.ToUpper(strings.TrimSpace(order.Currency)),
		CallbackURL:   domain + "/callback/opennode",
		SuccessURL:    domain + "/shop/success/" + order.PublicID,
		AutoSettle:    false,
		TTL:           uint(types.ShopCheckoutSessionTTL / time.Minute),
		Metadata:      metadata,
		NotifEmail:    order.BuyerEmail,
		CustomerEmail: order.BuyerEmail,
		CustomerName:  order.BuyerName,
		OrderID:       order.PublicID,
	}

	payload, err := json.Marshal(onReq)
	if err != nil {
		return nil, err
	}

	chargesURL := ctx.Env.OpenNode.Endpoint + CHARGES_ENDPOINT
	req, err := http.NewRequest("POST", chargesURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", ctx.Env.OpenNode.Key)
	req.Header.Add("Content-Type", "application/json")

	resp, err := openNodeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("error returned from opennode %d: %s", resp.StatusCode, body)
	}

	var onresp types.OpenNodeResponse
	if err := json.Unmarshal(body, &onresp); err != nil {
		return nil, fmt.Errorf("decode opennode response: %w", err)
	}
	if onresp.Data == nil {
		return nil, fmt.Errorf("opennode response is missing charge data")
	}

	return onresp.Data, nil
}

// InitOpenNodeServiceCheckout creates a Lightning checkout for a durable
// service-payment attempt. The attempt UUID is provider metadata so a fetched,
// paid charge can be matched to the immutable quote before fulfillment.
func InitOpenNodeServiceCheckout(ctx *config.AppContext, attempt *ServicePaymentAttempt, purchase *ServicePurchase, quote ServiceQuote, successURL string) (*types.OpenNodePayment, error) {
	if attempt == nil || purchase == nil || attempt.ID == "" || purchase.OrderID == "" || quote.TotalCents <= 0 {
		return nil, fmt.Errorf("missing service checkout")
	}
	metadata := &types.OpenNodeMetadata{
		Email: purchase.BuyerEmail, Quantity: float64(quote.Quantity), Currency: "USD",
		ServiceAttemptID: attempt.ID, ServiceOrderID: purchase.OrderID,
	}
	request := &types.OpenNodeRequest{
		Amount: float64(quote.TotalCents) / 100, Description: quote.Name, Currency: "USD",
		CallbackURL: strings.TrimRight(ctx.Env.GetURI(), "/") + "/callback/opennode",
		SuccessURL:  successURL, AutoSettle: false, TTL: uint(types.ShopCheckoutSessionTTL / time.Minute),
		Metadata: metadata, NotifEmail: purchase.BuyerEmail, CustomerEmail: purchase.BuyerEmail,
		// OpenNode receives the durable attempt as its merchant order key. If
		// creation times out, a retry carries the same provider-visible key.
		OrderID: attempt.ID,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(ctx.Env.OpenNode.Endpoint, "/")+CHARGES_ENDPOINT, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", ctx.Env.OpenNode.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := openNodeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("error returned from opennode %d: %s", resp.StatusCode, body)
	}
	var response types.OpenNodeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode opennode response: %w", err)
	}
	if response.Data == nil || strings.TrimSpace(response.Data.ID) == "" || strings.TrimSpace(response.Data.HostedCheckoutURL) == "" {
		return nil, fmt.Errorf("opennode response is missing service charge data")
	}
	return response.Data, nil
}
