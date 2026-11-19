package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type organizationShopPage struct {
	*OrganizationDashboardPage
	EventSales []getters.OrganizationEventSale
	EventStock []getters.OrganizationEventStock
	Org        *types.Org
	Profile    *types.OrganizationShopProfile
	Products   []*types.MerchProduct
	Product    *types.MerchProduct
	Orders     []*getters.OrganizationShopOrder
	Discounts  []types.OrganizationShopDiscount
	Confs      []*types.Conf
	Events     map[string]bool
	Tab        string
	Base       string
	Operation  string
	Flash      string
}

func registerOrganizationShops(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/dashboard/orgs/{organizationID}/shop", func(w http.ResponseWriter, r *http.Request) { OrganizationShop(w, r, app) }).Methods("GET", "POST")
	r.HandleFunc("/shop/discount", func(w http.ResponseWriter, r *http.Request) { ShopDiscount(w, r, app) }).Methods("POST")
}
func organizationShopIdentity(w http.ResponseWriter, r *http.Request, app *config.AppContext) (*auth.Identity, *types.Org) {
	id, e := auth.Resolve(r, app)
	if e != nil || id == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), 303)
		return nil, nil
	}
	ref := mux.Vars(r)["organizationID"]
	if id.IsGlobalAdmin() {
		org, e := getters.GetOrg(app, ref)
		if e != nil || org == nil {
			http.NotFound(w, r)
			return nil, nil
		}
		return id, org
	}
	members, e := getters.ListOrganizationMembershipsForPerson(app, id.PersonID)
	if e != nil {
		http.Error(w, "Unable to load organization access", 500)
		return nil, nil
	}
	membership := organizationMembershipByReference(members, ref)
	if membership == nil || !sponsorMembershipCanManage(membership) {
		http.Error(w, "Organization owners and managers can manage this shop", 403)
		return nil, nil
	}
	return id, membership.Organization
}
func OrganizationShop(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	id, org := organizationShopIdentity(w, r, app)
	if id == nil || org == nil {
		return
	}
	base := "/dashboard/orgs/" + organizationPathRef(org) + "/shop"
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	if r.Method == "POST" {
		organizationShopSave(w, r, app, id, org, base)
		return
	}
	csrf, e := ensureAuthMethodsCSRF(app, r)
	if e != nil {
		http.Error(w, "Unable to prepare shop", 500)
		return
	}
	workspace, e := organizationShopWorkspace(app, id, org)
	if e != nil {
		app.Err.Printf("organization shop workspace: %s", e)
		http.Error(w, "Unable to load organization workspace", http.StatusInternalServerError)
		return
	}
	workspace.CSRF = csrf
	p := &organizationShopPage{OrganizationDashboardPage: workspace, Org: org, Base: base, Tab: r.URL.Query().Get("tab"), Operation: uuid.NewString(), Events: map[string]bool{}, Flash: r.URL.Query().Get("saved")}
	if p.Tab == "" {
		p.Tab = "profile"
	}
	p.Profile, e = getters.GetOrganizationShopProfile(app, org.Ref)
	if e != nil {
		http.Error(w, "Unable to load shop profile", 500)
		return
	}
	switch p.Tab {
	case "profile":
	case "catalog":
		p.Products, e = getters.ListOrganizationMerch(app, org.Ref)
		if e == nil {
			p.EventStock, e = getters.OrganizationEventInventory(app, org.Ref)
		}
	case "product":
		productID := r.URL.Query().Get("product")
		if productID != "" {
			if !getters.OrganizationOwnsProduct(app, org.Ref, productID) {
				http.NotFound(w, r)
				return
			}
			p.Product, e = getters.GetMerchProductByID(app, productID)
			if e == nil {
				var events []string
				events, e = getters.OrganizationProductEvents(app, productID)
				for _, event := range events {
					p.Events[event] = true
				}
			}
		} else {
			p.Product = &types.MerchProduct{Currency: "USD", Symbol: "$", Status: "draft", ProductType: "apparel", RequiresShipping: true, OnlineEnabled: true, StripeTaxCode: types.StripeTaxCodeTangibleGood}
		}
		if e == nil {
			var confs []*types.Conf
			confs, e = getters.ListConfs(app)
			for _, conf := range confs {
				if conf.IsPublished() && conf.EndDate.After(time.Now()) {
					p.Confs = append(p.Confs, conf)
				}
			}
		}
	case "orders":
		p.Orders, e = getters.ListOrganizationShopOrders(app, org.Ref)
		if e == nil {
			p.EventSales, e = getters.OrganizationEventPurchases(app, org.Ref)
		}
	case "discounts":
		p.Discounts, e = getters.ListOrganizationDiscounts(app, org.Ref)
	default:
		http.NotFound(w, r)
		return
	}
	if e != nil {
		app.Err.Printf("organization shop: %s", e)
		http.Error(w, "Unable to load shop", 500)
		return
	}
	if e = app.TemplateCache.ExecuteTemplate(w, "shop/organization.tmpl", p); e != nil {
		app.Err.Printf("organization shop render: %s", e)
	}
}

func organizationShopWorkspace(app *config.AppContext, id *auth.Identity, org *types.Org) (*OrganizationDashboardPage, error) {
	memberships, err := getters.ListOrganizationMembershipsForPerson(app, id.PersonID)
	if err != nil {
		return nil, err
	}
	membership := organizationMembershipByReference(memberships, org.Ref)
	if membership == nil {
		if !id.IsGlobalAdmin() {
			return nil, fmt.Errorf("organization membership is no longer available")
		}
		// Global admins can inspect a shop without joining the organization.
		membership = &types.OrganizationMembership{OrganizationID: org.Ref, Organization: org, Role: "admin"}
	}
	members, err := getters.ListOrganizationMembers(app, org.Ref)
	if err != nil {
		return nil, err
	}
	sponsorEvents, err := getters.ListSponsorDashboardEvents(app, org.Ref)
	if err != nil {
		return nil, err
	}
	hasProjects, err := getters.HasHackathonParticipantProjectsForPerson(app, id.PersonID)
	if err != nil {
		app.Err.Printf("organization shop hackathon projects: %s", err)
	}
	return &OrganizationDashboardPage{
		Organization: org, Membership: membership, Memberships: memberships,
		Members: members, SponsorEvents: sponsorEvents, CanManage: true,
		IsOwner: membership.Role == getters.OrganizationRoleOwner, IsGlobalAdmin: id.IsGlobalAdmin(),
		PendingOrgInviteCount: pendingOrganizationInviteCount(app, id.PersonID, "organization shop"),
		HasHackathonProjects:  hasProjects, ShowSponsors: hasManagedSponsorOrganization(app, id.PersonID, "organization shop"),
		Year: helpers.CurrentYear(),
	}, nil
}

func organizationShopSave(w http.ResponseWriter, r *http.Request, app *config.AppContext, id *auth.Identity, org *types.Org, base string) {
	limitRequestBody(w, r, 12<<20)
	e := r.ParseMultipartForm(1 << 20)
	if e != nil && e != http.ErrNotMultipart {
		wizardError(w, fmt.Errorf("form is too large or invalid"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !wizardCSRF(w, r, app) {
		return
	}
	destination := base
	action := r.FormValue("action")
	switch action {
	case "profile":
		revision, err := strconv.ParseInt(r.FormValue("revision"), 10, 64)
		if err != nil {
			wizardError(w, fmt.Errorf("invalid form revision"))
			return
		}
		p := types.OrganizationShopProfile{Revision: revision, BillingName: r.FormValue("billing_name"), TaxID: r.FormValue("tax_id"), Notes: r.FormValue("notes"), Billing: shopProfileAddress(r, "billing_")}
		p.ShipFrom = types.EasyshipSettings{ContactName: r.FormValue("ship_contact"), CompanyName: r.FormValue("ship_company"), Email: r.FormValue("ship_email"), Phone: r.FormValue("ship_phone"), Line1: r.FormValue("ship_line1"), Line2: r.FormValue("ship_line2"), City: r.FormValue("ship_city"), Region: r.FormValue("ship_region"), PostalCode: r.FormValue("ship_postal_code"), CountryAlpha2: r.FormValue("ship_country")}
		_, e = getters.SaveOrganizationShopProfile(app, org.Ref, id.PersonID, p)
	case "product":
		if e = normalizeMerchAdminPrices(r); e != nil {
			break
		}
		in := getters.OrganizationProductInput{ID: r.FormValue("product_id"), MerchProductInput: merchProductInputFromForm(r), OnlineEnabled: r.FormValue("online_enabled") == "on", EventIDs: r.Form["event_ids"]}
		if in.ID != "" {
			in.UpdatedAt, e = time.Parse(time.RFC3339Nano, r.FormValue("updated_at"))
			if e != nil {
				break
			}
		}
		in.Currency = "USD"
		in.RequiresShipping = true
		var productID string
		productID, e = getters.SaveOrganizationProduct(app, org.Ref, in)
		destination = base + "?tab=product&product=" + url.QueryEscape(productID)
	case "variant":
		productID := r.FormValue("product_id")
		if e = normalizeMerchAdminPrices(r); e != nil {
			break
		}
		delta, err := strconv.Atoi(r.FormValue("stock_delta"))
		if err != nil {
			e = fmt.Errorf("enter a whole-number stock adjustment")
			break
		}
		_, e = getters.SaveOrganizationVariant(app, org.Ref, productID, r.FormValue("variant_id"), id.Email, r.FormValue("operation"), merchVariantInputFromForm(r, productID), delta)
		destination = base + "?tab=product&product=" + url.QueryEscape(productID)
	case "image":
		productID := r.FormValue("product_id")
		if !getters.OrganizationOwnsProduct(app, org.Ref, productID) {
			e = fmt.Errorf("product not found in this organization")
			break
		}
		var raw []byte
		var mime, ext string
		raw, mime, ext, e = readMultipartImageFile(r, "file", true)
		if e != nil {
			break
		}
		if !spaces.IsConfigured() {
			e = fmt.Errorf("image storage is not configured")
			break
		}
		_, _, e = storeMerchProductImage(app, productID, raw, mime, ext, r.FormValue("alt_text"), 0, true)
		destination = base + "?tab=product&product=" + url.QueryEscape(productID)
	case "discount":
		var percent, max int
		percent, e = strconv.Atoi(r.FormValue("percent_off"))
		if e != nil {
			break
		}
		max, e = strconv.Atoi(r.FormValue("max_uses"))
		if e != nil {
			break
		}
		d := types.OrganizationShopDiscount{ID: r.FormValue("discount_id"), Code: r.FormValue("code"), PercentOff: percent, MaxUses: max, Active: r.FormValue("active") == "on"}
		d.StartsAt, e = shopDiscountTime(r.FormValue("starts_at"))
		if e != nil {
			break
		}
		d.EndsAt, e = shopDiscountTime(r.FormValue("ends_at"))
		if e != nil {
			break
		}
		e = getters.SaveOrganizationDiscount(app, org.Ref, d)
		destination = base + "?tab=discounts"
	case "ship":
		e = getters.ShipOrganizationOrder(app, org.Ref, r.FormValue("order_id"), id.Email, r.FormValue("courier"), r.FormValue("tracking_number"), r.FormValue("tracking_url"))
		destination = base + "?tab=orders"
	default:
		e = fmt.Errorf("unknown shop action")
	}
	if e != nil {
		if errors.Is(e, getters.ErrShopEditConflict) {
			wizardJSON(w, 409, map[string]string{"error": e.Error()})
			return
		}
		wizardError(w, e)
		return
	}
	separator := "?"
	if strings.Contains(destination, "?") {
		separator = "&"
	}
	wizardJSON(w, 200, map[string]string{"url": destination + separator + "saved=1"})
}
func shopProfileAddress(r *http.Request, prefix string) types.ShopAddress {
	return types.ShopAddress{Line1: r.FormValue(prefix + "line1"), Line2: r.FormValue(prefix + "line2"), City: r.FormValue(prefix + "city"), Region: r.FormValue(prefix + "region"), PostalCode: r.FormValue(prefix + "postal_code"), Country: r.FormValue(prefix + "country")}
}
func shopDiscountTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, e := time.Parse("2006-01-02T15:04", raw)
	if e != nil {
		return nil, fmt.Errorf("enter a valid UTC date and time")
	}
	return &t, nil
}
