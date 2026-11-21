package helpers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"net/url"
)

func TicketDetailsSignature(ctx *config.AppContext, row *types.TicketAttendeeDetails) string {
	return CreateScopedHMAC(ctx, "ticket-attendee-details", row.RefID+"\x00"+row.AccessKey)
}

func TicketDetailsURL(ctx *config.AppContext, row *types.TicketAttendeeDetails) string {
	return "/ticket/" + url.PathEscape(row.RefID) + "/details?key=" + url.QueryEscape(TicketDetailsSignature(ctx, row))
}
