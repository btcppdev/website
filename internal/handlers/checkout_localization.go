package handlers

import (
	"fmt"
	"net/http"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// The locale travels with each checkout URL rather than a global cookie, so
// English and Korean purchases in separate tabs cannot change each other.
func checkoutLocale(r *http.Request, conf *types.Conf) string {
	language := r.URL.Query().Get("lang")
	if conf != nil && language != "" && conferenceCatalog.Supports(conf.Tag, language) {
		return language
	}
	return "en"
}

func checkoutMessage(r *http.Request, conf *types.Conf, key string) string {
	message, err := conferenceCatalog.Message(checkoutLocale(r, conf), key)
	if err != nil {
		panic(err)
	} // A missing programmer-owned key is a catalog error.
	return message
}

func renderCheckout(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, name string, page any, conf *types.Conf) error {
	language := checkoutLocale(r, conf)
	w.Header().Set("Content-Language", language)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if language == "en" {
		return ctx.TemplateCache.ExecuteTemplate(w, name, page)
	}
	templates := ctx.LocalizedTemplates[language]
	if templates == nil {
		return fmt.Errorf("missing template cache for locale %q", language)
	}
	return templates.ExecuteTemplate(w, name, page)
}

// Dynamic browser messages are resolved server-side from the same catalogs.
// Only numeric/price placeholders are filled in by the checkout interaction.
func checkoutClientMessages(language string) (map[string]string, error) {
	out := map[string]string{}
	values := map[string]any{"Count": "{Count}", "Tickets": "{Tickets}", "Total": "{Total}"}
	for _, key := range []string{"register", "addons", "bitcoin_price", "card_price", "ticket_one", "ticket_many", "pass_one", "pass_many", "continue", "no_addons", "tax_none", "tax_loading", "tax_done", "tax_error"} {
		value, err := conferenceCatalog.Message(language, "checkout."+key, values)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}
