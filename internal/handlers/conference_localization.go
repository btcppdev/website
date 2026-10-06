package handlers

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"

	"btcpp-web/internal/config"
	"btcpp-web/internal/i18n"
)

var conferenceCatalog = mustConferenceCatalog()

func mustConferenceCatalog() *i18n.Catalog {
	c, err := i18n.Load()
	if err != nil {
		panic(err)
	}
	return c
}

func localeTemplateFunctions(language string) template.FuncMap {
	return template.FuncMap{
		"checkoutURL":      func(path string) string { return i18n.CheckoutURL(language, path) },
		"checkoutMessages": func() (map[string]string, error) { return checkoutClientMessages(language) },
		"locale":           func() string { return language },
		"msg": func(key string, args ...map[string]any) (string, error) {
			return conferenceCatalog.Message(language, key, args...)
		},
		"localeURL": func(path string) string { return conferenceCatalog.URL(language, path) },
		"languages": func(event string) []i18n.LanguageLink { return conferenceCatalog.Languages(event, language) },
		"eventText": func(event, source string) string { return conferenceCatalog.Content(event, language, source) },
	}
}

func renderLocalizedConference(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, name string, page *ConfPage) error {
	language := mux.Vars(r)["locale"]
	if language == "" {
		language = "en"
	}
	if !conferenceCatalog.Supports(page.Conf.Tag, language) {
		http.NotFound(w, r)
		return nil
	}
	w.Header().Set("Content-Language", language)
	if language == "en" {
		return ctx.TemplateCache.ExecuteTemplate(w, name, page)
	}
	local := *page
	conf := *page.Conf
	local.Conf = &conf
	// Localize a request-owned copy. Cached event, inventory, and identity
	// records are never rewritten by the language selected for rendering.
	text := func(s string) string { return conferenceCatalog.Content(conf.Tag, language, s) }
	for _, field := range []*string{&conf.Desc, &conf.Tagline, &conf.DateDesc, &conf.Location, &conf.Venue, &conf.OGFlavor, &conf.HeroTitle, &conf.HeroCaption, &conf.AboutTitle, &conf.AboutBody, &conf.AboutBody2, &conf.VenueTitle, &conf.VenueSubtitle, &conf.VenueBody, &conf.HotelsIntro, &conf.LocalTicketBody, &conf.SpeakersTitle, &conf.SpeakersBody, &conf.HackathonSectionLabel, &conf.HackathonHeadline, &conf.HackathonJudgesNote, &conf.HackathonProofLabel} {
		*field = text(*field)
	}
	if page.Hackathon != nil {
		v := *page.Hackathon
		v.Title = text(v.Title)
		v.Description = text(v.Description)
		local.Hackathon = &v
	}
	local.Hotels = append(local.Hotels[:0:0], page.Hotels...)
	for i, h := range page.Hotels {
		if h != nil {
			v := *h
			v.Desc = text(v.Desc)
			v.Type = text(v.Type)
			local.Hotels[i] = &v
		}
	}
	local.SatelliteEvents = append(local.SatelliteEvents[:0:0], page.SatelliteEvents...)
	for i, e := range page.SatelliteEvents {
		if e != nil {
			v := *e
			v.Title = text(v.Title)
			v.Description = text(v.Description)
			v.EventType = text(v.EventType)
			local.SatelliteEvents[i] = &v
		}
	}
	local.ImportantDates = append(local.ImportantDates[:0:0], page.ImportantDates...)
	loc := conf.Loc()
	dateLayout, err := conferenceCatalog.Message(language, "dates.date_layout")
	if err != nil {
		return err
	}
	timeLayout, err := conferenceCatalog.Message(language, "dates.time_layout")
	if err != nil {
		return err
	}
	for i, d := range page.ImportantDates {
		if d != nil {
			v := *d
			v.Label = text(v.Label)
			v.Detail = text(v.Detail)
			v.Status = text(v.Status)
			v.DateLabel = v.OccursAt.In(loc).Format(dateLayout)
			v.TimeLabel = v.OccursAt.In(loc).Format(timeLayout)
			local.ImportantDates[i] = &v
		}
	}
	templates := ctx.LocalizedTemplates[language]
	if templates == nil {
		return fmt.Errorf("missing template cache for locale %q", language)
	}
	return templates.ExecuteTemplate(w, name, &local)
}
