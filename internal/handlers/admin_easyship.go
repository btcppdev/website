package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

type AdminEasyshipPage struct {
	Settings                *types.EasyshipSettings
	WebhookEvents           []*types.EasyshipWebhookEvent
	APIKeyConfigured        bool
	WebhookSecretConfigured bool
	Endpoint                string
	APIVersion              string
	Flash                   string
	Error                   string
	Year                    uint
}

func AdminEasyship(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	settings, err := getters.GetEasyshipSettings(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/easyship settings: %s", err)
		http.Error(w, "Unable to load Easyship settings", http.StatusInternalServerError)
		return
	}
	page := adminEasyshipPage(ctx, settings)
	page.WebhookEvents, err = getters.ListEasyshipWebhookEvents(ctx, 25)
	if err != nil {
		ctx.Err.Printf("/admin/easyship webhook events: %s", err)
		page.Error = "Unable to load recent webhook events."
	}
	page.Flash = r.URL.Query().Get("flash")
	if queryError := r.URL.Query().Get("err"); queryError != "" {
		page.Error = queryError
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/easyship.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/easyship template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func AdminEasyshipSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	settings := &types.EasyshipSettings{
		ContactName:   r.FormValue("contact_name"),
		CompanyName:   r.FormValue("company_name"),
		Email:         r.FormValue("email"),
		Phone:         r.FormValue("phone"),
		Line1:         r.FormValue("line_1"),
		Line2:         r.FormValue("line_2"),
		City:          r.FormValue("city"),
		Region:        r.FormValue("region"),
		PostalCode:    r.FormValue("postal_code"),
		CountryAlpha2: r.FormValue("country_alpha2"),
	}
	if err := getters.SaveEasyshipSettings(ctx, settings, id.Email); err != nil {
		ctx.Err.Printf("/admin/easyship save: %s", err)
		http.Redirect(w, r, "/admin/easyship?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/easyship?flash="+url.QueryEscape("Easyship origin updated."), http.StatusSeeOther)
}

func adminEasyshipPage(ctx *config.AppContext, settings *types.EasyshipSettings) *AdminEasyshipPage {
	page := &AdminEasyshipPage{Settings: settings, Year: helpers.CurrentYear()}
	if ctx != nil && ctx.Env != nil {
		page.APIKeyConfigured = strings.TrimSpace(ctx.Env.Easyship.APIKey) != ""
		page.WebhookSecretConfigured = strings.TrimSpace(ctx.Env.Easyship.WebhookSecret) != ""
		page.Endpoint = ctx.Env.Easyship.Endpoint
		page.APIVersion = ctx.Env.Easyship.APIVersion
	}
	return page
}
