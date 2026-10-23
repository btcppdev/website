package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
)

// PublishExpeditionPolicy switches checkout and lifecycle policy together. A
// failed activation may leave an archived revision, but never a partial rollout.
func PublishExpeditionPolicy(app *config.AppContext, p ServiceLegalPolicy) error {
	if p.ProductCode != expedition.ProductCode {
		return ErrServiceLegalRevision
	}
	if err := RegisterServiceLegalPolicy(app, p); err != nil {
		return err
	}
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-policy:"+p.ProductCode); err != nil {
		return err
	}
	if err = activateServiceLegalPolicyTx(ctx, tx, p.ProductCode, p.Version); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO expedition_lifecycle_policies(product_code,version,time_zone) VALUES($1,$2,'America/Chicago') ON CONFLICT(version) DO NOTHING`, p.ProductCode, p.Version); err != nil {
		return err
	}
	var zone string
	if err = tx.QueryRow(ctx, `SELECT time_zone FROM expedition_lifecycle_policies WHERE version=$1`, p.Version).Scan(&zone); err != nil {
		return err
	}
	if zone != "America/Chicago" {
		return ErrServiceLegalRevision
	}
	return tx.Commit(ctx)
}
