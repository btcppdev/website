package handlers

import (
	"btcpp-web/external/coingecko"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cache failures briefly as well as successful quotes so a catalog containing
// multiple products never repeats a failed upstream request for every price.
type merchExchangeRate struct {
	mu       sync.Mutex
	fetch    func(string) (float64, error)
	until    time.Time
	usd      float64
	currency string
}

var shopRate = &merchExchangeRate{fetch: coingecko.FreshBitcoinPrice}

func (rate *merchExchangeRate) sats(cents uint) string {
	rate.mu.Lock()
	defer rate.mu.Unlock()
	if time.Now().After(rate.until) {
		value, err := rate.fetch(rate.currencyCode())
		rate.usd = 0
		rate.until = time.Now().Add(time.Minute)
		if err == nil && value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			rate.usd = value
			rate.until = time.Now().Add(5 * time.Minute)
		}
	}
	if rate.usd == 0 {
		return ""
	}
	return strconv.FormatInt(int64(math.Round(float64(cents)*1e3/rate.usd)), 10) + "k"
}

func (rate *merchExchangeRate) currencyCode() string {
	if rate.currency == "" {
		return "usd"
	}
	return rate.currency
}

// Checkout asks for a fresh quote instead of keeping the cart's display quote.
func (rate *merchExchangeRate) refresh() {
	rate.mu.Lock()
	rate.until = time.Time{}
	rate.mu.Unlock()
	rate.sats(0)
}

var shopCurrencyRates sync.Map

func merchSats(cents uint, currency ...string) string {
	code := "usd"
	if len(currency) > 0 && currency[0] != "" {
		code = strings.ToLower(currency[0])
	}
	if code == "usd" {
		return shopRate.sats(cents)
	}
	rate, _ := shopCurrencyRates.LoadOrStore(code, &merchExchangeRate{currency: code, fetch: coingecko.FreshBitcoinPrice})
	return rate.(*merchExchangeRate).sats(cents)
}

func merchUSDQuote() float64 {
	shopRate.sats(0)
	shopRate.mu.Lock()
	defer shopRate.mu.Unlock()
	return shopRate.usd
}
