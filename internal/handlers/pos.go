package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/skip2/go-qrcode"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type posItemEdit struct {
	Price, Transfer string
	Enabled         bool
}

type posPage struct {
	ReceiptEmail string
	Edits        map[string]*posItemEdit
	Audit        []getters.POSAuditEvent
	Conf         *types.Conf
	Admin        bool
	NeedsPIN     bool
	CSRF         string
	RequestID    string
	Products     []types.POSProduct
	Enabled      bool
	Currency     string
	Rate         float64
	Error        string
	Flash        string
	Sale         *types.POSSale
	Recent       []*types.POSSale
}

func registerPOSRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/{conf}/admin/merch-pos", func(w http.ResponseWriter, r *http.Request) { POSAdmin(w, r, app) }).Methods("GET", "POST")
	r.HandleFunc("/{conf}/merch/sell", func(w http.ResponseWriter, r *http.Request) { POSRegister(w, r, app) }).Methods("GET", "POST")
	r.HandleFunc("/{conf}/merch/sell/status/{sale}", func(w http.ResponseWriter, r *http.Request) { POSStatus(w, r, app) }).Methods("GET")
	r.HandleFunc("/{conf}/merch/sell/qr/{sale}", func(w http.ResponseWriter, r *http.Request) { POSQR(w, r, app) }).Methods("GET")
	r.HandleFunc("/callback/opennode-pos/{sale}", func(w http.ResponseWriter, r *http.Request) { POSCallback(w, r, app) }).Methods("POST")
}
func posCurrency(conf *types.Conf) (string, error) {
	currency := ""
	for _, t := range conf.Tickets {
		if t == nil {
			continue
		}
		c := strings.ToUpper(strings.TrimSpace(t.Currency))
		if c == "" || c == "BTC" || c == "SAT" || c == "SATS" {
			continue
		}
		if currency != "" && currency != c {
			return "", fmt.Errorf("ticket tiers use multiple local currencies; configure a consistent event currency")
		}
		currency = c
	}
	if currency == "" {
		return "", fmt.Errorf("configure the event ticket currency before opening the register")
	}
	return currency, nil
}
func posConf(w http.ResponseWriter, r *http.Request, app *config.AppContext) *types.Conf {
	c, err := getters.GetConfByTag(app, mux.Vars(r)["conf"])
	if err != nil || c == nil {
		http.NotFound(w, r)
		return nil
	}
	return c
}
func posRender(w http.ResponseWriter, app *config.AppContext, p *posPage) {
	for _, sale := range p.Recent {
		sale.CreatedAt = sale.CreatedAt.In(p.Conf.Loc())
	}
	for i := range p.Audit {
		p.Audit[i].CreatedAt = p.Audit[i].CreatedAt.In(p.Conf.Loc())
	}
	var b bytes.Buffer
	templateName := "pos.tmpl"
	if p.Admin {
		templateName = "admin/merch_pos.tmpl"
	}
	if err := app.TemplateCache.ExecuteTemplate(&b, templateName, p); err != nil {
		app.Err.Printf("POS template: %s", err)
		http.Error(w, "Unable to load register", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Write(b.Bytes())
}
func posPost(w http.ResponseWriter, r *http.Request, app *config.AppContext) bool {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
		http.Error(w, "Session expired. Reload the page.", 403)
		return false
	}
	return true
}
func posAuthorized(r *http.Request, app *config.AppContext) bool {
	return secureTokenEqual(app.Env.RegistryPin, app.Session.GetString(r.Context(), "pin"))
}
func posOperator(r *http.Request, app *config.AppContext) string {
	key := "pos_operator"
	id := app.Session.GetString(r.Context(), key)
	if id == "" {
		id = uuid.NewString()
		app.Session.Put(r.Context(), key, id)
	}
	return id
}

var posPINAttempts = struct {
	sync.Mutex
	entries map[string]struct {
		Count int
		Until time.Time
	}
}{entries: make(map[string]struct {
	Count int
	Until time.Time
})}

func posAllowPIN(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	posPINAttempts.Lock()
	defer posPINAttempts.Unlock()
	now := time.Now()
	for k, v := range posPINAttempts.entries {
		if now.After(v.Until) {
			delete(posPINAttempts.entries, k)
		}
	}
	a := posPINAttempts.entries[host]
	if a.Count >= 10 || len(posPINAttempts.entries) > 10000 {
		return false
	}
	if a.Count == 0 {
		a.Until = now.Add(time.Minute)
	}
	a.Count++
	posPINAttempts.entries[host] = a
	return true
}
func POSAdmin(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	id := requireConfAdmin(w, r, app)
	if id == nil {
		return
	}
	conf := posConf(w, r, app)
	if conf == nil {
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Session unavailable", 500)
		return
	}
	p := &posPage{Conf: conf, Admin: true, CSRF: csrf, RequestID: uuid.NewString(), Flash: r.URL.Query().Get("saved")}
	if r.Method == "POST" {
		if !posPost(w, r, app) {
			return
		}
		if r.PostForm.Get("action") == "open" {
			_, currencyErr := posCurrency(conf)
			if r.PostForm.Get("enabled") == "on" && currencyErr != nil {
				err = currencyErr
			} else {
				err = getters.POSSetEnabled(app, conf.Ref, id.Email, r.PostForm.Get("enabled") == "on")
			}
		}
		if r.PostForm.Get("action") == "items" {
			p.Edits = map[string]*posItemEdit{}
			var updates []getters.POSItemUpdate
			for _, variant := range r.PostForm["variant"] {
				edit := &posItemEdit{Price: r.PostForm.Get("price_" + variant), Transfer: r.PostForm.Get("transfer_" + variant), Enabled: r.PostForm.Get("enabled_"+variant) == "on"}
				p.Edits[variant] = edit
				price, e1 := strconv.ParseInt(edit.Price, 10, 64)
				transfer, e2 := strconv.Atoi(edit.Transfer)
				if e1 != nil || e2 != nil {
					err = fmt.Errorf("enter whole sats and whole stock transfers for every item")
				}
				updates = append(updates, getters.POSItemUpdate{VariantID: variant, Enabled: edit.Enabled, PriceSats: price, Transfer: transfer})
			}
			if err == nil {
				err = getters.POSConfigureItems(app, conf.Ref, id.Email, r.PostForm.Get("operation_id"), updates)
			}
		}
		if r.PostForm.Get("action") == "stock" {
			price, e1 := strconv.ParseInt(r.PostForm.Get("price"), 10, 64)
			transfer, e2 := strconv.Atoi(r.PostForm.Get("transfer"))
			variant := r.PostForm.Get("variant")
			if _, e := uuid.Parse(variant); e != nil || e1 != nil || e2 != nil {
				err = fmt.Errorf("enter whole sats and a whole stock transfer")
			} else {
				err = getters.POSConfigure(app, conf.Ref, variant, id.Email, r.PostForm.Get("operation_id"), r.PostForm.Get("enabled") == "on", price, transfer)
			}
		}
		if r.PostForm.Get("action") == "cancel" {
			if r.PostForm.Get("verified_no_charge") != "on" {
				err = fmt.Errorf("verify in OpenNode that no charge exists first")
			} else {
				saleID := r.PostForm.Get("sale")
				if _, e := uuid.Parse(saleID); e != nil {
					err = fmt.Errorf("invalid sale")
				} else {
					err = getters.POSCancelUnconfirmed(app, saleID, conf.Ref, id.Email)
				}
			}
		}
		if r.PostForm.Get("action") == "recover" {
			saleID := r.PostForm.Get("sale")
			if _, e := uuid.Parse(saleID); e != nil {
				err = fmt.Errorf("invalid sale")
			} else {
				var charge *getters.POSCharge
				charge, err = getters.POSOpenNodeCharge(app, strings.TrimSpace(r.PostForm.Get("charge_id")))
				if err == nil {
					err = getters.POSRecoverCharge(app, saleID, conf.Ref, id.Email, charge)
				}
			}
		}
		if err == nil {
			http.Redirect(w, r, r.URL.Path+"?saved=Changes+saved", 303)
			return
		}
		p.Error = err.Error()
	}
	p.Products, p.Enabled, err = getters.POSProducts(app, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load event stock", 500)
		return
	}
	if err = posProductImages(app, p.Products); err != nil {
		http.Error(w, "Unable to load product photos", 500)
		return
	}
	p.Currency, err = posCurrency(conf)
	if err != nil {
		p.Error = err.Error()
	} else {
		p.Rate, _ = getters.POSLocalRate(app, p.Currency)
	}
	p.Recent, err = getters.POSRecentSales(app, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load sales", 500)
		return
	}
	p.Audit, err = getters.POSAudit(app, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load audit history", 500)
		return
	}
	posRender(w, app, p)
}
func POSRegister(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	conf := posConf(w, r, app)
	if conf == nil {
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Session unavailable", 500)
		return
	}
	p := &posPage{Conf: conf, CSRF: csrf, RequestID: uuid.NewString()}
	if r.URL.Query().Get("receipt") == "queued" {
		p.Flash = "Receipt queued for email delivery."
	}
	if r.Method == "POST" {
		if !posPost(w, r, app) {
			return
		}
		action := r.PostForm.Get("action")
		if action == "unlock" {
			if !posAllowPIN(r.RemoteAddr) {
				http.Error(w, "Too many PIN attempts. Try again in a minute.", 429)
				return
			}
			if !secureTokenEqual(app.Env.RegistryPin, r.PostForm.Get("pin")) {
				p.Error = "Incorrect PIN"
			} else {
				if err := app.Session.RenewToken(r.Context()); err != nil {
					http.Error(w, "Session unavailable", 500)
					return
				}
				app.Session.Put(r.Context(), "pin", r.PostForm.Get("pin"))
				http.Redirect(w, r, r.URL.Path, 303)
				return
			}
		}
		if action == "lock" {
			app.Session.Remove(r.Context(), "pin")
			http.Redirect(w, r, r.URL.Path, 303)
			return
		}
		if action != "unlock" && !posAuthorized(r, app) {
			http.Error(w, "Unlock the register first", 401)
			return
		}
		if action == "checkout" {
			posCheckout(w, r, app, conf)
			return
		}
		if action == "receipt" {
			posEmailReceipt(w, r, app, conf, csrf)
			return
		}
		if action == "handover" {
			saleID := r.PostForm.Get("sale")
			if _, err := uuid.Parse(saleID); err != nil {
				http.Error(w, "Invalid sale", 400)
				return
			}
			if err := getters.POSHandOver(app, saleID, conf.Ref, posOperator(r, app)); err != nil {
				http.Error(w, "Unable to record handover", 500)
				return
			}
			http.Redirect(w, r, r.URL.Path, 303)
			return
		}
	}
	if !posAuthorized(r, app) {
		p.NeedsPIN = true
		posRender(w, app, p)
		return
	}
	posOperator(r, app)
	if saleID := r.URL.Query().Get("sale"); saleID != "" {
		if _, err := uuid.Parse(saleID); err != nil {
			http.NotFound(w, r)
			return
		}
		p.Sale, err = getters.POSGetSale(app, saleID, conf.Ref)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		posRender(w, app, p)
		return
	}
	p.Currency, err = posCurrency(conf)
	if err != nil {
		p.Error = err.Error()
	} else {
		p.Rate, err = getters.POSLocalRate(app, p.Currency)
		if err != nil {
			p.Error = "Local currency estimate unavailable. Please retry shortly."
		}
	}
	p.Products, p.Enabled, err = getters.POSProducts(app, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load stock", 500)
		return
	}
	if err = posProductImages(app, p.Products); err != nil {
		http.Error(w, "Unable to load product photos", 500)
		return
	}
	p.RequestID = uuid.NewString()
	p.Recent, err = getters.POSRecentSales(app, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load recent sales", 500)
		return
	}
	posRender(w, app, p)
}
func posCheckout(w http.ResponseWriter, r *http.Request, app *config.AppContext, conf *types.Conf) {
	currency, err := posCurrency(conf)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var lines []types.POSCartLine
	if err = json.Unmarshal([]byte(r.PostForm.Get("cart")), &lines); err != nil {
		http.Error(w, "Invalid cart", 400)
		return
	}
	rate, err := getters.POSLocalRate(app, currency)
	if err != nil {
		http.Error(w, "Currency estimate unavailable. Your cart has not been charged; go back and retry.", 503)
		return
	}
	s, created, err := getters.POSCreateSale(app, conf.Ref, posOperator(r, app), r.PostForm.Get("request_id"), currency, rate, lines)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	if created {
		payment, e := getters.POSOpenNodeCreate(app, s)
		if e == nil {
			e = getters.POSAttachCharge(app, s.ID, payment)
		}
		// An ambiguous provider failure must not generate a second invoice or release stock.
		if e != nil {
			app.Err.Printf("POS charge needs reconciliation sale=%s: %s", s.ID, e)
		}
	}
	http.Redirect(w, r, r.URL.Path+"?sale="+url.QueryEscape(s.ID), 303)
}
func posReadSale(w http.ResponseWriter, r *http.Request, app *config.AppContext) *types.POSSale {
	w.Header().Set("Cache-Control", "no-store")
	if !posAuthorized(r, app) {
		http.Error(w, "Register locked", 401)
		return nil
	}
	conf := posConf(w, r, app)
	if conf == nil {
		return nil
	}
	id := mux.Vars(r)["sale"]
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return nil
	}
	s, err := getters.POSGetSale(app, id, conf.Ref)
	if err != nil {
		http.NotFound(w, r)
		return nil
	}
	return s
}
func posReconcile(app *config.AppContext, s *types.POSSale) error {
	if s.ChargeID == "" || s.Status != "pending" {
		return nil
	}
	c, err := getters.POSOpenNodeCharge(app, s.ChargeID)
	if err != nil {
		return err
	}
	if c.OrderID != s.ID {
		return fmt.Errorf("charge order ID mismatch")
	}
	if _, err := app.DB.Exec(app.DatabaseContext(), `UPDATE conference_pos_sales SET checked_at=now() WHERE id=$1`, s.ID); err != nil {
		return err
	}
	return getters.POSApplyPayment(app, s.ID, c.ID, c.Status, c.Price)
}
func POSStatus(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	s := posReadSale(w, r, app)
	if s == nil {
		return
	}
	if err := posReconcile(app, s); err != nil {
		http.Error(w, "Checking payment is temporarily unavailable; do not charge again.", 503)
		return
	}
	s, err := getters.POSGetSale(app, s.ID, s.ConferenceID)
	if err != nil {
		http.Error(w, "Unable to load sale", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}
func POSQR(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	s := posReadSale(w, r, app)
	if s == nil {
		return
	}
	if s.Invoice == "" || s.Status != "pending" || s.ExpiresAt == nil || time.Now().After(*s.ExpiresAt) {
		http.Error(w, "Invoice unavailable", 409)
		return
	}
	png, err := qrcode.Encode("lightning:"+strings.ToUpper(s.Invoice), qrcode.Medium, 400)
	if err != nil {
		http.Error(w, "QR unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
func POSCallback(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	limitRequestBody(w, r, maxWebhookBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad callback", 400)
		return
	}
	id := r.PostForm.Get("id")
	saleID := mux.Vars(r)["sale"]
	if _, err := uuid.Parse(saleID); err != nil || id == "" || app.Env.OpenNode.Key == "" || !validHash(app.Env.OpenNode.Key, id, r.PostForm.Get("hashed_order")) {
		http.Error(w, "Invalid signature", 403)
		return
	}
	c, err := getters.POSOpenNodeCharge(app, id)
	if err != nil {
		http.Error(w, "Unable to verify payment", 503)
		return
	}
	if c.OrderID != saleID {
		http.Error(w, "Order mismatch", 400)
		return
	}
	if err = getters.POSApplyPayment(app, saleID, c.ID, c.Status, c.Price); err != nil {
		app.Err.Printf("POS callback sale=%s: %s", saleID, err)
		http.Error(w, "Unable to record payment", 503)
		return
	}
	w.WriteHeader(200)
}
func reconcilePOSPayments(app *config.AppContext) {
	rows, err := app.DB.Query(app.DatabaseContext(), `SELECT id::text,conference_id::text FROM conference_pos_sales WHERE status='pending' AND charge_id IS NOT NULL ORDER BY checked_at NULLS FIRST,created_at LIMIT 20`)
	if err != nil {
		app.Err.Printf("POS reconciliation: %s", err)
		return
	}
	var ids [][2]string
	for rows.Next() {
		var id [2]string
		if rows.Scan(&id[0], &id[1]) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s, err := getters.POSGetSale(app, id[0], id[1])
		if err == nil {
			err = posReconcile(app, s)
		}
		if err != nil {
			app.Err.Printf("POS reconcile sale=%s: %s", id[0], err)
			return
		}
	}
}

func posProductImages(app *config.AppContext, rows []types.POSProduct) error {
	products, err := getters.ListMerchProducts(app, true)
	if err != nil {
		return err
	}
	images := map[string]string{}
	for _, product := range products {
		for _, variant := range product.Variants {
			images[variant.ID] = merchImage(product)
		}
	}
	for i := range rows {
		rows[i].Image = images[rows[i].VariantID]
	}
	return nil
}
