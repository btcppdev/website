package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestServiceRefundOrderTemplate(t *testing.T) {
	funcs := template.FuncMap{"merchMoney": merchMoney, "shopFulfillmentLabel": shopFulfillmentLabel, "shopOrderHasFulfillment": shopOrderHasFulfillment, "shopOrderFulfillmentSummary": shopOrderFulfillmentSummary, "safeURL": func(s string) string { return s }}
	tmpl := template.Must(template.New("").Funcs(funcs).ParseFiles("../../templates/admin/merch_order.tmpl"))
	tmpl = template.Must(tmpl.Parse(`{{define "mainnav"}}{{end}}`))
	for _, amount := range []uint{0, 300, 500} {
		page := shopPage{Order: &types.ShopOrder{PublicID: "order-test", Status: "paid", TotalCents: 500, Items: []*types.ShopOrderItem{{ID: "item", Status: "fulfilled", Quantity: 1, LineTotalCents: 500}}}, ServiceRefunds: getters.ServiceOrderRefundSummary{IsService: true, RefundedCents: amount, RemainingCents: 500 - amount, AttemptID: "attempt-test"}}
		var b bytes.Buffer
		if err := tmpl.ExecuteTemplate(&b, "merch_order.tmpl", page); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		if strings.Contains(html, ">Issue refund<") || strings.Contains(html, "/order-test/refund") {
			t.Fatal("service order exposed merch refund")
		}
		for _, want := range []string{"/admin/service-refunds?attempt=attempt-test", "Refunded", "Not refunded", merchMoney(amount, nil)} {
			if !strings.Contains(html, want) {
				t.Fatal("missing", want)
			}
		}
	}
}
