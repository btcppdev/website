package handlers

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

// AffiliateBuyerPctOptions are the slider stops the create + edit
// pages render. The fixed 20% ceiling means the affiliate's cut is
// always (20 - buyerPct) — see recordAffiliateUsageFromCheckout for
// the math.
var AffiliateBuyerPctOptions = []uint{0, 5, 10, 15, 20}

// AffiliatePage drives /dashboard/affiliate/new + /edit. The Form
// struct holds whatever the user typed so a re-render after a
// validation error doesn't lose their input. ConfNames is the
// human-readable list ("vienna, nairobi, …") of confs the new code
// will be wired to.
type AffiliatePage struct {
	Code         *types.DiscountCode
	IsEdit       bool
	IsLanding    bool
	BuyerPctOpts []uint
	ConfNames    []string
	Stats        *AffiliateStats
	CampaignRows []*AffiliateCampaignRow
	BaseURI      string
	Form         struct {
		CodeName string
		BuyerPct uint
	}
	FormError string
	Year      uint
}

// AffiliateLanding is the dedicated affiliate management page linked from the
// dashboard CTA. It shows the program explanation, current code, totals, and
// campaign rows in one place.
func AffiliateLanding(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, ok := affiliateAuthAndGate(w, r, ctx)
	if !ok {
		return
	}
	page := buildAffiliatePage(ctx, email, false, "")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_affiliate.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/affiliate render: %s", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// AffiliateNew renders the create-code form.
func AffiliateNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, ok := affiliateAuthAndGate(w, r, ctx)
	if !ok {
		return
	}
	// One-code-per-user: bounce to /edit when they already have one.
	if existing, _ := getters.FindAffiliateCodeByEmail(ctx, email); existing != nil {
		http.Redirect(w, r, "/dashboard/affiliate", http.StatusSeeOther)
		return
	}
	page := buildAffiliatePage(ctx, email, false, "")
	page.Form.BuyerPct = 10 // sensible default
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_affiliate.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/affiliate/new render: %s", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// AffiliateCreate validates + persists a new affiliate code.
func AffiliateCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, ok := affiliateAuthAndGate(w, r, ctx)
	if !ok {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	codeName := strings.ToUpper(strings.TrimSpace(r.FormValue("CodeName")))
	buyerPct := parseBuyerPct(r.FormValue("BuyerPct"))

	formErr := func(msg string) {
		page := buildAffiliatePage(ctx, email, false, msg)
		page.Form.CodeName = codeName
		page.Form.BuyerPct = buyerPct
		if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_affiliate.tmpl", page); err != nil {
			ctx.Err.Printf("/dashboard/affiliate/new re-render: %s", err)
		}
	}

	if codeName == "" {
		formErr("Pick a code name.")
		return
	}
	if !validCodeName(codeName) {
		formErr("Code names can only contain letters and numbers.")
		return
	}
	if existing, _ := getters.FindAffiliateCodeByEmail(ctx, email); existing != nil {
		formErr("You already have an affiliate code — edit it instead.")
		return
	}
	avail, err := getters.IsCodeNameAvailable(ctx, codeName)
	if err != nil {
		ctx.Err.Printf("/dashboard/affiliate/new uniqueness check: %s", err)
		formErr("Couldn't check uniqueness — try again.")
		return
	}
	if !avail {
		formErr("That code is already taken — pick another.")
		return
	}

	// Affiliate codes mint without any Conference relation —
	// CalcDiscount treats an empty ConfRef as "valid at any
	// active event," so a single user code works wherever they
	// share it without an admin re-attaching it per conf launch.
	if _, err := getters.CreateAffiliateCode(ctx, email, codeName, buyerPct, nil); err != nil {
		ctx.Err.Printf("/dashboard/affiliate/new create: %s", err)
		formErr("Couldn't save the code — try again.")
		return
	}
	http.Redirect(w, r,
		affiliateURLForEmail(ctx, email, "Affiliate code "+codeName+" created.", ""),
		http.StatusSeeOther)
}

// AffiliateEdit renders the edit-code form pre-filled with the
// user's current code.
func AffiliateEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, ok := affiliateAuthAndGate(w, r, ctx)
	if !ok {
		return
	}
	code, err := getters.FindAffiliateCodeByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/dashboard/affiliate/edit lookup: %s", err)
		http.Redirect(w, r, dashboardURLForEmail(ctx, email, "", "Couldn't load your code."), http.StatusSeeOther)
		return
	}
	if code == nil {
		http.Redirect(w, r, "/dashboard/affiliate/new", http.StatusSeeOther)
		return
	}
	page := buildAffiliatePage(ctx, email, true, "")
	page.Code = code
	page.Form.CodeName = code.CodeName
	page.Form.BuyerPct = code.Amount
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_affiliate.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/affiliate/edit render: %s", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// AffiliateUpdate persists the edit form.
func AffiliateUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, ok := affiliateAuthAndGate(w, r, ctx)
	if !ok {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code, err := getters.FindAffiliateCodeByEmail(ctx, email)
	if err != nil || code == nil {
		http.Redirect(w, r, dashboardURLForEmail(ctx, email, "", "Couldn't load your code."), http.StatusSeeOther)
		return
	}
	codeName := strings.ToUpper(strings.TrimSpace(r.FormValue("CodeName")))
	buyerPct := parseBuyerPct(r.FormValue("BuyerPct"))

	formErr := func(msg string) {
		page := buildAffiliatePage(ctx, email, true, msg)
		page.Code = code
		page.Form.CodeName = codeName
		page.Form.BuyerPct = buyerPct
		if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_affiliate.tmpl", page); err != nil {
			ctx.Err.Printf("/dashboard/affiliate/edit re-render: %s", err)
		}
	}

	if codeName == "" || !validCodeName(codeName) {
		formErr("Code names can only contain letters and numbers.")
		return
	}
	// Skip the uniqueness check when the name hasn't changed —
	// otherwise the user's own existing code would block them.
	if !strings.EqualFold(codeName, code.CodeName) {
		avail, err := getters.IsCodeNameAvailable(ctx, codeName)
		if err != nil {
			ctx.Err.Printf("/dashboard/affiliate/edit uniqueness check: %s", err)
			formErr("Couldn't check uniqueness — try again.")
			return
		}
		if !avail {
			formErr("That code is already taken — pick another.")
			return
		}
	}

	// Affiliate codes stay universal — clear any Conference
	// relation a previous form save (or admin edit) might have
	// left behind. Empty slice = wildcard at CalcDiscount time.
	if err := getters.UpdateAffiliateCode(ctx, code.Ref, codeName, buyerPct, nil); err != nil {
		ctx.Err.Printf("/dashboard/affiliate/edit update: %s", err)
		formErr("Couldn't save the change — try again.")
		return
	}
	http.Redirect(w, r,
		affiliateURLForEmail(ctx, email, "Affiliate code updated.", ""),
		http.StatusSeeOther)
}

// AffiliateDisable archives the user's code.
func AffiliateDisable(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, ok := affiliateAuthAndGate(w, r, ctx)
	if !ok {
		return
	}
	code, err := getters.FindAffiliateCodeByEmail(ctx, email)
	if err != nil || code == nil {
		http.Redirect(w, r, dashboardURLForEmail(ctx, email, "", "Couldn't find your code."), http.StatusSeeOther)
		return
	}
	if err := getters.ArchiveAffiliateCode(ctx, code.Ref); err != nil {
		ctx.Err.Printf("/dashboard/affiliate/disable archive: %s", err)
		http.Redirect(w, r, affiliateURLForEmail(ctx, email, "", "Couldn't disable the code."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r,
		affiliateURLForEmail(ctx, email, "Affiliate code "+code.CodeName+" disabled.", ""),
		http.StatusSeeOther)
}

func buildAffiliatePage(ctx *config.AppContext, email string, isEdit bool, formErr string) *AffiliatePage {
	code := loadAffiliateCode(ctx, email, true)
	page := &AffiliatePage{
		Code:         code,
		IsEdit:       isEdit,
		IsLanding:    !isEdit && code != nil,
		BuyerPctOpts: AffiliateBuyerPctOptions,
		ConfNames:    activeConfTagNames(ctx),
		Stats:        loadAffiliateStats(ctx, email, true),
		CampaignRows: affiliateCampaignRows(ctx, email),
		BaseURI:      ctx.Env.GetURI(),
		FormError:    formErr,
		Year:         helpers.CurrentYear(),
	}
	if code != nil {
		page.Form.CodeName = code.CodeName
		page.Form.BuyerPct = code.Amount
	} else {
		page.Form.BuyerPct = 10
	}
	return page
}

func affiliateCampaignRows(ctx *config.AppContext, email string) []*AffiliateCampaignRow {
	usages, err := getters.QueryAffiliateUsageByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/dashboard/affiliate usages %s: %s", email, err)
		return nil
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/dashboard/affiliate confs %s: %s", email, err)
	}
	confByTag := map[string]*types.Conf{}
	for _, conf := range confs {
		if conf != nil {
			confByTag[conf.Tag] = conf
		}
	}
	byTag := map[string]*AffiliateCampaignRow{}
	for _, usage := range usages {
		if usage == nil {
			continue
		}
		tag := strings.TrimSpace(usage.ConfTag)
		if tag == "" {
			tag = "all"
		}
		row := byTag[tag]
		if row == nil {
			conf := confByTag[tag]
			row = &AffiliateCampaignRow{
				Conf:     conf,
				CodeName: usage.CodeName,
				When:     "all editions",
				Tense:    "past",
			}
			if conf != nil {
				row.When = conf.DateDesc
				row.Tense = "past"
				if conf.InFuture() {
					row.Tense = "upcoming"
				}
			}
			byTag[tag] = row
		}
		if row.CodeName == "" {
			row.CodeName = usage.CodeName
		}
		row.TicketsSold += int(usage.TicketsCount)
		row.SavedSats += usage.SavedSats
		row.EarnedSats += usage.EarnedSats
	}
	rows := make([]*AffiliateCampaignRow, 0, len(byTag))
	for _, row := range byTag {
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if left.Conf != nil && right.Conf != nil && !left.Conf.StartDate.Equal(right.Conf.StartDate) {
			return left.Conf.StartDate.After(right.Conf.StartDate)
		}
		return affiliateCampaignTitle(left) < affiliateCampaignTitle(right)
	})
	return rows
}

func affiliateCampaignTitle(row *AffiliateCampaignRow) string {
	if row == nil {
		return ""
	}
	if row.Conf != nil {
		return row.Conf.ArchiveTitle()
	}
	if row.CodeName != "" {
		return row.CodeName
	}
	return "all editions"
}

// affiliateAuthAndGate resolves the authed email from the SCS
// session (set by the dashboard / magic-link flow) and confirms the
// user has at least one ticket on file. Returns ("", false) and
// writes a redirect when either gate fails.
func affiliateAuthAndGate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (string, bool) {
	email := ctx.Session.GetString(r.Context(), auth.SessionEmailKey)
	if email == "" {
		http.Redirect(w, r,
			"/login?next="+url.QueryEscape(r.URL.RequestURI()),
			http.StatusSeeOther)
		return "", false
	}
	regs, err := getters.ListRegistrationsByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/dashboard/affiliate gate check %s: %s", email, err)
		http.Redirect(w, r, dashboardURLForEmail(ctx, email, "", "Couldn't check your ticket history."), http.StatusSeeOther)
		return "", false
	}
	if len(regs) == 0 {
		http.Redirect(w, r,
			dashboardURLForEmail(ctx, email, "", "Affiliate codes are open to ticket holders. Buy a ticket first."),
			http.StatusSeeOther)
		return "", false
	}
	return email, true
}

// dashboardURLForEmail builds /dashboard?em=&hr=&flash=&error= for
// post-action redirects. The em+hr pair lets the dashboard handler's
// URL-auth path pick up the visitor without falling through to its
// session-based fallback (which works too, but landing with the
// canonical URL keeps every dashboard sub-link that hand-builds
// from .HMAC / .Email working without a re-mint).
func dashboardURLForEmail(ctx *config.AppContext, email, flash, errMsg string) string {
	q := url.Values{}
	if email != "" {
		q.Set("em", base64.RawURLEncoding.EncodeToString([]byte(email)))
		q.Set("hr", base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email))))
	}
	if flash != "" {
		q.Set("flash", flash)
	}
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	return "/dashboard?" + q.Encode()
}

func affiliateURLForEmail(ctx *config.AppContext, email, flash, errMsg string) string {
	u := dashboardURLForEmail(ctx, email, flash, errMsg)
	if strings.HasPrefix(u, "/dashboard?") {
		return "/dashboard/affiliate?" + strings.TrimPrefix(u, "/dashboard?")
	}
	return "/dashboard/affiliate"
}

// activeConfTagNames returns the tag names of every Active conf,
// used by the affiliate form to show the user what events their
// universal code currently applies to (e.g. "currently vienna,
// nairobi"). Affiliate codes mint without a Conference relation
// — see CalcDiscount's wildcard handling — so this list is purely
// informational, not a binding.
func activeConfTagNames(ctx *config.AppContext) []string {
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		return nil
	}
	var tags []string
	for _, c := range confs {
		if c != nil && c.Active {
			tags = append(tags, c.Tag)
		}
	}
	return tags
}

// parseBuyerPct clamps the form input to the allowed slider stops.
// Any unrecognized value falls back to 10 (a middling default the
// user can still see and adjust on a re-render).
func parseBuyerPct(raw string) uint {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 10
	}
	for _, opt := range AffiliateBuyerPctOptions {
		if uint(v) == opt {
			return opt
		}
	}
	return 10
}

// validCodeName: alphanumeric only. Keeps URL-safety + Notion-side
// readability simple. No length cap server-side; the client form
// caps at 32 for a sensible UX.
func validCodeName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		isLetter := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
}
