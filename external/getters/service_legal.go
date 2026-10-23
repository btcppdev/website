package getters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"github.com/jackc/pgx/v5"
)

var ErrServiceLegalReview = errors.New("review and accept the current service terms")
var ErrServiceLegalRevision = errors.New("service policy version has different contents")

type ServiceLegalPolicy struct {
	ProductCode   string
	Version       string
	TermsText     string
	PrivacyText   string
	ContentSHA256 string
	EffectiveAt   time.Time
}

// RegisterServiceLegalPolicy archives reviewed text without activating it. The
// operator must use the exact snapshot published by the product's policy pages.
func RegisterServiceLegalPolicy(app *config.AppContext, p ServiceLegalPolicy) error {
	if strings.TrimSpace(p.Version) != p.Version || p.Version == "" || len(p.Version) > 200 || strings.ContainsAny(p.Version, "\r\n\x00") || strings.TrimSpace(p.TermsText) == "" || strings.TrimSpace(p.PrivacyText) == "" || len(p.TermsText) > 256<<10 || len(p.PrivacyText) > 256<<10 || p.EffectiveAt.IsZero() {
		return ErrServiceLegalRevision
	}
	raw, _ := json.Marshal([]string{p.TermsText, p.PrivacyText})
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	ctx := app.DatabaseContext()
	_, err := app.DB.Exec(ctx, `INSERT INTO service_legal_revisions(product_code,version,terms_text,privacy_text,content_sha256,effective_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, p.ProductCode, p.Version, p.TermsText, p.PrivacyText, digest, p.EffectiveAt)
	if err != nil {
		return err
	}
	var same bool
	err = app.DB.QueryRow(ctx, `SELECT terms_text=$3 AND privacy_text=$4 AND content_sha256=$5 AND effective_at=$6 FROM service_legal_revisions WHERE product_code=$1 AND version=$2`, p.ProductCode, p.Version, p.TermsText, p.PrivacyText, digest, p.EffectiveAt).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return ErrServiceLegalRevision
	}
	return nil
}

func ActivateServiceLegalPolicy(app *config.AppContext, product, version string) error {
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-policy:"+product); err != nil {
		return err
	}
	if err = activateServiceLegalPolicyTx(ctx, tx, product, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func activateServiceLegalPolicyTx(ctx context.Context, tx pgx.Tx, product, version string) error {
	var effective time.Time
	if err := tx.QueryRow(ctx, `SELECT effective_at FROM service_legal_revisions WHERE product_code=$1 AND version=$2`, product, version).Scan(&effective); err != nil {
		return err
	}
	if effective.After(time.Now()) {
		return ErrServiceLegalReview
	}
	var previous time.Time
	err := tx.QueryRow(ctx, `SELECT r.effective_at FROM service_legal_current c JOIN service_legal_revisions r USING(product_code,version) WHERE c.product_code=$1`, product).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if previous.After(effective) {
		return ErrServiceLegalRevision
	}
	if _, err = tx.Exec(ctx, `INSERT INTO service_legal_current(product_code,version) VALUES($1,$2) ON CONFLICT(product_code) DO UPDATE SET version=excluded.version`, product, version); err != nil {
		return err
	}
	return nil
}

func CurrentServiceLegalPolicy(app *config.AppContext, product string) (*ServiceLegalPolicy, error) {
	p := &ServiceLegalPolicy{ProductCode: product}
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT r.version,r.terms_text,r.privacy_text,r.content_sha256,r.effective_at FROM service_legal_current c JOIN service_legal_revisions r USING(product_code,version) WHERE c.product_code=$1`, product).Scan(&p.Version, &p.TermsText, &p.PrivacyText, &p.ContentSHA256, &p.EffectiveAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func serviceLegalAcceptanceTx(ctx context.Context, tx pgx.Tx, product, version string) (*servicebilling.LegalAcceptance, error) {
	// Share the activation lock, including when no policy has yet been activated.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-policy:"+product); err != nil {
		return nil, err
	}
	a := &servicebilling.LegalAcceptance{}
	var effective time.Time
	err := tx.QueryRow(ctx, `SELECT r.version,r.terms_text,r.privacy_text,r.content_sha256,r.effective_at FROM service_legal_current c JOIN service_legal_revisions r USING(product_code,version) WHERE c.product_code=$1`, product).Scan(&a.Version, &a.TermsText, &a.PrivacyText, &a.ContentSHA256, &effective)
	if errors.Is(err, pgx.ErrNoRows) {
		if version != "" {
			return nil, ErrServiceLegalReview
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if version != a.Version || effective.After(time.Now()) {
		return nil, ErrServiceLegalReview
	}
	a.AcceptedAt = time.Now().UTC().Truncate(time.Second)
	return a, nil
}
