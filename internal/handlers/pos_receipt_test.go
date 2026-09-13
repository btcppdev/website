package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"html/template"
	"os"
	"strings"
	"testing"
)

func TestPOSReceipt(t *testing.T) {
	source, err := os.ReadFile("../../templates/shop/pos_receipt.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{TemplateCache: template.Must(template.New("shop/pos_receipt.tmpl").Parse(string(source)))}
	sale := &types.POSSale{ID: uuid.NewString(), Status: "paid", TotalSats: 50000, Currency: "EUR", LocalPerBTC: 80000, Items: []types.POSSaleItem{{Name: "Hat <script>", Label: "Rust", Quantity: 2, PriceSats: 25000}}}
	conf := &types.Conf{Location: "Berlin"}
	request := uuid.NewString()
	first, err := composePOSReceipt(app, conf, sale, "buyer@example.com", request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first.HTMLBody), "Hat &lt;script&gt;") || !strings.Contains(string(first.HTMLBody), "40.00 EUR") || !strings.Contains(string(first.TextBody), "50000 sats") {
		t.Fatal("wrong receipt content", string(first.HTMLBody))
	}
	again, _ := composePOSReceipt(app, conf, sale, "buyer@example.com", request)
	resend, _ := composePOSReceipt(app, conf, sale, "buyer@example.com", uuid.NewString())
	if first.JobKey != again.JobKey || first.JobKey == resend.JobKey {
		t.Fatal("retry/resend keys incorrect")
	}
	for _, email := range []string{"", "not-email", "A <a@example.com>", "a@example.com\r\nBcc: b@example.com"} {
		if _, err := composePOSReceipt(app, conf, sale, email, request); err == nil {
			t.Fatalf("accepted %q", email)
		}
	}
	for _, status := range []string{"creating", "pending", "expired", "cancelled", "review"} {
		sale.Status = status
		if _, err := composePOSReceipt(app, conf, sale, "buyer@example.com", request); err == nil {
			t.Fatalf("receipt for %s", status)
		}
	}
}
