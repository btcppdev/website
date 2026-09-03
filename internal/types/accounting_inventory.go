package types

import "time"

// AccountingInventoryVariant is the PII-free inventory snapshot shared with
// the accounting service. Sales and revenue are deliberately omitted because
// the accounting service derives them from the sale feed.
type AccountingInventoryVariant struct {
	SourceID     string
	SKU          string
	ProductName  string
	VariantLabel string
	OnHand       int
	UpdatedAt    time.Time
}

// AccountingInventorySale is one stable, inventory-affecting sale record.
// Ticket registrations and merchandise order items share this projection so
// the accounting service can apply effective-dated bundle recipes.
type AccountingInventorySale struct {
	SourceID         string
	SellableSourceID string
	EventID          string
	Kind             string
	ProductName      string
	VariantLabel     string
	SKU              string
	Quantity         int
	RefundedQuantity int
	RevenueCents     int64
	Currency         string
	SoldAt           time.Time
	UpdatedAt        time.Time
}
