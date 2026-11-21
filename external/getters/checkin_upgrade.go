package getters

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

func CheckInEligibility(app *config.AppContext, ref string) (*types.CheckInEligibility, error) {
	e := new(types.CheckInEligibility)
	var raw []byte
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT checkin_requirements,proof_checked_at IS NOT NULL,full_ticket_upgraded_at IS NOT NULL,revoked,proof_revision FROM registrations WHERE ref_id=$1`, ref).Scan(&raw, &e.Verified, &e.Upgraded, &e.Revoked, &e.Revision)
	if err == nil {
		err = json.Unmarshal(raw, &e.Requirements)
	}
	return e, err
}

func ConfirmCheckInProof(app *config.AppContext, ref, operator string, revision int64) error {
	tag, err := app.DB.Exec(app.DatabaseContext(), `UPDATE registrations SET proof_checked_at=now(),proof_checked_by=$2,checked_in_at=coalesce(checked_in_at,now()) WHERE ref_id=$1 AND proof_revision=$3 AND NOT revoked AND lower(type)<>'sponsored' AND jsonb_array_length(checkin_requirements)>0 AND full_ticket_upgraded_at IS NULL`, ref, operator, revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("ticket changed; reload before checking proof")
	}
	return nil
}

// TicketUpgradePrice uses the current standard Bitcoin price; after sales close
// it uses the highest enabled standard tier. The existing ticket keeps its seat.
func TicketUpgradePrice(app *config.AppContext, ref string) (int64, int64, string, error) {
	var confID, currency string
	var paid *int64
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT conference_id::text,upper(currency),round(amount_paid*100)::bigint FROM registrations WHERE ref_id=$1 AND NOT revoked`, ref).Scan(&confID, &currency, &paid)
	if err != nil {
		return 0, 0, "", err
	}
	if paid == nil || *paid < 0 || currency == "" {
		return 0, 0, "", fmt.Errorf("original ticket payment is missing; ask an event admin to review it")
	}
	conf, err := GetConfByRef(app, confID)
	if err != nil {
		return 0, 0, "", err
	}
	inventory, err := TicketInventory(app, confID)
	if err != nil {
		return 0, 0, "", err
	}
	current := types.CurrentConfTicketAt(conf.Tickets, inventory.Sold+inventory.Held, time.Now())
	if current == nil {
		for _, t := range conf.Tickets {
			if t != nil && !t.Evergreen && !t.Disabled && (current == nil || t.StandardPrice() > current.StandardPrice()) {
				current = t
			}
		}
	}
	if current == nil || !strings.EqualFold(currency, current.Currency) || current.StandardPrice() == 0 {
		return 0, 0, "", fmt.Errorf("standard price is unavailable in the original payment currency; ask an event admin")
	}
	return int64(current.StandardPrice()) * 100, *paid, currency, nil
}

const upgradeColumns = `id::text,ticket_ref,owner_key::text,proof_revision,original_type,currency,previous_cents,full_price_cents,amount_cents,status,coalesce(charge_id,''),total_sats,invoice,chain_address,expires_at`

func scanCheckInUpgrade(row pgx.Row) (*types.CheckInUpgrade, error) {
	u := new(types.CheckInUpgrade)
	err := row.Scan(&u.ID, &u.TicketRef, &u.OwnerKey, &u.Revision, &u.OriginalType, &u.Currency, &u.PreviousCents, &u.FullPriceCents, &u.AmountCents, &u.Status, &u.ChargeID, &u.TotalSats, &u.Invoice, &u.ChainAddress, &u.ExpiresAt)
	return u, err
}
func GetCheckInUpgrade(app *config.AppContext, id, ref string) (*types.CheckInUpgrade, error) {
	return scanCheckInUpgrade(app.DB.QueryRow(app.DatabaseContext(), `SELECT `+upgradeColumns+` FROM ticket_checkin_upgrades WHERE id=$1 AND ticket_ref=$2`, id, ref))
}
func BeginCheckInUpgrade(app *config.AppContext, ref, operator string, revision int64) (*types.CheckInUpgrade, bool, error) {
	c := app.DatabaseContext()
	tx, err := app.DB.Begin(c)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(c)
	var key, kind string
	var currentRevision int64
	var allowed bool
	err = tx.QueryRow(c, `SELECT attendee_access_key::text,type,proof_revision,NOT revoked AND lower(type)<>'sponsored' AND proof_checked_at IS NULL AND full_ticket_upgraded_at IS NULL AND jsonb_array_length(checkin_requirements)>0 FROM registrations WHERE ref_id=$1 FOR UPDATE`, ref).Scan(&key, &kind, &currentRevision, &allowed)
	if err != nil {
		return nil, false, err
	}
	if !allowed || revision != currentRevision {
		return nil, false, fmt.Errorf("ticket changed; reload check-in")
	}
	existing, err := scanCheckInUpgrade(tx.QueryRow(c, `SELECT `+upgradeColumns+` FROM ticket_checkin_upgrades WHERE ticket_ref=$1 AND status IN ('creating','pending','processing','underpaid','review')`, ref))
	if err == nil {
		if existing.OwnerKey != key || existing.Revision != currentRevision {
			return nil, false, fmt.Errorf("an earlier upgrade needs admin review before charging this ticket again")
		}
		return existing, false, tx.Commit(c)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	full, paid, currency, err := TicketUpgradePrice(app, ref)
	if err != nil {
		return nil, false, err
	}
	amount := full - paid
	if amount < 0 {
		amount = 0
	}
	u, err := scanCheckInUpgrade(tx.QueryRow(c, `INSERT INTO ticket_checkin_upgrades(ticket_ref,owner_key,proof_revision,original_type,operator,currency,previous_cents,full_price_cents,amount_cents) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+upgradeColumns, ref, key, revision, kind, operator, currency, paid, full, amount))
	if err != nil {
		return nil, false, err
	}
	return u, true, tx.Commit(c)
}

// A fiat-denominated charge fixes the conversion once for both payment rails.
func CreateCheckInUpgradeCharge(app *config.AppContext, u *types.CheckInUpgrade) error {
	payload, err := json.Marshal(types.OpenNodeRequest{Amount: float64(u.AmountCents) / 100, Currency: u.Currency, Description: "bitcoin++ full ticket upgrade", OrderID: u.ID, CallbackURL: strings.TrimRight(app.Env.GetURI(), "/") + "/callback/opennode-ticket-upgrade/" + u.ID, TTL: 10})
	if err != nil {
		return err
	}
	var result types.OpenNodeResponse
	if err = posOpenNodeRequest(app, http.MethodPost, strings.TrimRight(app.Env.OpenNode.Endpoint, "/")+"/charges", payload, &result); err != nil {
		return err
	}
	p := result.Data
	if p == nil || p.ID == "" || p.OrderID != u.ID || p.Currency != u.Currency || math.Round(p.SourceFiatValue*100) != float64(u.AmountCents) || p.Amount == 0 || p.Amount > 500000000 || p.LNInvoice.Invoice == "" || p.LNInvoice.ExpiresAt == 0 || p.ChainInvoice.BTCAddress == "" {
		return fmt.Errorf("incomplete or mismatched upgrade charge")
	}
	_, err = app.DB.Exec(app.DatabaseContext(), `UPDATE ticket_checkin_upgrades SET charge_id=$2,total_sats=$3,invoice=$4,chain_address=$5,expires_at=$6,status=CASE WHEN status='creating' THEN 'pending' ELSE status END WHERE id=$1 AND (charge_id IS NULL OR charge_id=$2) AND (total_sats=0 OR total_sats=$3)`, u.ID, p.ID, p.Amount, p.LNInvoice.Invoice, p.ChainInvoice.BTCAddress, time.Unix(int64(p.LNInvoice.ExpiresAt), 0))
	return err
}

type CheckInUpgradeCharge struct {
	ID       string  `json:"id"`
	OrderID  string  `json:"order_id"`
	Status   string  `json:"status"`
	Currency string  `json:"currency"`
	Fiat     float64 `json:"source_fiat_value"`
	Sats     int64   `json:"price"`
	Amount   int64   `json:"amount"`
	Missing  int64   `json:"missing_amt"`
}

func LookupCheckInUpgradeCharge(app *config.AppContext, id string) (*CheckInUpgradeCharge, error) {
	u, err := url.Parse(app.Env.OpenNode.Endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = "/v2/charge/" + url.PathEscape(id)
	u.RawPath = ""
	u.RawQuery = ""
	var result struct {
		Data CheckInUpgradeCharge `json:"data"`
	}
	err = posOpenNodeRequest(app, http.MethodGet, u.String(), nil, &result)
	if err == nil && result.Data.ID != id {
		err = fmt.Errorf("charge ID mismatch")
	}
	if result.Data.Sats == 0 {
		result.Data.Sats = result.Data.Amount
	}
	return &result.Data, err
}

// Both callback and polling use authenticated provider evidence. A closed QR
// dialog still reconciles; only a confirmed, matching payment grants admission.
func ApplyCheckInUpgradePayment(app *config.AppContext, id string, p *CheckInUpgradeCharge) error {
	c := app.DatabaseContext()
	tx, err := app.DB.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	// Always lock registration before upgrade, matching the creation path.
	var ref string
	if err = tx.QueryRow(c, `SELECT ticket_ref FROM ticket_checkin_upgrades WHERE id=$1`, id).Scan(&ref); err != nil {
		return err
	}
	var key string
	var rev int64
	var blocked bool
	if err = tx.QueryRow(c, `SELECT attendee_access_key::text,proof_revision,revoked OR proof_checked_at IS NOT NULL OR full_ticket_upgraded_at IS NOT NULL FROM registrations WHERE ref_id=$1 FOR UPDATE`, ref).Scan(&key, &rev, &blocked); err != nil {
		return err
	}
	u, err := scanCheckInUpgrade(tx.QueryRow(c, `SELECT `+upgradeColumns+` FROM ticket_checkin_upgrades WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if u.Status == "paid" {
		return tx.Commit(c)
	}
	status := "paid"
	if u.AmountCents > 0 {
		if p == nil || p.ID == "" || p.OrderID != u.ID || p.Currency != u.Currency || math.IsNaN(p.Fiat) || math.Round(p.Fiat*100) != float64(u.AmountCents) || p.Sats <= 0 || (u.TotalSats != 0 && p.Sats != u.TotalSats) || (u.ChargeID != "" && p.ID != u.ChargeID) {
			return fmt.Errorf("upgrade payment does not match its quote")
		}
		status = p.Status
		if status == "unpaid" {
			status = "pending"
		}
		switch status {
		case "pending", "processing", "underpaid", "paid", "expired", "refunded", "failed":
		default:
			return fmt.Errorf("unknown payment status")
		}
		if status == "paid" && p.Missing != 0 {
			return fmt.Errorf("upgrade is not fully paid")
		}
		if _, err = tx.Exec(c, `UPDATE ticket_checkin_upgrades SET charge_id=$2,total_sats=$3,checked_at=now() WHERE id=$1`, u.ID, p.ID, p.Sats); err != nil {
			return err
		}
	}
	if status == "paid" {
		if blocked || key != u.OwnerKey || rev != u.Revision || u.Status == "expired" || u.Status == "refunded" || u.Status == "failed" || u.Status == "review" {
			status = "review"
		} else {
			// Keep the purchase amount and original ticket allocation for accounting.
			// The upgrade ledger records the additional Bitcoin payment separately.
			_, err = tx.Exec(c, `UPDATE registrations SET full_ticket_upgraded_at=now(),checked_in_at=coalesce(checked_in_at,now()),type=CASE WHEN type IN ('local','student') THEN 'genpop' ELSE type END WHERE ref_id=$1`, ref)
			if err != nil {
				return err
			}
		}
	}
	if u.Status == "review" {
		status = "review"
	}
	_, err = tx.Exec(c, `UPDATE ticket_checkin_upgrades SET status=$2,paid_at=CASE WHEN $2='paid' THEN now() ELSE paid_at END WHERE id=$1`, u.ID, status)
	if err != nil {
		return err
	}
	return tx.Commit(c)
}
