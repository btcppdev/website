package handlers

import (
	"encoding/base64"
	"fmt"

	"btcpp-web/internal/config"

	qrcode "github.com/skip2/go-qrcode"
)

func ticketCheckInURL(ctx *config.AppContext, ticketRef string) string {
	return fmt.Sprintf("%s/check-in/%s", ctx.Env.GetURI(), ticketRef)
}

func ticketQRCodeURI(ctx *config.AppContext, ticketRef string) (string, error) {
	qrpng, err := qrcode.Encode(ticketCheckInURL(ctx, ticketRef), qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(qrpng)), nil
}
