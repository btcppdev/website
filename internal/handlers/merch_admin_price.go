package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Parse decimal currency exactly; never silently truncate fractional cents.
func merchAdminCents(raw string, signed bool) (int64, error) {
	raw = strings.TrimSpace(raw)
	negative := strings.HasPrefix(raw, "-")
	if negative {
		if !signed {
			return 0, fmt.Errorf("price must not be negative")
		}
		raw = strings.TrimPrefix(raw, "-")
	}
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("enter a price with at most two decimal places")
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 {
			return 0, fmt.Errorf("enter a price with at most two decimal places")
		}
		fraction = (parts[1] + "0")[:2]
	}
	for _, c := range parts[0] + fraction {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("enter a numeric price")
		}
	}
	value, err := strconv.ParseInt(parts[0]+fraction, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("price is too large")
	}
	if negative {
		value = -value
	}
	return value, nil
}

func normalizeMerchAdminPrices(r *http.Request) error {
	for _, f := range []struct {
		display, stored string
		signed          bool
	}{{"base_price", "base_price_cents", false}, {"price_adjustment", "price_delta_cents", true}} {
		if _, ok := r.PostForm[f.display]; !ok {
			continue
		}
		cents, err := merchAdminCents(r.PostForm.Get(f.display), f.signed)
		if err != nil {
			return err
		}
		r.Form.Set(f.stored, strconv.FormatInt(cents, 10))
	}
	return nil
}

func merchDecimal(cents interface{}) string {
	var value int64
	switch n := cents.(type) {
	case uint:
		value = int64(n)
	case int:
		value = int64(n)
	case int64:
		value = n
	default:
		return "0.00"
	}
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	return fmt.Sprintf("%s%d.%02d", sign, value/100, value%100)
}
