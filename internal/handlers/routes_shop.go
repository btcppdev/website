package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerShopRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/admin/merch", func(w http.ResponseWriter, r *http.Request) {
		AdminMerch(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/merch", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/new", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchNew(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/merch/orders", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchOrders(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/merch/orders/{order}", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchOrder(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/merch/orders/{order}/{action}", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchOrderAction(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/{id}", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchProduct(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/merch/{id}", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/{id}/upload-image", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchUploadImage(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/{id}/variants", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchVariantCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/{id}/variants/{variant}", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchVariantUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/{id}/images/{image}", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchImageUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/{id}/options", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchOptionSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/merch/{id}/options/{option}", func(w http.ResponseWriter, r *http.Request) {
		AdminMerchOptionSave(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/shop", func(w http.ResponseWriter, r *http.Request) {
		ShopHome(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/shop/all", func(w http.ResponseWriter, r *http.Request) {
		ShopCollection(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/shop/cart", func(w http.ResponseWriter, r *http.Request) {
		ShopCart(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/shop/cart/add", func(w http.ResponseWriter, r *http.Request) {
		ShopCartAdd(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/shop/cart", func(w http.ResponseWriter, r *http.Request) {
		ShopCartUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/shop/checkout", func(w http.ResponseWriter, r *http.Request) {
		ShopCheckout(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/shop/checkout", func(w http.ResponseWriter, r *http.Request) {
		ShopCheckoutCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/shop/shipping-rates", func(w http.ResponseWriter, r *http.Request) {
		ShopShippingRates(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/shop/tax-quote", func(w http.ResponseWriter, r *http.Request) {
		ShopTaxQuote(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/shop/checkout/cancel/{order}", func(w http.ResponseWriter, r *http.Request) {
		ShopCheckoutCancel(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/shop/success/{order}", func(w http.ResponseWriter, r *http.Request) {
		ShopSuccess(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/shop/{slug}", func(w http.ResponseWriter, r *http.Request) {
		ShopItem(w, r, requestApp(r, app))
	}).Methods("GET")
}
