package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
)

const adminSubscribersPageSize = 25

type AdminSubscribersPage struct {
	Summary         getters.AdminSubscriberSummary
	ListCounts      []getters.AdminSubscriberListCount
	Subscribers     []getters.AdminSubscriberRow
	Search          string
	SelectedList    string
	Status          string
	TotalFiltered   int
	Page            int
	TotalPages      int
	FirstResult     int
	LastResult      int
	PreviousPageURL string
	NextPageURL     string
	Year            uint
}

func AdminSubscribers(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}

	search := strings.TrimSpace(r.URL.Query().Get("q"))
	list := strings.TrimSpace(r.URL.Query().Get("list"))
	status := normalizeAdminSubscriberStatus(r.URL.Query().Get("status"))
	pageNumber := positiveInt(r.URL.Query().Get("page"), 1)

	result, err := getters.ListAdminSubscribers(
		ctx,
		search,
		list,
		status,
		adminSubscribersPageSize,
		(pageNumber-1)*adminSubscribersPageSize,
	)
	if err != nil {
		ctx.Err.Printf("/admin/subscribers: %s", err)
		http.Error(w, "Unable to load subscribers", http.StatusInternalServerError)
		return
	}
	totalPages := 1
	if result.TotalFiltered > 0 {
		totalPages = (result.TotalFiltered + adminSubscribersPageSize - 1) / adminSubscribersPageSize
	}
	if pageNumber > totalPages {
		http.Redirect(w, r, adminSubscribersURL(search, list, status, totalPages), http.StatusSeeOther)
		return
	}

	page := buildAdminSubscribersPage(result, search, list, status, pageNumber)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/subscribers.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/subscribers template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func buildAdminSubscribersPage(result *getters.AdminSubscriberResult, search, list, status string, pageNumber int) *AdminSubscribersPage {
	if pageNumber < 1 {
		pageNumber = 1
	}
	totalPages := 1
	if result.TotalFiltered > 0 {
		totalPages = (result.TotalFiltered + adminSubscribersPageSize - 1) / adminSubscribersPageSize
	}
	first := 0
	last := 0
	if len(result.Subscribers) > 0 {
		first = (pageNumber-1)*adminSubscribersPageSize + 1
		last = first + len(result.Subscribers) - 1
	}

	page := &AdminSubscribersPage{
		Summary:       result.Summary,
		ListCounts:    result.ListCounts,
		Subscribers:   result.Subscribers,
		Search:        search,
		SelectedList:  list,
		Status:        normalizeAdminSubscriberStatus(status),
		TotalFiltered: result.TotalFiltered,
		Page:          pageNumber,
		TotalPages:    totalPages,
		FirstResult:   first,
		LastResult:    last,
		Year:          helpers.CurrentYear(),
	}
	if pageNumber > 1 {
		page.PreviousPageURL = adminSubscribersURL(search, list, page.Status, pageNumber-1)
	}
	if pageNumber < totalPages {
		page.NextPageURL = adminSubscribersURL(search, list, page.Status, pageNumber+1)
	}
	return page
}

func adminSubscribersURL(search, list, status string, page int) string {
	query := url.Values{}
	if search = strings.TrimSpace(search); search != "" {
		query.Set("q", search)
	}
	if list = strings.TrimSpace(list); list != "" {
		query.Set("list", list)
	}
	if status = normalizeAdminSubscriberStatus(status); status != "all" {
		query.Set("status", status)
	}
	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}
	if encoded := query.Encode(); encoded != "" {
		return "/admin/subscribers?" + encoded
	}
	return "/admin/subscribers"
}

func normalizeAdminSubscriberStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "active":
		return "active"
	case "inactive":
		return "inactive"
	default:
		return "all"
	}
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return fallback
	}
	if parsed > 1000000 {
		return fallback
	}
	return parsed
}
