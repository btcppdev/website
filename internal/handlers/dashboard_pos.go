package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"net/http"
)

func DashboardPOSReceipt(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	id, err := auth.Resolve(r, app)
	if err != nil || id == nil || id.PersonID == "" {
		http.NotFound(w, r)
		return
	}
	saleID := mux.Vars(r)["sale"]
	if _, err := uuid.Parse(saleID); err != nil {
		http.NotFound(w, r)
		return
	}
	sale, event, err := getters.POSPurchaseForPerson(app, id.PersonID, saleID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var html bytes.Buffer
	data := posReceiptData(&types.Conf{Location: event}, sale)
	data.AccountView = true
	if err := app.TemplateCache.ExecuteTemplate(&html, "shop/pos_receipt.tmpl", data); err != nil {
		http.Error(w, "Unable to load receipt", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}
