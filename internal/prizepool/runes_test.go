package prizepool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServiceRunesGrantOnlyRequiredMethods(t *testing.T) {
	monitor, provision := RuneCommands()
	for _, tc := range []struct {
		command string
		allowed []string
	}{
		{monitor, []string{"getinfo", "waitanyinvoice", "listinvoices"}},
		{provision, []string{"getinfo", "offer", "disableoffer", "clnurl-list", "clnurl-add", "clnurl-remove"}},
	} {
		payload, ok := strings.CutPrefix(tc.command, "lightning-cli createrune -k restrictions='")
		if !ok || !strings.HasSuffix(payload, "'") {
			t.Fatal("invalid shell command")
		}
		payload = strings.TrimSuffix(payload, "'")
		if strings.Contains(payload, "'") {
			t.Fatal("unescaped shell quote")
		}
		var restrictions [][]string
		if err := json.Unmarshal([]byte(payload), &restrictions); err != nil {
			t.Fatal(err)
		}
		// Separate arrays mean AND, which would prevent any method from working.
		if len(restrictions) != 1 || len(restrictions[0]) != len(tc.allowed) {
			t.Fatal("method permissions must be a single OR restriction")
		}
		permits := func(method string) bool {
			for _, rule := range restrictions[0] {
				if rule == "method="+method {
					return true
				}
			}
			return false
		}
		for _, method := range tc.allowed {
			if !permits(method) {
				t.Fatalf("required method %s denied", method)
			}
		}
		for _, method := range []string{"pay", "xpay", "withdraw", "sendpay", "sendinvoice", "keysend", "createrune", "listdatastore", "listinvoices-extra", "offer-extra"} {
			if permits(method) {
				t.Fatalf("unnecessary permission %s granted", method)
			}
		}
	}
}
