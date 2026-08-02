package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/missives"
	"github.com/gorilla/mux"
	stripe "github.com/stripe/stripe-go/v76"
)

func registerPublicRoutes(r *mux.Router, app *config.AppContext) {
	/* Handle 404s */
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle404(w, r, requestApp(r, app))
	})

	// Set up the routes, we'll have one page per course
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		RenderPage(w, r, requestApp(r, app), "index")
	}).Methods("GET")

	// SEO endpoints — robots policy at site root + dynamic sitemap
	// rebuilt from the confs cache on each request.
	r.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		Robots(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		Sitemap(w, r, requestApp(r, app))
	}).Methods("GET")

	/* List of 'normie' pages */
	for _, page := range pages {
		/* Normie Pages */
		renderPage := page
		r.HandleFunc("/"+renderPage, func(w http.ResponseWriter, r *http.Request) {
			RenderPage(w, r, requestApp(r, app), renderPage)
		}).Methods("GET")
	}

	/* Theme aliases — keyword URLs that map to a specific edition
	   ("ecash" was Berlin 24, "mempool" was ATX 25, etc.). Kept
	   alive for legacy share links and for vanity URLs that don't
	   match a conf tag. Self-aliases (berlin23 → /conf/berlin23,
	   etc.) used to live here too but now resolve natively via the
	   /{conf} catch-all registered near the end of this router. */
	r.HandleFunc("/ecash", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/berlin24", http.StatusMovedPermanently)
	}).Methods("GET")
	r.HandleFunc("/mempool", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/atx25", http.StatusMovedPermanently)
	}).Methods("GET")
	r.HandleFunc("/lightning", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/berlin25", http.StatusMovedPermanently)
	}).Methods("GET")
	r.HandleFunc("/exploits", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/floripa26", http.StatusMovedPermanently)
	}).Methods("GET")
	r.HandleFunc("/talks", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}).Methods("GET")
	/* /conf/* legacy paths — 301 to the new short form. Captures
	   /conf/{tag}, /conf/{tag}/talks, /conf/{tag}/talk/{anchor}/calendar.ics,
	   /conf/{tag}/success. The handler relocates the leading "/conf"
	   prefix; any preserved query string + hash fragment carries
	   through (the fragment never reaches the server but the
	   browser preserves it across a 301). */
	r.HandleFunc("/conf/{conf}", func(w http.ResponseWriter, r *http.Request) {
		redirectStripConfPrefix(w, r)
	}).Methods("GET")
	r.HandleFunc("/conf/{conf}/talks", func(w http.ResponseWriter, r *http.Request) {
		params := mux.Vars(r)
		redirectToConfAgenda(w, r, params["conf"])
	}).Methods("GET")
	r.HandleFunc("/conf/{conf}/talk/{anchor}/calendar.ics", func(w http.ResponseWriter, r *http.Request) {
		redirectStripConfPrefix(w, r)
	}).Methods("GET")
	r.HandleFunc("/conf/{conf}/success", func(w http.ResponseWriter, r *http.Request) {
		redirectStripConfPrefix(w, r)
	}).Methods("GET")

	r.HandleFunc("/volunteer", func(w http.ResponseWriter, r *http.Request) {
		RenderVolunteers(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/volunteer/{conf}", func(w http.ResponseWriter, r *http.Request) {
		RenderVolunteerConf(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/talk", func(w http.ResponseWriter, r *http.Request) {
		RenderSpeakers(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/talk/{conf}", func(w http.ResponseWriter, r *http.Request) {
		RenderSpeakerConf(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/whois", func(w http.ResponseWriter, r *http.Request) {
		RenderWhoIs(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/whois/{speaker}/archive", func(w http.ResponseWriter, r *http.Request) {
		RenderWhoIsArchive(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/whois/{speaker}", func(w http.ResponseWriter, r *http.Request) {
		RenderWhoIsProfile(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		ContactPage(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/sponsor", func(w http.ResponseWriter, r *http.Request) {
		SponsorPage(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/tix/{tix}/collect-email", func(w http.ResponseWriter, r *http.Request) {
		HandleCheckout(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/tix/{tix}/checkout", func(w http.ResponseWriter, r *http.Request) {
		HandleCheckout(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/tix/{tix}/apply-discount", func(w http.ResponseWriter, r *http.Request) {
		HandleDiscount(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/tix/{tix}/tax-quote", func(w http.ResponseWriter, r *http.Request) {
		TicketTaxQuote(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/conf-reload", func(w http.ResponseWriter, r *http.Request) {
		ReloadConf(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/check-in/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		CheckIn(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/check-in/{ticket}/pickups", func(w http.ResponseWriter, r *http.Request) {
		CheckInPickups(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/check-in/{ticket}/merch/{itemID}", func(w http.ResponseWriter, r *http.Request) {
		CheckInMerchPickup(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dev/check-in", func(w http.ResponseWriter, r *http.Request) {
		DevCheckInPreviewIndex(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dev/check-in/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		DevCheckInPreview(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/i/{conf}/sendcal", func(w http.ResponseWriter, r *http.Request) {
		if id := requireConfAdmin(w, r, requestApp(r, app)); id == nil {
			return
		}
		SendCals(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	AddMediaRoutes(r, app)

	r.HandleFunc("/ticket/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		Ticket(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/ticket/{ticket}/pdf", func(w http.ResponseWriter, r *http.Request) {
		TicketPDF(w, r, requestApp(r, app))
	}).Methods("GET")

	/* Register routes for newsletters */
	missives.RegisterNewsletterHandlers(r, app)
	emails.RegisterEndpoints(r, app)

	/* Setup stripe! */
	stripe.Key = app.Env.StripeKey
	r.HandleFunc("/callback/stripe", func(w http.ResponseWriter, r *http.Request) {
		StripeCallback(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/callback/opennode", func(w http.ResponseWriter, r *http.Request) {
		OpenNodeCallback(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/callbacks/easyship", func(w http.ResponseWriter, r *http.Request) {
		EasyshipCallback(w, r, requestApp(r, app))
	}).Methods("POST")
}
