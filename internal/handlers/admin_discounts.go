package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

type AdminDiscountsPage struct {
	Conf      *types.Conf
	Discounts []AdminDiscountRow
	Form      DiscountForm
	Flash     string
	FlashErr  string
	Year      uint
}

type AdminDiscountRow struct {
	ID             string
	CodeName       string
	Expression     string
	AmountLabel    string
	ValidDates     string
	UsesLabel      string
	AffiliateEmail string
	Form           DiscountForm
}

type DiscountForm struct {
	ID             string
	CodeName       string
	DiscountType   string
	Amount         string
	ValidFrom      string
	ExpiresAt      string
	MaxAllowed     string
	AffiliateEmail string
}

type GlobalAdminDiscountsPage struct {
	Confs                  []*types.Conf
	Discounts              []GlobalAdminDiscountRow
	Form                   GlobalDiscountForm
	SelectedConferenceRefs map[string]bool
	Flash                  string
	FlashErr               string
	Year                   uint
}

type GlobalDiscountForm struct {
	DiscountForm
	ConferenceRefs  []string
	ConferenceScope string
}

type GlobalAdminDiscountRow struct {
	AdminDiscountRow
	Conferences []*types.Conf
}

func AdminDiscounts(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil || conf == nil {
		handle404(w, r, ctx)
		return
	}

	page := &AdminDiscountsPage{
		Conf:      conf,
		Discounts: discountsForConf(ctx, conf),
		Flash:     r.URL.Query().Get("flash"),
		Year:      helpers.CurrentYear(),
	}

	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			page.FlashErr = "Couldn't read form. Try again."
			renderAdminDiscounts(w, r, ctx, page)
			return
		}
		action := strings.TrimSpace(r.PostForm.Get("action"))
		if action == "" {
			action = "create"
		}
		page.Form = discountFormFromRequest(r)
		if action == "delete" {
			if err := deleteAdminDiscount(ctx, conf, page.Form.ID); err != nil {
				ctx.Err.Printf("/%s/admin/discounts delete %s: %s", conf.Tag, page.Form.ID, err)
				page.FlashErr = err.Error()
				renderAdminDiscounts(w, r, ctx, page)
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/discounts?flash=%s", conf.Tag, url.QueryEscape("Deleted discount code.")), http.StatusSeeOther)
			return
		}
		expr, err := buildDiscountExpr(page.Form)
		if err != nil {
			page.FlashErr = err.Error()
			renderAdminDiscounts(w, r, ctx, page)
			return
		}

		switch action {
		case "create":
			if available, err := getters.IsCodeNameAvailable(ctx, page.Form.CodeName); err != nil {
				ctx.Err.Printf("/%s/admin/discounts availability %s: %s", conf.Tag, page.Form.CodeName, err)
				page.FlashErr = "Couldn't check whether that code already exists."
				renderAdminDiscounts(w, r, ctx, page)
				return
			} else if !available {
				page.FlashErr = "That code already exists. Pick another code name."
				renderAdminDiscounts(w, r, ctx, page)
				return
			}

			_, err = getters.CreateDiscount(ctx, getters.DiscountInput{
				CodeName:       strings.ToUpper(page.Form.CodeName),
				DiscountExpr:   expr,
				ConfRef:        conf.Ref,
				AffiliateEmail: page.Form.AffiliateEmail,
			})
			if err != nil {
				ctx.Err.Printf("/%s/admin/discounts create %s (%s): %s", conf.Tag, page.Form.CodeName, expr, err)
				page.FlashErr = "Creating the discount failed. Check server logs."
				renderAdminDiscounts(w, r, ctx, page)
				return
			}

			flash := fmt.Sprintf("Created %s for %s.", strings.ToUpper(page.Form.CodeName), conf.Desc)
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/discounts?flash=%s", conf.Tag, url.QueryEscape(flash)), http.StatusSeeOther)
			return

		case "update":
			if err := updateAdminDiscount(ctx, conf, page.Form, expr); err != nil {
				ctx.Err.Printf("/%s/admin/discounts update %s (%s): %s", conf.Tag, page.Form.ID, expr, err)
				page.FlashErr = err.Error()
				renderAdminDiscounts(w, r, ctx, page)
				return
			}
			flash := fmt.Sprintf("Updated %s.", strings.ToUpper(page.Form.CodeName))
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/discounts?flash=%s", conf.Tag, url.QueryEscape(flash)), http.StatusSeeOther)
			return

		default:
			page.FlashErr = "Unknown discount action."
			renderAdminDiscounts(w, r, ctx, page)
			return
		}
	}

	renderAdminDiscounts(w, r, ctx, page)
}

func GlobalAdminDiscounts(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/discounts conferences: %s", err)
		http.Error(w, "Unable to load discounts", http.StatusInternalServerError)
		return
	}
	sortGlobalDiscountConfs(confs)
	discounts, err := globalAdminDiscountRows(ctx, confs)
	if err != nil {
		ctx.Err.Printf("/admin/discounts list: %s", err)
		http.Error(w, "Unable to load discounts", http.StatusInternalServerError)
		return
	}
	page := &GlobalAdminDiscountsPage{
		Confs:                  confs,
		Discounts:              discounts,
		SelectedConferenceRefs: map[string]bool{},
		Flash:                  r.URL.Query().Get("flash"),
		Year:                   helpers.CurrentYear(),
		Form: GlobalDiscountForm{DiscountForm: DiscountForm{
			DiscountType: "percent",
			Amount:       "50",
		}, ConferenceScope: "selected"},
	}

	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			page.FlashErr = "Couldn't read form. Try again."
			renderGlobalAdminDiscounts(w, ctx, page)
			return
		}
		action := strings.TrimSpace(r.PostForm.Get("action"))
		if action == "delete" {
			discountID := strings.TrimSpace(r.PostForm.Get("discount_id"))
			discount, err := getters.GetDiscountByRef(ctx, discountID)
			if err != nil {
				ctx.Err.Printf("/admin/discounts delete %s lookup: %s", discountID, err)
				page.FlashErr = "Couldn't load that discount code."
				renderGlobalAdminDiscounts(w, ctx, page)
				return
			}
			if discount == nil {
				page.FlashErr = "Discount code not found."
				renderGlobalAdminDiscounts(w, ctx, page)
				return
			}
			if err := getters.DeleteDiscount(ctx, discountID); err != nil {
				ctx.Err.Printf("/admin/discounts delete %s: %s", discountID, err)
				page.FlashErr = "Deleting the discount failed. Check server logs."
				renderGlobalAdminDiscounts(w, ctx, page)
				return
			}
			flash := fmt.Sprintf("Deleted %s.", strings.ToUpper(discount.CodeName))
			http.Redirect(w, r, "/admin/discounts?flash="+url.QueryEscape(flash), http.StatusSeeOther)
			return
		}
		page.Form = GlobalDiscountForm{
			DiscountForm:    discountFormFromRequest(r),
			ConferenceRefs:  r.PostForm["conference_refs"],
			ConferenceScope: normalizeGlobalDiscountScope(r.PostForm.Get("conference_scope")),
		}
		for _, ref := range page.Form.ConferenceRefs {
			page.SelectedConferenceRefs[ref] = true
		}
		var confRefs []string
		var selectedConfs []*types.Conf
		if page.Form.ConferenceScope == "selected" {
			confRefs, selectedConfs, err = validateGlobalDiscountConferences(confs, page.Form.ConferenceRefs)
			if err != nil {
				page.FlashErr = err.Error()
				renderGlobalAdminDiscounts(w, ctx, page)
				return
			}
		}
		expr, err := buildDiscountExpr(page.Form.DiscountForm)
		if err != nil {
			page.FlashErr = err.Error()
			renderGlobalAdminDiscounts(w, ctx, page)
			return
		}
		available, err := getters.IsCodeNameAvailable(ctx, page.Form.CodeName)
		if err != nil {
			ctx.Err.Printf("/admin/discounts availability %s: %s", page.Form.CodeName, err)
			page.FlashErr = "Couldn't check whether that code already exists."
			renderGlobalAdminDiscounts(w, ctx, page)
			return
		}
		if !available {
			page.FlashErr = "That code already exists. Pick another code name."
			renderGlobalAdminDiscounts(w, ctx, page)
			return
		}
		_, err = getters.CreateDiscount(ctx, getters.DiscountInput{
			CodeName:       strings.ToUpper(page.Form.CodeName),
			DiscountExpr:   expr,
			ConfRefs:       confRefs,
			AllConferences: page.Form.ConferenceScope == "all",
			AffiliateEmail: page.Form.AffiliateEmail,
		})
		if err != nil {
			ctx.Err.Printf("/admin/discounts create %s (%s): %s", page.Form.CodeName, expr, err)
			page.FlashErr = "Creating the discount failed. Check server logs."
			renderGlobalAdminDiscounts(w, ctx, page)
			return
		}
		target := conferenceNames(selectedConfs)
		if page.Form.ConferenceScope == "all" {
			target = "all current and future conferences"
		}
		flash := fmt.Sprintf("Created %s for %s.", strings.ToUpper(page.Form.CodeName), target)
		http.Redirect(w, r, "/admin/discounts?flash="+url.QueryEscape(flash), http.StatusSeeOther)
		return
	}

	renderGlobalAdminDiscounts(w, ctx, page)
}

func normalizeGlobalDiscountScope(scope string) string {
	if strings.TrimSpace(scope) == "all" {
		return "all"
	}
	return "selected"
}

func renderGlobalAdminDiscounts(w http.ResponseWriter, ctx *config.AppContext, page *GlobalAdminDiscountsPage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/global_discounts.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/discounts template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func validateGlobalDiscountConferences(confs []*types.Conf, requested []string) ([]string, []*types.Conf, error) {
	byRef := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			byRef[conf.Ref] = conf
		}
	}
	seen := make(map[string]bool, len(requested))
	refs := make([]string, 0, len(requested))
	selected := make([]*types.Conf, 0, len(requested))
	for _, ref := range requested {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		conf := byRef[ref]
		if conf == nil {
			return nil, nil, fmt.Errorf("One of the selected conferences no longer exists. Refresh and try again.")
		}
		seen[ref] = true
		refs = append(refs, ref)
		selected = append(selected, conf)
	}
	if len(refs) == 0 {
		return nil, nil, fmt.Errorf("Choose at least one conference.")
	}
	return refs, selected, nil
}

func sortGlobalDiscountConfs(confs []*types.Conf) {
	sort.SliceStable(confs, func(i, j int) bool {
		if confs[i].Active != confs[j].Active {
			return confs[i].Active
		}
		if !confs[i].StartDate.Equal(confs[j].StartDate) {
			return confs[i].StartDate.After(confs[j].StartDate)
		}
		return confs[i].Tag < confs[j].Tag
	})
}

func globalAdminDiscountRows(ctx *config.AppContext, confs []*types.Conf) ([]GlobalAdminDiscountRow, error) {
	discounts, err := getters.ListDiscounts(ctx)
	if err != nil {
		return nil, err
	}
	byRef := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			byRef[conf.Ref] = conf
		}
	}
	out := make([]GlobalAdminDiscountRow, 0, len(discounts))
	for _, discount := range discounts {
		row := GlobalAdminDiscountRow{AdminDiscountRow: adminDiscountRow(discount)}
		for _, ref := range discount.ConfRef {
			if conf := byRef[ref]; conf != nil {
				row.Conferences = append(row.Conferences, conf)
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToUpper(out[i].CodeName) < strings.ToUpper(out[j].CodeName)
	})
	return out, nil
}

func conferenceNames(confs []*types.Conf) string {
	names := make([]string, 0, len(confs))
	for _, conf := range confs {
		if conf != nil {
			names = append(names, conf.Desc)
		}
	}
	return strings.Join(names, " and ")
}

func renderAdminDiscounts(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, page *AdminDiscountsPage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/discounts.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/discounts template: %s", page.Conf.Tag, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func discountFormFromRequest(r *http.Request) DiscountForm {
	return DiscountForm{
		ID:             strings.TrimSpace(r.PostForm.Get("discount_id")),
		CodeName:       strings.TrimSpace(r.PostForm.Get("code_name")),
		DiscountType:   strings.TrimSpace(r.PostForm.Get("discount_type")),
		Amount:         strings.TrimSpace(r.PostForm.Get("amount")),
		ValidFrom:      strings.TrimSpace(r.PostForm.Get("valid_from")),
		ExpiresAt:      strings.TrimSpace(r.PostForm.Get("expires_at")),
		MaxAllowed:     strings.TrimSpace(r.PostForm.Get("max_allowed")),
		AffiliateEmail: strings.TrimSpace(r.PostForm.Get("affiliate_email")),
	}
}

func updateAdminDiscount(ctx *config.AppContext, conf *types.Conf, form DiscountForm, expr string) error {
	discount, err := adminDiscountForConf(ctx, conf, form.ID)
	if err != nil {
		return err
	}
	if discount == nil {
		return fmt.Errorf("Discount code not found for this event.")
	}
	if err := codeNameAvailableForUpdate(ctx, form.ID, form.CodeName); err != nil {
		return err
	}
	return getters.UpdateDiscount(ctx, form.ID, getters.DiscountInput{
		CodeName:       strings.ToUpper(form.CodeName),
		DiscountExpr:   expr,
		ConfRefs:       discount.ConfRef,
		AffiliateEmail: form.AffiliateEmail,
	})
}

func deleteAdminDiscount(ctx *config.AppContext, conf *types.Conf, discountID string) error {
	discount, err := adminDiscountForConf(ctx, conf, discountID)
	if err != nil {
		return err
	}
	if discount == nil {
		return fmt.Errorf("Discount code not found for this event.")
	}
	return getters.DeleteDiscount(ctx, discountID)
}

func adminDiscountForConf(ctx *config.AppContext, conf *types.Conf, discountID string) (*types.DiscountCode, error) {
	if strings.TrimSpace(discountID) == "" {
		return nil, fmt.Errorf("Discount ID is required.")
	}
	discount, err := getters.GetDiscountByRef(ctx, discountID)
	if err != nil {
		return nil, err
	}
	if discount == nil {
		return nil, nil
	}
	for _, ref := range discount.ConfRef {
		if ref == conf.Ref {
			return discount, nil
		}
	}
	return nil, nil
}

func codeNameAvailableForUpdate(ctx *config.AppContext, discountID, codeName string) error {
	discount, err := getters.GetDiscountByCode(ctx, codeName)
	if err != nil {
		return fmt.Errorf("Couldn't check whether that code already exists.")
	}
	if discount != nil && discount.Ref != discountID {
		return fmt.Errorf("That code already exists. Pick another code name.")
	}
	return nil
}

func buildDiscountExpr(form DiscountForm) (string, error) {
	code := strings.TrimSpace(form.CodeName)
	if code == "" {
		return "", fmt.Errorf("Code name is required.")
	}
	if strings.ContainsAny(code, " \t\r\n") {
		return "", fmt.Errorf("Code name cannot contain spaces.")
	}
	if form.AffiliateEmail != "" {
		if _, err := mail.ParseAddress(form.AffiliateEmail); err != nil {
			return "", fmt.Errorf("Affiliate email must be a valid email address, or blank.")
		}
	}
	amount, err := strconv.ParseUint(form.Amount, 10, 32)
	if err != nil || amount == 0 {
		return "", fmt.Errorf("Amount must be a positive whole number.")
	}

	var prefix string
	switch form.DiscountType {
	case "percent":
		if amount > 100 {
			return "", fmt.Errorf("Percent off cannot be more than 100.")
		}
		prefix = "%"
	case "dollars":
		prefix = "$"
	case "fixed":
		prefix = "="
	default:
		return "", fmt.Errorf("Choose percent off, dollars off, or an exact price.")
	}

	expr := fmt.Sprintf("%s%d", prefix, amount)
	if form.MaxAllowed != "" {
		maxUses, err := strconv.ParseUint(form.MaxAllowed, 10, 32)
		if err != nil || maxUses == 0 {
			return "", fmt.Errorf("Max allowed must be a positive whole number, or blank for unlimited.")
		}
		expr += fmt.Sprintf(":%d", maxUses)
	}

	from, err := parseDiscountAdminDate(form.ValidFrom)
	if err != nil {
		return "", fmt.Errorf("Valid from must be a valid date.")
	}
	until, err := parseDiscountAdminDate(form.ExpiresAt)
	if err != nil {
		return "", fmt.Errorf("Expires at must be a valid date.")
	}
	if from != "" && until != "" && until < from {
		return "", fmt.Errorf("Expires at must be the same day or after valid from.")
	}
	switch {
	case from != "" && until != "":
		expr += "@" + from + "-" + until
	case from != "":
		expr += "@" + from + "-"
	case until != "":
		expr += "<" + until
	}

	dc := &types.DiscountCode{Discount: expr}
	if err := dc.ParseDiscountExpr(); err != nil {
		return "", fmt.Errorf("Generated discount expression is invalid: %s", err)
	}
	return expr, nil
}

func parseDiscountAdminDate(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return "", err
	}
	return t.Format("20060102"), nil
}

func discountsForConf(ctx *config.AppContext, conf *types.Conf) []AdminDiscountRow {
	discounts, err := getters.ListDiscountsForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/admin/discounts fetch: %s", conf.Tag, err)
		return nil
	}
	sort.Slice(discounts, func(i, j int) bool {
		return strings.ToUpper(discounts[i].CodeName) < strings.ToUpper(discounts[j].CodeName)
	})

	out := make([]AdminDiscountRow, 0, len(discounts))
	for _, d := range discounts {
		out = append(out, adminDiscountRow(d))
	}
	return out
}

func adminDiscountRow(d *types.DiscountCode) AdminDiscountRow {
	amount := "-"
	switch d.DiscType {
	case '%':
		amount = fmt.Sprintf("%d%% off", d.Amount)
	case '$':
		amount = fmt.Sprintf("$%d off", d.Amount)
	case '=':
		amount = fmt.Sprintf("$%d fixed", d.Amount)
	}

	from := "anytime"
	if d.ValidFrom != nil {
		from = d.ValidFrom.Format("2006-01-02")
	}
	until := "no expiry"
	if d.ValidUntil != nil {
		until = d.ValidUntil.Format("2006-01-02")
	}

	max := "unlimited"
	if d.MaxUses > 0 {
		max = strconv.FormatUint(uint64(d.MaxUses), 10)
	}

	return AdminDiscountRow{
		ID:             d.Ref,
		CodeName:       d.CodeName,
		Expression:     d.Discount,
		AmountLabel:    amount,
		ValidDates:     from + " to " + until,
		UsesLabel:      fmt.Sprintf("%d / %s", d.UsesCount, max),
		AffiliateEmail: d.AffiliateEmail,
		Form:           discountFormFromCode(d),
	}
}

func discountFormFromCode(d *types.DiscountCode) DiscountForm {
	form := DiscountForm{
		ID:             d.Ref,
		CodeName:       d.CodeName,
		Amount:         strconv.FormatUint(uint64(d.Amount), 10),
		AffiliateEmail: d.AffiliateEmail,
	}
	switch d.DiscType {
	case '%':
		form.DiscountType = "percent"
	case '$':
		form.DiscountType = "dollars"
	case '=':
		form.DiscountType = "fixed"
	default:
		form.DiscountType = "percent"
	}
	if d.MaxUses > 0 {
		form.MaxAllowed = strconv.FormatUint(uint64(d.MaxUses), 10)
	}
	if d.ValidFrom != nil {
		form.ValidFrom = d.ValidFrom.Format("2006-01-02")
	}
	if d.ValidUntil != nil {
		form.ExpiresAt = d.ValidUntil.Format("2006-01-02")
	}
	return form
}
