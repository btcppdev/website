package handlers

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/bitcoinpayout"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
)

func TestClosureReviewInputAndAuthorization(t *testing.T) {
	for _, role := range []string{"", "nairobi-admin", "global-speaker"} {
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			serveAdminServiceClosure(w, httptest.NewRequest(method, "/admin/service-closure", nil), &config.AppContext{}, &auth.Identity{Roles: auth.ParseRoles([]string{role})})
			if w.Code != 403 {
				t.Fatal("scoped role authorized", w.Code)
			}
		}
	}
	for _, form := range []string{"", "csrf=wrong", "csrf=expected&csrf=expected"} {
		manager := scs.New()
		r := httptest.NewRequest("POST", "/admin/service-closure", strings.NewReader(form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx, err := manager.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		manager.Put(ctx, authMethodsCSRFKey, "expected")
		w := httptest.NewRecorder()
		serveAdminServiceClosure(w, r.WithContext(ctx), &config.AppContext{Session: manager}, &auth.Identity{Roles: auth.ParseRoles([]string{"global-admin"})})
		if w.Code != 403 {
			t.Fatal("bad CSRF reached database", w.Code)
		}
	}
	const attempt = "00000000-0000-0000-0000-000000000001"
	for _, raw := range []string{"", "2030-01-01", "2030-01-01T00:00:00", "2030-01-01T00:00:00.000000001Z"} {
		if _, _, err := parseClosureInputs(attempt, raw); err == nil {
			t.Fatal("ambiguous or unsupported precision", raw)
		}
	}
	_, when, err := parseClosureInputs(attempt, "2030-01-01T02:00:00+02:00")
	if err != nil || when.Hour() != 0 {
		t.Fatal("timezone not normalized", when, err)
	}
}

func TestClosureReviewTemplateStates(t *testing.T) {
	raw, err := os.ReadFile("../../templates/admin/service_closure.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("closure").Parse(`{{define "mainnav"}}{{end}}` + string(raw))
	if err != nil {
		t.Fatal(err)
	}
	amount := int64(12345)
	for _, mode := range []string{"preview", "blocked", "saved"} {
		page := &serviceClosurePage{CSRF: "test-csrf", Plan: &getters.BitcoinClosurePlan{RemainingRefundSatoshis: &amount}, CanReview: mode == "preview"}
		if mode == "blocked" {
			page.Plan.RemainingRefundSatoshis = nil
			page.Plan.ReviewReasons = []string{"unknown payment evidence"}
		}
		if mode == "saved" {
			page.Review = &getters.BitcoinClosureReview{Reason: "<script>unsafe</script>"}
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, page); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		if strings.Contains(html, "Record calculation review") != (mode == "preview") {
			t.Fatal("wrong approval state", mode)
		}
		if mode == "preview" && (!strings.Contains(html, `value="12345"`) || !strings.Contains(html, "test-csrf")) {
			t.Fatal("missing amount or token")
		}
		if mode == "blocked" && !strings.Contains(html, "unknown payment evidence") {
			t.Fatal("missing review reason")
		}
		if mode == "saved" && (!strings.Contains(html, "&lt;script&gt;") || strings.Contains(html, "<script>unsafe")) {
			t.Fatal("unsafe audit reason")
		}
	}
}

func TestClosurePayoutConfiguration(t *testing.T) {
	t.Setenv("BITCOIN_RPC_URL", "http://127.0.0.1:18443")
	t.Setenv("BITCOIN_RPC_USER", "fixture")
	t.Setenv("BITCOIN_RPC_PASSWORD", "fixture")
	t.Setenv("BITCOIN_RPC_CHAIN", "regtest")
	if _, err := closureBitcoinNode(&config.AppContext{Env: &types.EnvConfig{Prod: true}}); err == nil {
		t.Fatal("production accepted regtest")
	}
	if _, err := closureBitcoinNode(&config.AppContext{}); err == nil {
		t.Fatal("unknown environment accepted regtest")
	}
	if _, err := closureBitcoinNode(&config.AppContext{Env: &types.EnvConfig{}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BITCOIN_RPC_CHAIN", "")
	if _, err := closureBitcoinNode(&config.AppContext{Env: &types.EnvConfig{Prod: true}}); err != nil {
		t.Fatal("default mainnet", err)
	}
	t.Setenv("BITCOIN_RPC_PASSWORD", "")
	if _, err := closureBitcoinNode(&config.AppContext{}); err == nil {
		t.Fatal("missing credentials accepted")
	}
}

func TestClosurePayoutTemplateStates(t *testing.T) {
	raw, err := os.ReadFile("../../templates/admin/service_closure.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("closure").Parse(`{{define "mainnav"}}{{end}}` + string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"destination", "payout", "recorded", "stale", "unconfigured"} {
		page := &serviceClosurePage{Plan: &getters.BitcoinClosurePlan{}, Review: &getters.BitcoinClosureReview{ID: "review"}, CurrentBasis: mode != "stale", NodeConfigured: mode != "unconfigured"}
		if mode == "payout" || mode == "recorded" {
			page.Destination = &getters.BitcoinClosureDestination{Destination: bitcoinpayout.Destination{Address: "<script>address</script>", Chain: "main"}}
		}
		if mode == "recorded" {
			page.Payout = &getters.BitcoinClosurePayout{Evidence: &bitcoinpayout.Evidence{}}
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, page); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		if strings.Contains(html, "Verify and approve address") != (mode == "destination") {
			t.Fatal("wrong destination form", mode)
		}
		if strings.Contains(html, "Verify and record payout") != (mode == "payout") {
			t.Fatal("wrong payout form", mode)
		}
		if strings.Contains(html, "<script>address") {
			t.Fatal("unescaped address")
		}
		if mode == "recorded" && !strings.Contains(html, "Payout recorded") {
			t.Fatal("missing recorded evidence")
		}
	}
}

func TestClosurePayoutRequiresConfirmation(t *testing.T) {
	for _, action := range []string{"destination", "payout", "reconcile", "unknown"} {
		manager := scs.New()
		form := "csrf=expected&action=" + action + "&review=00000000-0000-0000-0000-000000000001&address=address&reason=reason&txid=txid&block=block&vout=0"
		r := httptest.NewRequest("POST", "/admin/service-closure", strings.NewReader(form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx, err := manager.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		manager.Put(ctx, authMethodsCSRFKey, "expected")
		w := httptest.NewRecorder()
		serveAdminServiceClosure(w, r.WithContext(ctx), &config.AppContext{Session: manager}, &auth.Identity{Roles: auth.ParseRoles([]string{"global-admin"})})
		if w.Code != 400 {
			t.Fatal("unconfirmed action reached configuration or database", action, w.Code)
		}
	}
}
