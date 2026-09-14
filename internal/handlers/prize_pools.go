package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/prizepool"
	"btcpp-web/internal/types"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
	"github.com/skip2/go-qrcode"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type prizePoolPage struct {
	*HackathonAdminPage
	Payments                           []prizepool.Payment
	PaymentsTab                        bool
	Offset, NextOffset, PreviousOffset int
	HasNext                            bool
	Domain                             string
	Prizes                             []prizepool.Prize
	Conf                               *types.Conf
	Pool                               *prizepool.Pool
	Admin, Configured                  bool
	CSRF, Error                        string
}

func registerPrizePoolRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/{conf}/prize-pool/events", func(w http.ResponseWriter, r *http.Request) { communityPoolStream(w, r, app) }).Methods("GET")
	r.HandleFunc("/{conf}/prize-pool/status", func(w http.ResponseWriter, r *http.Request) { communityPoolStatus(w, r, app) }).Methods("GET")
	r.HandleFunc("/{conf}/prize-pool", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+mux.Vars(r)["conf"]+"/hackathon?prize=community#community-prize", http.StatusSeeOther)
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/prize-pool", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+mux.Vars(r)["conf"]+"/admin/hackathon/community-pool", http.StatusSeeOther)
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/community-pool", func(w http.ResponseWriter, r *http.Request) { HackathonAdminCommunityPool(w, r, app) }).Methods("GET", "POST")
	r.HandleFunc("/{conf}/prize-pool/qr", func(w http.ResponseWriter, r *http.Request) { prizePoolQR(w, r, app) }).Methods("GET")
}
func StartPrizePoolMonitor(app *config.AppContext) {
	s := prizepool.Environment()
	if !s.Enabled() {
		return
	}
	rpc := prizepool.Commando{Host: s.Host, NodeID: s.NodeID, Rune: s.Rune}
	go prizepool.Run(context.Background(), app.DB, rpc, s, app.Err)
}
func prizePoolHandler(w http.ResponseWriter, r *http.Request, app *config.AppContext, admin bool) {
	w.Header().Set("Cache-Control", "no-store")
	conf, err := getters.GetConfByTag(app, mux.Vars(r)["conf"])
	if err != nil || conf == nil {
		http.NotFound(w, r)
		return
	}
	actor := ""
	if admin {
		id := requireHackathonAdmin(w, r, app)
		if id == nil {
			return
		}
		actor = id.Email
	}
	s := prizepool.Environment()
	page := prizePoolPage{Conf: conf, Admin: admin, Configured: s.Enabled() && s.ProvisionRune != "" && s.CFToken != "" && s.CFZone != ""}
	competition, err := getters.GetCompetitionByConferenceID(app, conf.Ref)
	if err != nil || competition == nil {
		http.NotFound(w, r)
		return
	}
	page.HackathonAdminPage = &HackathonAdminPage{Conf: conf, Competition: competition, ActiveTab: "community", Year: helpers.CurrentYear()}
	populateAdminHackathonCounts(app, page.HackathonAdminPage)
	page.Domain = s.Domain
	page.PaymentsTab = r.URL.Query().Get("tab") == "payments"
	if admin {
		page.CSRF, err = ensureAuthMethodsCSRF(app, r)
		if err != nil {
			http.Error(w, "Unable to prepare form", 500)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil || !secureTokenEqual(page.CSRF, r.PostForm.Get("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		rpc := prizepool.Commando{Host: s.Host, NodeID: s.NodeID, Rune: s.ProvisionRune}
		switch r.PostForm.Get("action") {
		case "enable":
			if r.PostForm.Get("CommunityEnabled") != "yes" {
				err = errors.New("select Enable community prize pool")
				break
			}
			err = saveCommunitySetup(r, app, conf.Ref, actor)
		case "setup":
			if !page.Configured {
				err = errors.New("prize pool connection and DNS credentials are not configured")
				break
			}
			_, err = prizepool.Load(ctx, app.DB, conf.Ref)
			if err == nil {
				var dns prizepool.Publisher
				dns, err = prizepool.DNS(s)
				if err == nil {
					err = prizepool.Provision(ctx, app.DB, conf.Ref, actor, s, rpc, dns)
				}
			}
		case "link":
			err = prizepool.LinkPrize(ctx, app.DB, conf.Ref, r.PostForm.Get("prize"), actor, app.Env.GetURI())
		case "open":
			if r.PostForm.Get("verified") != "yes" {
				err = errors.New("verify the address and DNSSEC before opening funding")
			} else {
				err = prizepool.Open(ctx, app.DB, conf.Ref, actor)
			}
		case "close":
			if r.PostForm.Get("confirmed") != "yes" {
				err = errors.New("confirm that funding should close")
			} else if !s.Enabled() || s.ProvisionRune == "" {
				err = errors.New("CLN is not configured")
			} else {
				err = prizepool.VerifyNode(ctx, rpc, s)
				if err == nil {
					err = prizepool.ClosePool(ctx, app.DB, conf.Ref, actor, rpc)
				}
			}
		default:
			err = errors.New("unknown action")
		}
		if err != nil {
			app.Err.Printf("Prize pool action failed for %s: %v", conf.Tag, err)
			page.Error = "The action did not complete. Address registration and closing can be retried safely. Check the service log for details."
		} else {
			http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
			return
		}
	}
	page.Pool, err = prizepool.Load(ctx, app.DB, conf.Ref)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to load prize pool", 503)
		return
	}
	if !admin && (page.Pool == nil || page.Pool.Status == "draft") {
		http.NotFound(w, r)
		return
	}
	if admin && page.Pool != nil {
		page.Prizes, err = prizepool.ListPrizes(ctx, app.DB, conf.Ref)
		if err != nil {
			http.Error(w, "Unable to load hackathon prizes", 503)
			return
		}
	}
	if page.Pool != nil && page.PaymentsTab {
		page.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
		if page.Offset < 0 || page.Offset > 10000000 {
			page.Offset = 0
		}
		page.Payments, err = prizepool.ListPayments(ctx, app.DB, page.Pool.ID, page.Offset)
		if err != nil {
			http.Error(w, "Unable to load payments", 503)
			return
		}
		page.HasNext = len(page.Payments) > 50
		if page.HasNext {
			page.Payments = page.Payments[:50]
		}
		page.NextOffset = page.Offset + 50
		page.PreviousOffset = max(0, page.Offset-50)
	}
	var b bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&b, "prize_pool.tmpl", page); err != nil {
		app.Err.Printf("prize template: %v", err)
		http.Error(w, "Unable to render prize pool", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b.Bytes())
}
func prizePoolQR(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	conf, err := getters.GetConfByTag(app, mux.Vars(r)["conf"])
	if err != nil || conf == nil {
		http.NotFound(w, r)
		return
	}
	p, err := prizepool.Load(r.Context(), app.DB, conf.Ref)
	if err != nil || p.Status != "open" {
		http.NotFound(w, r)
		return
	}
	payload := p.Offer
	if r.URL.Query().Get("kind") == "address" {
		payload = p.Address()
	} else if !strings.HasPrefix(payload, "lno1") {
		http.NotFound(w, r)
		return
	}
	b, err := qrcode.Encode(payload, qrcode.Medium, 512)
	if err != nil {
		http.Error(w, "Unable to generate QR", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(b)
}

func publicCommunityPool(ctx context.Context, app *config.AppContext, conf *types.Conf) *prizepool.Pool {
	if app.DB == nil || conf == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	p, err := prizepool.Load(ctx, app.DB, conf.Ref)
	if err != nil || p.Status == "draft" {
		return nil
	}
	p.EventTag = conf.Tag
	return p
}
func communityPoolStatus(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	conf, err := getters.GetConfByTag(app, mux.Vars(r)["conf"])
	if err != nil {
		http.Error(w, "Funding updates unavailable", 503)
		return
	}
	if conf == nil || !conf.IsPublished() {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	p, err := prizepool.Load(ctx, app.DB, conf.Ref)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Funding updates unavailable", 503)
		return
	}
	if p.Status == "draft" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(communityStatusData(p))
}
func communitySocialCard(app *config.AppContext, conf *types.Conf, p *prizepool.Pool) imgproc.SiteSocialCard {
	card := hackathonSocialCard(app, &HackathonPage{Conf: conf})
	card.Kind = "community"
	card.Title = "Community prize"
	card.Eyebrow = conf.Tag
	card.ValueLabel = "Community-funded"
	card.Value = p.Sats()
	card.ValueSuffix = "sats"
	card.Callout = "Help reach " + p.GoalLabel() + " sats"
	card.Subtitle = p.Address()
	return normalizeSiteSocialCard(app, card)
}

func HackathonAdminCommunityPool(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	if requireHackathonAdmin(w, r, app) == nil {
		return
	}
	competition, err := getters.GetCompetitionByID(app, mux.Vars(r)["competitionID"])
	if err != nil || competition == nil {
		http.NotFound(w, r)
		return
	}
	conf, err := getters.GetConfByRef(app, competition.ConferenceID)
	if err != nil || conf == nil {
		http.NotFound(w, r)
		return
	}
	vars := mux.Vars(r)
	vars["conf"] = conf.Tag
	prizePoolHandler(w, mux.SetURLVars(r, vars), app, true)
}

func communitySetupValues(r *http.Request, conf *types.Conf) (string, string, error) {
	slug := strings.TrimSpace(r.PostFormValue("CommunitySlug"))
	if slug == "" {
		slug = conf.Tag
	}
	description := strings.TrimSpace(r.PostFormValue("CommunityDescription"))
	if description == "" {
		description = "bitcoin++ " + conf.Tag + " community prize pool"
	}
	return slug, description, prizepool.ValidateSetup(slug, description)
}
func saveCommunitySetup(r *http.Request, app *config.AppContext, confID, actor string) error {
	if r.PostFormValue("CommunityEnabled") != "yes" {
		return nil
	}
	conf, err := getters.GetConfByRef(app, confID)
	if err != nil {
		return err
	}
	slug, description, err := communitySetupValues(r, conf)
	if err != nil {
		return err
	}
	return prizepool.Create(r.Context(), app.DB, confID, slug, description, actor, prizepool.Environment())
}
