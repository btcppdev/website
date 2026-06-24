package getters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/niftynei/go-notion"
)

func ListDiscountsNotion(n *types.Notion) ([]*types.DiscountCode, error) {
	var discounts []*types.DiscountCode

	hasMore := true
	nextCursor := ""
	for hasMore {
		var err error
		var pages []*notion.Page

		pages, nextCursor, hasMore, err = n.Client.QueryDatabase(context.Background(),
			n.Config.DiscountsDb, notion.QueryDatabaseParam{
				StartCursor: nextCursor,
			})

		if err != nil {
			return nil, err
		}
		for _, page := range pages {
			discount := parseDiscount(page.ID, page.Properties)
			discounts = append(discounts, discount)
		}
	}

	return discounts, nil
}

func incrementDiscountUsesNotion(ctx *config.AppContext, discountRef string, addCount uint) error {
	discounts, err := listDiscounts(ctx)
	if err != nil {
		return err
	}

	var currentUses uint
	for _, d := range discounts {
		if d.Ref == discountRef {
			currentUses = d.UsesCount
			break
		}
	}

	newCount := float64(currentUses + addCount)

	_, err = ctx.Notion.Client.UpdatePageProperties(context.Background(), discountRef,
		map[string]*notion.PropertyValue{
			"UsesCount": {
				Type:   notion.PropertyNumber,
				Number: newCount,
			},
		})

	return err
}

func createDiscountNotion(n *types.Notion, in DiscountInput) (string, error) {
	props := map[string]*notion.PropertyValue{
		"CodeName":   titleValue(in.CodeName),
		"Discount":   richTextValue(in.DiscountExpr),
		"Conference": relationValue([]string{in.ConfRef}),
	}
	if in.AffiliateEmail != "" {
		props["AffiliateEmail"] = notion.NewEmailPropertyValue(in.AffiliateEmail)
	}

	parent := notion.NewDatabaseParent(n.Config.DiscountsDb)
	page, err := n.Client.CreatePage(context.Background(), parent, props)
	if err != nil {
		return "", err
	}
	return page.ID, nil
}

func updateDiscountNotion(ctx *config.AppContext, discountID string, in DiscountInput) error {
	props := map[string]*notion.PropertyValue{
		"CodeName":       titleValue(in.CodeName),
		"Discount":       richTextValue(in.DiscountExpr),
		"Conference":     relationValue([]string{in.ConfRef}),
		"AffiliateEmail": notion.NewEmailPropertyValue(in.AffiliateEmail),
	}
	_, err := ctx.Notion.Client.UpdatePageProperties(context.Background(), discountID, props)
	if err != nil {
		return err
	}
	return nil
}

func archiveDiscountNotion(ctx *config.AppContext, discountID string) error {
	body, err := json.Marshal(map[string]interface{}{"archived": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PATCH",
		"https://api.notion.com/v1/pages/"+discountID,
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+ctx.Notion.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Notion-Version", "2022-06-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("notion archive discount %s: %v", discountID, errResp)
	}
	return nil
}
