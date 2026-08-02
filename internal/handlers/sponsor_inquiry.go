package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

type SponsorFormPage struct {
	Confs       []*types.Conf
	ConfItems   []types.CheckItem
	SponsorOpps []types.CheckItem
	Year        uint
}

func getSponsorOpps() []types.CheckItem {
	return []types.CheckItem{
		{ItemID: "opp-event", ItemDesc: "Event Sponsorship"},
		{ItemID: "opp-hackathon", ItemDesc: "Hackathon Sponsorship"},
		{ItemID: "opp-workshop", ItemDesc: "Workshop Sponsorship"},
		{ItemID: "opp-happy-hour", ItemDesc: "Happy Hour / After Party"},
		{ItemID: "opp-lanyard", ItemDesc: "Lanyards / Swag"},
		{ItemID: "opp-other", ItemDesc: "Other"},
	}
}

func SponsorPage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	confs := listConfs(w, ctx)
	if confs == nil {
		return
	}

	switch r.Method {
	case http.MethodGet:
		var confItems []types.CheckItem
		for _, conf := range confs {
			if !conf.Active || !conf.InFuture() {
				continue
			}
			confItems = append(confItems, types.CheckItem{
				ItemID:   "conf-" + conf.Ref,
				ItemDesc: conf.Desc + " " + conf.DateDesc,
			})
		}

		if err := ctx.TemplateCache.ExecuteTemplate(w, "embeds/sponsor.tmpl", &SponsorFormPage{
			Confs:       confs,
			ConfItems:   confItems,
			SponsorOpps: getSponsorOpps(),
			Year:        helpers.CurrentYear(),
		}); err != nil {
			http.Error(w, "Unable to load page", http.StatusInternalServerError)
			ctx.Err.Printf("/sponsor ExecuteTemplate failed: %s", err)
		}
		return

	case http.MethodPost:
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		name := strings.TrimSpace(r.FormValue("Name"))
		phone := strings.TrimSpace(r.FormValue("Phone"))
		email := strings.TrimSpace(r.FormValue("Email"))
		signal := strings.TrimSpace(r.FormValue("Signal"))
		telegram := strings.TrimSpace(r.FormValue("Telegram"))
		contactAt := strings.TrimSpace(r.FormValue("ContactAt"))
		org := strings.TrimSpace(r.FormValue("Org"))
		orgSite := strings.TrimSpace(r.FormValue("OrgSite"))
		orgTwitter := types.ParseTwitter(r.FormValue("OrgTwitter")).Handle
		orgNostr := strings.TrimSpace(r.FormValue("OrgNostr"))
		budget := strings.TrimSpace(r.FormValue("Budget"))
		discoveredVia := strings.TrimSpace(r.FormValue("DiscoveredVia"))
		comments := strings.TrimSpace(r.FormValue("Comments"))

		if strings.TrimSpace(r.FormValue("Captcha")) != "5" {
			_, _ = w.Write([]byte(helpers.ErrApp("Incorrect captcha. The answer is 5.", "sponsors")))
			return
		}
		if email == "" || !strings.Contains(email, "@") {
			_, _ = w.Write([]byte(helpers.ErrApp("Please provide a valid email address.", "sponsors")))
			return
		}
		if name == "" || org == "" {
			_, _ = w.Write([]byte(helpers.ErrApp("Name and organization are required.", "sponsors")))
			return
		}

		var selectedConfs []string
		for key := range r.PostForm {
			if !strings.HasPrefix(key, "conf-") {
				continue
			}
			confRef := strings.TrimPrefix(key, "conf-")
			for _, conf := range confs {
				if conf.Ref == confRef {
					selectedConfs = append(selectedConfs, conf.Desc+" "+conf.DateDesc)
					break
				}
			}
		}

		var selectedOpps []string
		for _, opp := range getSponsorOpps() {
			if r.FormValue(opp.ItemID) != "" {
				selectedOpps = append(selectedOpps, opp.ItemDesc)
			}
		}

		htmlBody := fmt.Sprintf(
			"<h3>Sponsor Inquiry</h3>"+
				"<p><strong>Name:</strong> %s</p>"+
				"<p><strong>Email:</strong> %s</p>"+
				"<p><strong>Phone:</strong> %s</p>"+
				"<p><strong>Signal:</strong> %s</p>"+
				"<p><strong>Telegram:</strong> %s</p>"+
				"<p><strong>Best way to contact:</strong> %s</p>"+
				"<hr/>"+
				"<p><strong>Organization:</strong> %s</p>"+
				"<p><strong>Website:</strong> %s</p>"+
				"<p><strong>X:</strong> %s</p>"+
				"<p><strong>Nostr:</strong> %s</p>"+
				"<hr/>"+
				"<p><strong>Budget:</strong> %s</p>"+
				"<p><strong>Events:</strong> %s</p>"+
				"<p><strong>Interested in:</strong> %s</p>"+
				"<p><strong>Discovered via:</strong> %s</p>"+
				"<hr/>"+
				"<p><strong>Comments:</strong></p><p>%s</p>",
			template.HTMLEscapeString(name), template.HTMLEscapeString(email), template.HTMLEscapeString(phone), template.HTMLEscapeString(signal), template.HTMLEscapeString(telegram), template.HTMLEscapeString(contactAt),
			template.HTMLEscapeString(org), template.HTMLEscapeString(orgSite), template.HTMLEscapeString(orgTwitter), template.HTMLEscapeString(orgNostr),
			template.HTMLEscapeString(budget), template.HTMLEscapeString(strings.Join(selectedConfs, ", ")), template.HTMLEscapeString(strings.Join(selectedOpps, ", ")),
			template.HTMLEscapeString(discoveredVia), template.HTMLEscapeString(comments))

		textBody := fmt.Sprintf(
			"Sponsor Inquiry\n\nName: %s\nEmail: %s\nPhone: %s\nSignal: %s\nTelegram: %s\nBest way to contact: %s\n\n"+
				"Organization: %s\nWebsite: %s\nX: %s\nNostr: %s\n\n"+
				"Budget: %s\nEvents: %s\nInterested in: %s\nDiscovered via: %s\n\nComments:\n%s",
			name, email, phone, signal, telegram, contactAt,
			org, orgSite, orgTwitter, orgNostr,
			budget, strings.Join(selectedConfs, ", "), strings.Join(selectedOpps, ", "),
			discoveredVia, comments)

		mail := &emails.Mail{
			JobKey:   fmt.Sprintf("sponsor-%s-%d", email, time.Now().Unix()),
			Email:    "sponsor@btcpp.dev",
			ReplyTo:  email,
			Title:    fmt.Sprintf("Sponsor Inquiry: %s (%s)", org, name),
			SendAt:   time.Now(),
			HTMLBody: []byte(htmlBody),
			TextBody: []byte(textBody),
		}
		if err := emails.ComposeAndSendMail(ctx, mail); err != nil {
			ctx.Err.Printf("/sponsor failed to send email: %s", err)
			_, _ = w.Write([]byte(helpers.ErrApp("Unable to send your inquiry. Please try again.", "sponsors")))
			return
		}

		copyMail := &emails.Mail{
			JobKey:   fmt.Sprintf("sponsor-copy-%s-%d", email, time.Now().Unix()),
			Email:    email,
			ReplyTo:  "sponsor@btcpp.dev",
			Title:    fmt.Sprintf("Your Sponsor Inquiry: %s", org),
			SendAt:   time.Now(),
			HTMLBody: []byte("<p>Thanks for your interest in sponsoring bitcoin++! Here's a copy of your inquiry:</p><hr/>" + htmlBody),
			TextBody: []byte("Thanks for your interest in sponsoring bitcoin++! Here's a copy of your inquiry:\n\n" + textBody),
		}
		if err := emails.ComposeAndSendMail(ctx, copyMail); err != nil {
			ctx.Err.Printf("/sponsor failed to send copy to %s: %s", email, err)
		}

		ctx.Infos.Printf("Sponsor inquiry from %s (%s) at %s", name, email, org)
		_, _ = w.Write([]byte(helpers.SuccessApp("Your sponsor inquiry has been sent! We'll get back to you soon.")))
	}
}
