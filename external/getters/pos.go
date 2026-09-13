package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"math"
	"sort"
	"time"
)

func POSProducts(ctx *config.AppContext, confID string) ([]types.POSProduct, bool, error) {
	var enabled bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT coalesce((SELECT enabled FROM conference_pos_settings WHERE conference_id=$1),false)`, confID).Scan(&enabled)
	if err != nil {
		return nil, false, err
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT v.id::text,p.name,v.label,v.sku,coalesce(s.enabled,false) AND v.status='active' AND p.status<>'archived',coalesce(s.price_sats,0),coalesce(s.available,0),coalesce((SELECT sum(quantity_delta) FROM merch_inventory_events WHERE variant_id=v.id),0) FROM merch_variants v JOIN merch_products p ON p.id=v.product_id LEFT JOIN conference_pos_stock s ON s.variant_id=v.id AND s.conference_id=$1 WHERE (v.status='active' AND p.status <> 'archived') OR s.variant_id IS NOT NULL ORDER BY p.name,v.label,v.id`, confID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []types.POSProduct
	for rows.Next() {
		var p types.POSProduct
		if err = rows.Scan(&p.VariantID, &p.Name, &p.Label, &p.SKU, &p.Enabled, &p.PriceSats, &p.Available, &p.Central); err != nil {
			return nil, false, err
		}
		out = append(out, p)
	}
	return out, enabled, rows.Err()
}

// Transfers are deltas, so concurrent sales cannot turn an absolute stock edit into a restock.
type POSItemUpdate struct {
	VariantID string
	Enabled   bool
	PriceSats int64
	Transfer  int
}

func POSConfigure(ctx *config.AppContext, confID, variantID, actor, operationID string, enabled bool, price int64, transfer int) error {
	return POSConfigureItems(ctx, confID, actor, operationID, []POSItemUpdate{{VariantID: variantID, Enabled: enabled, PriceSats: price, Transfer: transfer}})
}

// Save the entire table atomically: a bad row cannot leave earlier transfers committed.
func POSConfigureItems(ctx *config.AppContext, confID, actor, operationID string, items []POSItemUpdate) error {
	if len(items) == 0 || len(items) > 2000 {
		return fmt.Errorf("choose between 1 and 2000 items")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if _, err := uuid.Parse(item.VariantID); err != nil || seen[item.VariantID] {
			return fmt.Errorf("invalid or duplicate item")
		}
		seen[item.VariantID] = true
		if item.PriceSats < 0 || item.PriceSats > 500000000 || (item.Enabled && item.PriceSats == 0) || item.Transfer < -1000000 || item.Transfer > 1000000 {
			return fmt.Errorf("each enabled item needs a positive sats price and a valid stock transfer")
		}
	}
	items = append([]POSItemUpdate(nil), items...)
	sort.Slice(items, func(i, j int) bool { return items[i].VariantID < items[j].VariantID })
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	for _, item := range items {
		if err = posConfigureItem(ctx, tx, confID, item.VariantID, actor, operationID, item.Enabled, item.PriceSats, item.Transfer); err != nil {
			return err
		}
	}
	return tx.Commit(ctx.DatabaseContext())
}

func posConfigureItem(ctx *config.AppContext, tx pgx.Tx, confID, variantID, actor, operationID string, enabled bool, price int64, transfer int) error {
	var err error
	if _, err := uuid.Parse(operationID); err != nil {
		return fmt.Errorf("invalid stock operation")
	}
	if _, err = tx.Exec(ctx.DatabaseContext(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, operationID+variantID); err != nil {
		return err
	}
	var applied bool
	if err = tx.QueryRow(ctx.DatabaseContext(), `SELECT EXISTS(SELECT 1 FROM conference_pos_events WHERE conference_id=$1 AND variant_id=$2 AND operation_id=$3)`, confID, variantID, operationID).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	// Serialize against online reservations and other event transfers.
	var central int
	if _, err = tx.Exec(ctx.DatabaseContext(), `SELECT id FROM merch_variants WHERE id=$1 FOR UPDATE`, variantID); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx.DatabaseContext(), `SELECT coalesce(sum(quantity_delta),0) FROM merch_inventory_events WHERE variant_id=$1`, variantID).Scan(&central); err != nil {
		return err
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_stock(conference_id,variant_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, confID, variantID)
	if err != nil {
		return err
	}
	var available int
	if err = tx.QueryRow(ctx.DatabaseContext(), `SELECT available FROM conference_pos_stock WHERE conference_id=$1 AND variant_id=$2 FOR UPDATE`, confID, variantID).Scan(&available); err != nil {
		return err
	}
	if transfer > 0 && central < transfer {
		return fmt.Errorf("only %d units are available in central stock", central)
	}
	if available+transfer < 0 {
		return fmt.Errorf("only %d unreserved units can be returned", available)
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_stock SET enabled=$3,price_sats=$4,available=available+$5 WHERE conference_id=$1 AND variant_id=$2`, confID, variantID, enabled, price, transfer)
	if err != nil {
		return err
	}
	if transfer != 0 {
		_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO merch_inventory_events(variant_id,event_type,quantity_delta,conference_id,actor_email,notes) VALUES($1,'adjustment',$2,$3,NULLIF($4,'')::citext,'Transfer to/from event POS')`, variantID, -transfer, confID, actor)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_events(conference_id,variant_id,actor,description,operation_id) VALUES($1,$2,$3,$4,$5)`, confID, variantID, actor, fmt.Sprintf("price=%d sats; enabled=%t; transfer=%d", price, enabled, transfer), operationID)
	if err != nil {
		return err
	}
	return nil
}
func POSSetEnabled(ctx *config.AppContext, confID, actor string, enabled bool) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_settings(conference_id,enabled) VALUES($1,$2) ON CONFLICT(conference_id) DO UPDATE SET enabled=excluded.enabled`, confID, enabled)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_events(conference_id,actor,description) VALUES($1,$2,$3)`, confID, actor, fmt.Sprintf("Register enabled=%t", enabled))
	if err != nil {
		return err
	}
	return tx.Commit(ctx.DatabaseContext())
}

func ValidatePOSCart(lines []types.POSCartLine) error {
	if len(lines) == 0 || len(lines) > 100 {
		return fmt.Errorf("choose 1–100 variants")
	}
	seen := map[string]bool{}
	var total int64
	for _, l := range lines {
		if _, err := uuid.Parse(l.VariantID); err != nil {
			return fmt.Errorf("invalid variant")
		}
		if seen[l.VariantID] || l.Quantity < 1 || l.Quantity > 1000 || l.PriceSats < 1 || l.PriceSats > 500000000 {
			return fmt.Errorf("invalid cart item")
		}
		seen[l.VariantID] = true
		total += l.PriceSats * int64(l.Quantity)
	}
	if total > 500000000 {
		return fmt.Errorf("cart exceeds Lightning payment limit")
	}
	return nil
}

// Returns created=false on retry: callers must never create a second provider charge.
func POSCreateSale(ctx *config.AppContext, confID, operator, requestID, currency string, rate float64, lines []types.POSCartLine) (*types.POSSale, bool, error) {
	if err := ValidatePOSCart(lines); err != nil {
		return nil, false, err
	}
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, false, fmt.Errorf("invalid checkout request")
	}
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return nil, false, fmt.Errorf("local currency estimate unavailable")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	// One lock per request serializes double taps across servers.
	if _, err = tx.Exec(ctx.DatabaseContext(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, requestID); err != nil {
		return nil, false, err
	}
	var prior, priorConf, priorOperator string
	err = tx.QueryRow(ctx.DatabaseContext(), `SELECT id::text,conference_id::text,operator_id FROM conference_pos_sales WHERE request_id=$1`, requestID).Scan(&prior, &priorConf, &priorOperator)
	if err == nil {
		if priorConf != confID || priorOperator != operator {
			return nil, false, fmt.Errorf("checkout belongs to another register")
		}
		sale, err := POSGetSale(ctx, prior, confID)
		return sale, false, err
	}
	if err != pgx.ErrNoRows {
		return nil, false, err
	}
	var enabled bool
	if err = tx.QueryRow(ctx.DatabaseContext(), `SELECT enabled FROM conference_pos_settings WHERE conference_id=$1 FOR SHARE`, confID).Scan(&enabled); err != nil || !enabled {
		return nil, false, fmt.Errorf("register is closed")
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].VariantID < lines[j].VariantID })
	var total int64
	var items []types.POSSaleItem
	for _, l := range lines {
		var item types.POSSaleItem
		var available int
		var sellable bool
		err = tx.QueryRow(ctx.DatabaseContext(), `SELECT s.available,s.price_sats,s.enabled AND v.status='active' AND p.status<>'archived',p.name,v.label FROM conference_pos_stock s JOIN merch_variants v ON v.id=s.variant_id JOIN merch_products p ON p.id=v.product_id WHERE s.conference_id=$1 AND s.variant_id=$2 FOR UPDATE OF s`, confID, l.VariantID).Scan(&available, &item.PriceSats, &sellable, &item.Name, &item.Label)
		if err != nil {
			return nil, false, fmt.Errorf("item is not configured for this event")
		}
		if !sellable || available < l.Quantity {
			return nil, false, fmt.Errorf("%s: only %d available", item.Name, available)
		}
		if item.PriceSats != l.PriceSats {
			return nil, false, fmt.Errorf("%s price changed; refresh the register", item.Name)
		}
		item.VariantID = l.VariantID
		item.Quantity = l.Quantity
		items = append(items, item)
		total += item.PriceSats * int64(item.Quantity)
	}
	var id string
	err = tx.QueryRow(ctx.DatabaseContext(), `INSERT INTO conference_pos_sales(conference_id,request_id,operator_id,total_sats,currency,local_per_btc) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, confID, requestID, operator, total, currency, rate).Scan(&id)
	if err != nil {
		return nil, false, err
	}
	for _, item := range items {
		_, err = tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_stock SET available=available-$3 WHERE conference_id=$1 AND variant_id=$2`, confID, item.VariantID, item.Quantity)
		if err != nil {
			return nil, false, err
		}
		_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_sale_items(sale_id,variant_id,product_name,variant_label,quantity,price_sats) VALUES($1,$2,$3,$4,$5,$6)`, id, item.VariantID, item.Name, item.Label, item.Quantity, item.PriceSats)
		if err != nil {
			return nil, false, err
		}
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_events(conference_id,sale_id,actor,description) VALUES($1,$2,$3,'Sale reserved')`, confID, id, operator)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, false, err
	}
	sale, err := POSGetSale(ctx, id, confID)
	return sale, true, err
}
func POSGetSale(ctx *config.AppContext, id, confID string) (*types.POSSale, error) {
	s := new(types.POSSale)
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT id::text,conference_id::text,operator_id,status,total_sats,currency,local_per_btc,coalesce(charge_id,''),invoice,expires_at,created_at,paid_at,handed_over_at FROM conference_pos_sales WHERE id=$1 AND conference_id=$2`, id, confID).Scan(&s.ID, &s.ConferenceID, &s.OperatorID, &s.Status, &s.TotalSats, &s.Currency, &s.LocalPerBTC, &s.ChargeID, &s.Invoice, &s.ExpiresAt, &s.CreatedAt, &s.PaidAt, &s.HandedOverAt)
	if err != nil {
		return nil, err
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT variant_id::text,product_name,variant_label,quantity,price_sats FROM conference_pos_sale_items WHERE sale_id=$1 ORDER BY product_name,variant_label`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i types.POSSaleItem
		if err = rows.Scan(&i.VariantID, &i.Name, &i.Label, &i.Quantity, &i.PriceSats); err != nil {
			return nil, err
		}
		s.Items = append(s.Items, i)
	}
	return s, rows.Err()
}
func POSRecentSales(ctx *config.AppContext, confID string) ([]*types.POSSale, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT id::text FROM conference_pos_sales WHERE conference_id=$1 ORDER BY created_at DESC LIMIT 30`, confID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []*types.POSSale
	for _, id := range ids {
		s, e := POSGetSale(ctx, id, confID)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, nil
}
func POSAttachCharge(ctx *config.AppContext, saleID string, p *types.OpenNodePayment) error {
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_sales SET charge_id=$2,invoice=$3,expires_at=to_timestamp($4),status=CASE WHEN status='creating' THEN 'pending' ELSE status END WHERE id=$1 AND (charge_id IS NULL OR charge_id=$2) AND total_sats=$5`, saleID, p.ID, p.LNInvoice.Invoice, int64(p.LNInvoice.ExpiresAt), int64(p.Amount))
	if err == nil && tag.RowsAffected() != 1 {
		return fmt.Errorf("charge does not match sale")
	}
	return err
}

// Only call after fetching the charge from OpenNode, never from untrusted webhook fields.
func POSApplyPayment(ctx *config.AppContext, saleID, chargeID, status string, amount int64) error {
	if status != "paid" && status != "expired" {
		return nil
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var conf, old, bound string
	var expected int64
	err = tx.QueryRow(ctx.DatabaseContext(), `SELECT conference_id::text,status,total_sats,coalesce(charge_id,'') FROM conference_pos_sales WHERE id=$1 FOR UPDATE`, saleID).Scan(&conf, &old, &expected, &bound)
	if err != nil {
		return err
	}
	if amount != expected || chargeID == "" || (bound != "" && bound != chargeID) {
		return fmt.Errorf("payment does not match sale")
	}
	if old == "paid" || old == status || old == "review" || (old == "cancelled" && status == "expired") {
		return nil
	}
	// A payment reported after stock was released needs admin reconciliation, never a second stock deduction.
	if (old == "expired" || old == "cancelled") && status == "paid" {
		status = "review"
	}
	if status == "expired" {
		_, err = tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_stock s SET available=s.available+i.quantity FROM conference_pos_sale_items i WHERE i.sale_id=$1 AND s.variant_id=i.variant_id AND s.conference_id=$2`, saleID, conf)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_sales SET status=$2,charge_id=$3,paid_at=CASE WHEN $2 IN ('paid','review') THEN now() ELSE paid_at END WHERE id=$1`, saleID, status, chargeID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_events(conference_id,sale_id,actor,description) VALUES($1,$2,'opennode',$3)`, conf, saleID, "Payment "+status)
	if err != nil {
		return err
	}
	return tx.Commit(ctx.DatabaseContext())
}
func POSHandOver(ctx *config.AppContext, id, confID, actor string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	tag, err := tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_sales SET handed_over_at=now() WHERE id=$1 AND conference_id=$2 AND status='paid' AND handed_over_at IS NULL`, id, confID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_events(conference_id,sale_id,actor,description) VALUES($1,$2,$3,'Items handed over')`, confID, id, actor)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx.DatabaseContext())
}

// An admin can recover a provider response lost in transit using its verified charge ID.
func POSRecoverCharge(ctx *config.AppContext, saleID, confID, actor string, charge *POSCharge) error {
	if charge.OrderID != saleID {
		return fmt.Errorf("charge belongs to a different sale")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	tag, err := tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_sales SET charge_id=$3,status='pending' WHERE id=$1 AND conference_id=$2 AND status='creating' AND charge_id IS NULL AND total_sats=$4`, saleID, confID, charge.ID, charge.Price)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("sale cannot be recovered with that charge")
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_events(conference_id,sale_id,actor,description) VALUES($1,$2,$3,'Recovered OpenNode charge')`, confID, saleID, actor)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx.DatabaseContext()); err != nil {
		return err
	}
	return POSApplyPayment(ctx, saleID, charge.ID, charge.Status, charge.Price)
}

type POSAuditEvent struct {
	Actor, Description, VariantID, SaleID string
	CreatedAt                             time.Time
}

func POSAudit(ctx *config.AppContext, confID string) ([]POSAuditEvent, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT actor,description,coalesce(variant_id::text,''),coalesce(sale_id::text,''),created_at FROM conference_pos_events WHERE conference_id=$1 ORDER BY id DESC LIMIT 100`, confID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []POSAuditEvent
	for rows.Next() {
		var e POSAuditEvent
		if err = rows.Scan(&e.Actor, &e.Description, &e.VariantID, &e.SaleID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Only an admin who checked the provider dashboard may cancel an ambiguous
// creation failure. Pending invoices must expire at the provider instead.
func POSCancelUnconfirmed(ctx *config.AppContext, saleID, confID, actor string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var status string
	var charge *string
	var created time.Time
	if err = tx.QueryRow(ctx.DatabaseContext(), `SELECT status,charge_id,created_at FROM conference_pos_sales WHERE id=$1 AND conference_id=$2 FOR UPDATE`, saleID, confID).Scan(&status, &charge, &created); err != nil {
		return err
	}
	if status == "cancelled" {
		return nil
	}
	if status != "creating" || charge != nil || time.Since(created) < 2*time.Minute {
		return fmt.Errorf("only unconfirmed checkouts older than two minutes can be cancelled")
	}
	if _, err = tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_stock s SET available=s.available+i.quantity FROM conference_pos_sale_items i WHERE i.sale_id=$1 AND s.variant_id=i.variant_id AND s.conference_id=$2`, saleID, confID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_sales SET status='cancelled' WHERE id=$1`, saleID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO conference_pos_events(conference_id,sale_id,actor,description) VALUES($1,$2,$3,'Admin verified no OpenNode charge; cancelled checkout and released stock')`, confID, saleID, actor); err != nil {
		return err
	}
	return tx.Commit(ctx.DatabaseContext())
}
