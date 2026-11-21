package types

// TicketCartLine is an immutable price and ticket allocation recorded before
// creating a payment. Ticket is only used while reserving; it is not persisted.
type TicketCartLine struct {
	Ticket        *ConfTicket `json:"-" schema:"-"`
	TicketID      string      `json:"ticket_id"`
	Quantity      uint        `json:"quantity"`
	UnitCents     uint        `json:"unit_cents"`
	BaseCents     uint        `json:"base_cents"`
	OriginalCents uint        `json:"original_cents"`
	DiscountRef   string      `json:"discount_ref"`
	Kind          string      `json:"kind"`
	Title         string      `json:"title"`
	Currency      string      `json:"currency"`
}
