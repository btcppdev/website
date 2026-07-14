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

func qrCodeDataURI(value string, size int) (string, error) {
	if size <= 0 {
		size = 256
	}
	qrpng, err := qrcode.Encode(value, qrcode.Medium, size)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(qrpng)), nil
}

func ticketQRCodeURI(ctx *config.AppContext, ticketRef string) (string, error) {
	return qrCodeDataURI(ticketCheckInURL(ctx, ticketRef), 256)
}
