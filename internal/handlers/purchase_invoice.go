package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

type purchaseInvoicePage struct {
	CSRF, Reference, Notice string
	Buyer                   getters.InvoiceBuyer
	Invoice                 *getters.PurchaseInvoice
}

func DashboardPurchaseInvoice(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	identity := requirePersonIdentity(w, r, app)
	if identity == nil {
		return
	}
	ref := mux.Vars(r)["order"]
	if ref == "" || len(ref) > 128 {
		http.NotFound(w, r)
		return
	}
	invoice, err := getters.PurchaseInvoiceForPerson(r.Context(), app, identity.PersonID, ref)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to load invoice", 503)
		return
	}
	page := purchaseInvoicePage{Reference: ref, Invoice: invoice}
	status := http.StatusOK
	if invoice == nil {
		order, err := getters.GetShopOrderByPublicID(app, ref)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		owns, err := getters.PersonOwnsShopOrder(app, identity.PersonID, order.ID)
		if err != nil || !owns {
			http.NotFound(w, r)
			return
		}
		if order.Status != "paid" && order.Status != "refunded" && order.Status != "partially_refunded" {
			http.Error(w, "Invoices are available after payment is confirmed.", 409)
			return
		}
		page.Buyer.Name = order.BuyerName
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid billing details", 400)
			return
		}
		if !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostFormValue("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		buyer := getters.InvoiceBuyer{Name: strings.TrimSpace(r.PostFormValue("name")), Address: strings.TrimSpace(r.PostFormValue("address")), City: strings.TrimSpace(r.PostFormValue("city")), Region: strings.TrimSpace(r.PostFormValue("region")), PostalCode: strings.TrimSpace(r.PostFormValue("postal")), Country: strings.TrimSpace(r.PostFormValue("country")), TaxID: strings.TrimSpace(r.PostFormValue("tax_id"))}
		invoice, err = getters.IssuePurchaseInvoice(r.Context(), app, identity.PersonID, ref, buyer)
		if err == nil {
			http.Redirect(w, r, "/dashboard/orders/"+url.PathEscape(ref)+"/invoice", http.StatusSeeOther)
			return
		}
		if errors.Is(err, getters.ErrInvoiceDetails) || errors.Is(err, getters.ErrInvoiceUnavailable) {
			page.Notice = err.Error()
			page.Buyer = buyer
			status = http.StatusUnprocessableEntity
		} else {
			http.Error(w, "Unable to issue invoice", 503)
			return
		}
	}
	if page.Invoice != nil && r.URL.Query().Get("download") == "pdf" {
		var html bytes.Buffer
		if err := app.TemplateCache.ExecuteTemplate(&html, "purchase_invoice_pdf.tmpl", page.Invoice); err != nil {
			http.Error(w, "Unable to render invoice", 500)
			return
		}
		file, err := os.CreateTemp("", "btcpp-invoice-*.html")
		if err != nil {
			http.Error(w, "Unable to generate invoice", 500)
			return
		}
		defer os.Remove(file.Name())
		_, err = file.Write(html.Bytes())
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			http.Error(w, "Unable to generate invoice", 500)
			return
		}
		pdf, err := helpers.BuildChromePdf(app, &helpers.PDFPage{URL: (&url.URL{Scheme: "file", Path: file.Name()}).String(), Width: 8.5, Height: 11})
		if err != nil {
			http.Error(w, "Unable to generate PDF. Please retry.", 503)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+page.Invoice.Number()+`.pdf"`)
		w.Write(pdf)
		return
	}
	page.CSRF, err = ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare invoice form", 500)
		return
	}
	var output bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&output, "purchase_invoice.tmpl", page); err != nil {
		app.Err.Printf("invoice template failed")
		http.Error(w, "Unable to show invoice", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(output.Bytes())
}
