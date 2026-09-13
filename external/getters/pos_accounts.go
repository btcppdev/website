package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"strings"
	"time"
)

// Receipt resends may not move a purchase to a different account.
func POSLinkVerifiedBuyer(ctx *config.AppContext, saleID, confID, email string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE conference_pos_sales SET buyer_person_id=(SELECT person_id FROM person_emails WHERE email=$3::citext AND verified_at IS NOT NULL) WHERE id=$1 AND conference_id=$2 AND status='paid' AND buyer_person_id IS NULL`, saleID, confID, strings.TrimSpace(email))
	return err
}

type POSPurchase struct {
	ID, Event, Currency string
	TotalSats           int64
	LocalEstimate       float64
	CreatedAt           time.Time
}

func POSPurchasesForPerson(ctx *config.AppContext, personID string, limit int) ([]POSPurchase, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `SELECT s.id::text,c.location,s.currency,s.total_sats,s.total_sats*s.local_per_btc/100000000,s.created_at FROM conference_pos_sales s JOIN conferences c ON c.id=s.conference_id WHERE s.buyer_person_id=$1 AND s.status='paid' ORDER BY s.created_at DESC LIMIT $2`, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []POSPurchase
	for rows.Next() {
		var p POSPurchase
		if err := rows.Scan(&p.ID, &p.Event, &p.Currency, &p.TotalSats, &p.LocalEstimate, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func POSPurchaseForPerson(ctx *config.AppContext, personID, saleID string) (*types.POSSale, string, error) {
	var confID, event string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT s.conference_id::text,c.location FROM conference_pos_sales s JOIN conferences c ON c.id=s.conference_id WHERE s.id=$1 AND s.buyer_person_id=$2 AND s.status='paid'`, saleID, personID).Scan(&confID, &event)
	if err != nil {
		return nil, "", err
	}
	sale, err := POSGetSale(ctx, saleID, confID)
	return sale, event, err
}
