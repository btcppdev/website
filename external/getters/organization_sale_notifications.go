package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

// Queue from the same transaction that confirms payment, using the organization
// recorded at purchase rather than the product's current owner.
func queueOrganizationShopSale(ctx *config.AppContext, tx pgx.Tx, orderID string) error {
	_, err := tx.Exec(ctx.DatabaseContext(), `INSERT INTO organization_sale_notifications(organization_id,order_id)
 SELECT DISTINCT fulfillment_organization_id,order_id FROM shop_order_items
 WHERE order_id=$1 AND fulfillment_organization_id IS NOT NULL
 ON CONFLICT (organization_id,order_id) DO NOTHING`, orderID)
	return err
}

func queueOrganizationPOSSale(ctx *config.AppContext, tx pgx.Tx, saleID string) error {
	_, err := tx.Exec(ctx.DatabaseContext(), `INSERT INTO organization_sale_notifications(organization_id,pos_sale_id)
 SELECT DISTINCT fulfillment_organization_id,sale_id FROM conference_pos_sale_items
 WHERE sale_id=$1 AND fulfillment_organization_id IS NOT NULL
 ON CONFLICT (organization_id,pos_sale_id) DO NOTHING`, saleID)
	return err
}

func ClaimOrganizationSaleNotification(ctx *config.AppContext) (string, error) {
	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `WITH candidate AS (
 SELECT id FROM organization_sale_notifications WHERE sent_at IS NULL AND next_attempt_at<=now()
 ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
 ) UPDATE organization_sale_notifications n SET next_attempt_at=now()+interval '10 minutes'
 FROM candidate c WHERE n.id=c.id RETURNING n.id::text`).Scan(&id)
	return id, err
}

func CompleteOrganizationSaleNotification(ctx *config.AppContext, id string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE organization_sale_notifications SET sent_at=now() WHERE id=$1`, id)
	return err
}

// One verified address for every current owner/manager, preferring their primary.
// Pending invitations, removed members and unverified addresses are not recipients.
func ListOrganizationSaleEmails(ctx *config.AppContext, orgID string) ([]string, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT DISTINCT lower(email.email::text)
 FROM organization_memberships m JOIN LATERAL (
 SELECT email FROM person_emails WHERE person_id=m.person_id AND verified_at IS NOT NULL
 ORDER BY is_primary DESC,created_at,id LIMIT 1
 ) email ON true
 WHERE m.organization_id=$1 AND m.status='active' AND m.role IN ('owner','manager')
 ORDER BY 1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		out = append(out, email)
	}
	return out, rows.Err()
}

type OrganizationSaleItem struct {
	Name, Variant, Fulfillment string
	Quantity                   int
}

type OrganizationSaleNotice struct {
	ID, OrganizationID, OrganizationName string
	Reference, Currency, Event           string
	Items                                []OrganizationSaleItem
	SupplierAmount                       uint
	HasSupplierAmount                    bool
}

func GetOrganizationSaleNotice(ctx *config.AppContext, id string) (*OrganizationSaleNotice, error) {
	n := &OrganizationSaleNotice{ID: id, HasSupplierAmount: true}
	var orderID, posID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT n.organization_id::text,o.name,coalesce(n.order_id::text,''),coalesce(n.pos_sale_id::text,'')
 FROM organization_sale_notifications n JOIN organizations o ON o.id=n.organization_id WHERE n.id=$1`, id).
		Scan(&n.OrganizationID, &n.OrganizationName, &orderID, &posID)
	if err != nil {
		return nil, err
	}
	var rows pgx.Rows
	if orderID != "" {
		err = ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT public_id,currency FROM shop_orders WHERE id=$1 AND paid_at IS NOT NULL`, orderID).Scan(&n.Reference, &n.Currency)
		if err == nil {
			rows, err = ctx.DB.Query(ctx.DatabaseContext(), `SELECT product_name_snapshot,variant_label_snapshot,fulfillment_method,quantity,supplier_amount_cents
 FROM shop_order_items WHERE order_id=$1 AND fulfillment_organization_id=$2 ORDER BY created_at,id`, orderID, n.OrganizationID)
		}
	} else {
		n.Currency = "sats"
		err = ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT s.id::text,c.description FROM conference_pos_sales s JOIN conferences c ON c.id=s.conference_id
 WHERE s.id=$1 AND s.status='paid'`, posID).Scan(&n.Reference, &n.Event)
		if err == nil {
			rows, err = ctx.DB.Query(ctx.DatabaseContext(), `SELECT product_name,variant_label,'pos_takeaway',quantity,supplier_amount_sats
 FROM conference_pos_sale_items WHERE sale_id=$1 AND fulfillment_organization_id=$2 ORDER BY variant_id`, posID, n.OrganizationID)
		}
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item OrganizationSaleItem
		var amount *uint
		if err := rows.Scan(&item.Name, &item.Variant, &item.Fulfillment, &item.Quantity, &amount); err != nil {
			return nil, err
		}
		n.Items = append(n.Items, item)
		if amount == nil {
			n.HasSupplierAmount = false // Older orders may have no recorded commercial terms.
		} else {
			n.SupplierAmount += *amount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(n.Items) == 0 {
		return nil, fmt.Errorf("organization sale notice has no items")
	}
	return n, nil
}
