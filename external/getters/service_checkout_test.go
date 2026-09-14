package getters

import "testing"

func TestSafeOpenNodeCheckout(t *testing.T) {
	id := "c9628482-2124-4e45-aa64-3708ccca8cdc"
	for _, tc := range []struct {
		base     string
		prod, ok bool
	}{
		{"https://checkout.opennode.com/", true, true},
		{"https://checkout.dev.opennode.com/", false, true},
		{"https://checkout.dev.opennode.com/", true, false},
		{"http://checkout.opennode.com/", false, false},
		{"https://checkout.opennode.com.evil.test/", false, false},
		{"https://evil.test@checkout.opennode.com/", false, false},
		{"https://checkout.opennode.com:443/", false, false},
		{"https://checkout.opennode.com/wrong/", false, false},
	} {
		_, err := safeOpenNodeHostedCheckout(tc.base+id, id, tc.prod)
		if (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
	}
	for _, suffix := range []string{"?redirect=https://evil.test", "#fragment", "/extra"} {
		if _, err := safeOpenNodeHostedCheckout("https://checkout.opennode.com/"+id+suffix, id, false); err == nil {
			t.Fatal("accepted", suffix)
		}
	}
}
