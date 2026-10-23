package getters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

var ErrInvoiceDetails = errors.New("complete billing name, address, city, postal code and country are required")
var ErrInvoiceUnavailable = errors.New("this purchase does not have sufficient confirmed billing records for an invoice")

type InvoiceBuyer struct {
	Name, Address, City, Region, PostalCode, Country, TaxID string
}

func (b InvoiceBuyer) Validate() error {
	for _, v := range []string{b.Name, b.Address, b.City, b.PostalCode, b.Country} {
		if strings.TrimSpace(v) == "" {
			return ErrInvoiceDetails
		}
	}
	for _, v := range []string{b.Name, b.Address, b.City, b.Region, b.PostalCode, b.Country, b.TaxID} {
		if !utf8.ValidString(v) || len(v) > 500 || strings.ContainsAny(v, "\x00\r") {
			return ErrInvoiceDetails
		}
	}
	return nil
}

type InvoiceItem struct {
	Name     string
	Quantity int64
	Amount   int64
}
type InvoiceSnapshot struct {
	SellerName, SellerAddress, SellerEIN                      string
	Buyer                                                     InvoiceBuyer
	OrderReference, Currency, PaymentMethod                   string
	PurchasedAt                                               time.Time
	Subtotal, Discount, Shipping, Tax, Duty, ImportTax, Total int64
	Items                                                     []InvoiceItem
}
type PurchaseInvoice struct {
	ID       int64
	IssuedAt time.Time
	Snapshot InvoiceSnapshot
}

func (i PurchaseInvoice) Number() string { return fmt.Sprintf("BTCPP-%010d", i.ID) }
func InvoiceMoney(cents int64) string    { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

func PurchaseInvoiceForPerson(ctx context.Context, app *config.AppContext, person, ref string) (*PurchaseInvoice, error) {
	var i PurchaseInvoice
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT i.id,i.issued_at,i.snapshot FROM purchase_invoices i JOIN shop_orders o ON o.id=i.order_id JOIN people p ON p.id=i.person_id WHERE i.person_id=$1 AND o.public_id=$2 AND NOT p.is_deleted_account`, person, ref).Scan(&i.ID, &i.IssuedAt, &raw)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &i.Snapshot)
	return &i, err
}

// IssuePurchaseInvoice serializes issuance against the owned order. Retries return
// the original snapshot and number; browser input never supplies amounts or tax.
func IssuePurchaseInvoice(ctx context.Context, app *config.AppContext, person, ref string, buyer InvoiceBuyer) (*PurchaseInvoice, error) {
	if err := buyer.Validate(); err != nil {
		return nil, err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var orderID, status string
	s := InvoiceSnapshot{SellerName: "btcplusplus LLC", SellerAddress: "900 Olive St\nAustin TX 78702\nUSA", SellerEIN: "93-4644668", Buyer: buyer}
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.public_id,o.status,upper(o.currency),o.created_at,o.payment_provider,o.subtotal_cents,o.discount_amount_cents,o.shipping_amount_cents,o.sales_tax_amount_cents,o.import_duty_amount_cents,o.import_tax_amount_cents,o.total_cents FROM shop_orders o JOIN people p ON p.id=o.buyer_person_id WHERE o.buyer_person_id=$1 AND o.public_id=$2 AND NOT p.is_deleted_account FOR UPDATE OF o`, person, ref).Scan(&orderID, &s.OrderReference, &status, &s.Currency, &s.PurchasedAt, &s.PaymentMethod, &s.Subtotal, &s.Discount, &s.Shipping, &s.Tax, &s.Duty, &s.ImportTax, &s.Total)
	if err != nil {
		return nil, err
	}
	var existing PurchaseInvoice
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id,issued_at,snapshot FROM purchase_invoices WHERE order_id=$1 AND person_id=$2`, orderID, person).Scan(&existing.ID, &existing.IssuedAt, &raw)
	if err == nil {
		err = json.Unmarshal(raw, &existing.Snapshot)
		return &existing, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if status != "paid" && status != "refunded" && status != "partially_refunded" {
		return nil, ErrInvoiceUnavailable
	}
	switch s.Currency {
	case "USD", "EUR", "GBP", "CHF", "CAD", "AUD":
	default:
		return nil, ErrInvoiceUnavailable
	}
	if s.Subtotal-s.Discount+s.Shipping+s.Tax+s.Duty+s.ImportTax != s.Total {
		return nil, ErrInvoiceUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT product_name_snapshot,quantity,CASE WHEN fulfillment_method IN ('service','digital') THEN line_total_cents-tax_amount_cents ELSE line_total_cents END FROM shop_order_items WHERE order_id=$1 ORDER BY created_at,id`, orderID)
	if err != nil {
		return nil, err
	}
	var lineSum int64
	for rows.Next() {
		var item InvoiceItem
		if err = rows.Scan(&item.Name, &item.Quantity, &item.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		if item.Amount < 0 || item.Quantity <= 0 {
			rows.Close()
			return nil, ErrInvoiceUnavailable
		}
		lineSum += item.Amount
		s.Items = append(s.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(s.Items) == 0 || lineSum != s.Subtotal {
		return nil, ErrInvoiceUnavailable
	}
	raw, err = json.Marshal(s)
	if err != nil {
		return nil, err
	}
	i := PurchaseInvoice{Snapshot: s}
	err = tx.QueryRow(ctx, `INSERT INTO purchase_invoices(order_id,person_id,snapshot) VALUES($1,$2,$3) RETURNING id,issued_at`, orderID, person, raw).Scan(&i.ID, &i.IssuedAt)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &i, nil
}

func (i InvoiceItem) Money() string { return InvoiceMoney(i.Amount) }

type InvoiceTotal struct{ Label, Amount string }

func (s InvoiceSnapshot) Totals() []InvoiceTotal {
	out := []InvoiceTotal{{"Subtotal", InvoiceMoney(s.Subtotal)}, {"Discount", "−" + InvoiceMoney(s.Discount)}, {"Shipping", InvoiceMoney(s.Shipping)}, {"Sales tax", InvoiceMoney(s.Tax)}}
	if s.Duty != 0 {
		out = append(out, InvoiceTotal{"Import duty", InvoiceMoney(s.Duty)})
	}
	if s.ImportTax != 0 {
		out = append(out, InvoiceTotal{"Import tax", InvoiceMoney(s.ImportTax)})
	}
	return append(out, InvoiceTotal{"Total (" + s.Currency + ")", InvoiceMoney(s.Total)})
}
