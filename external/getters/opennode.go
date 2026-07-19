package getters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const CHARGES_ENDPOINT = "/charges"

var openNodeHTTPClient = &http.Client{Timeout: 15 * time.Second}

func InitOpenNodeCheckout(ctx *config.AppContext, tixPrice, preDiscountPrice uint, tix *types.ConfTicket, conf *types.Conf, ticketKind string, count uint, email string, discountRef string, subNewsletter bool, addOnCents uint, shopOrderID string) (*types.OpenNodePayment, error) {
	if ticketKind == "" {
		ticketKind = types.TicketTypeGeneral
	}

	metadata := &types.OpenNodeMetadata{
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
		SuccessURL:    domain + "/" + conf.Tag + "/success",
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
