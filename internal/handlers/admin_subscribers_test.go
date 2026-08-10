package handlers

import (
	"net/url"
	"testing"

	"btcpp-web/external/getters"
)

func TestBuildAdminSubscribersPagePagination(t *testing.T) {
	rows := make([]getters.AdminSubscriberRow, adminSubscribersPageSize)
	page := buildAdminSubscribersPage(&getters.AdminSubscriberResult{
		Subscribers:   rows,
		TotalFiltered: 52,
	}, "sats+search@example.com", "newsletter", "active", 2)

	if page.TotalPages != 3 || page.FirstResult != 26 || page.LastResult != 50 {
		t.Fatalf("pagination = pages:%d range:%d-%d", page.TotalPages, page.FirstResult, page.LastResult)
	}
	for name, rawURL := range map[string]string{"previous": page.PreviousPageURL, "next": page.NextPageURL} {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("parse %s URL: %v", name, err)
		}
		if parsed.Path != "/admin/subscribers" || parsed.Query().Get("q") != "sats+search@example.com" || parsed.Query().Get("list") != "newsletter" || parsed.Query().Get("status") != "active" {
			t.Fatalf("%s URL lost filters: %s", name, rawURL)
		}
	}
	if got := page.PreviousPageURL; urlPage(t, got) != "" {
		t.Fatalf("previous page query = %q, want first page without page parameter", urlPage(t, got))
	}
	if got := urlPage(t, page.NextPageURL); got != "3" {
		t.Fatalf("next page query = %q, want 3", got)
	}
}

func TestAdminSubscriberFilterNormalization(t *testing.T) {
	if got := normalizeAdminSubscriberStatus("inactive"); got != "inactive" {
		t.Fatalf("status = %q", got)
	}
	if got := normalizeAdminSubscriberStatus("unknown"); got != "all" {
		t.Fatalf("unknown status = %q, want all", got)
	}
	if got := positiveInt("-9", 1); got != 1 {
		t.Fatalf("negative page = %d, want fallback", got)
	}
	if got := positiveInt("4", 1); got != 4 {
		t.Fatalf("page = %d, want 4", got)
	}
}

func urlPage(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	return parsed.Query().Get("page")
}
