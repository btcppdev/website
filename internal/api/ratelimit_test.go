package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestRateLimiterRefillsAndReturnsRetryAfter(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter()
	limiter.now = func() time.Time { return now }
	if allowed, _ := limiter.allow("client", 2, 1); !allowed {
		t.Fatal("first request denied")
	}
	if allowed, _ := limiter.allow("client", 2, 1); !allowed {
		t.Fatal("second request denied")
	}
	if allowed, retry := limiter.allow("client", 2, 1); allowed || retry != 1 {
		t.Fatalf("third request allowed=%v retry=%d", allowed, retry)
	}
	now = now.Add(time.Second)
	if allowed, _ := limiter.allow("client", 2, 1); !allowed {
		t.Fatal("request did not refill")
	}
}

func TestPaginationUsesOpaqueCursorAndRejectsInvalidLimits(t *testing.T) {
	firstRequest := httptest.NewRequest(http.MethodGet, "/items?limit=2", nil)
	page, cursor, limit, err := paginate(firstRequest, []int{1, 2, 3})
	if err != nil || len(page) != 2 || page[0] != 1 || cursor == "" || limit != 2 {
		t.Fatalf("first page=%v cursor=%q limit=%d err=%v", page, cursor, limit, err)
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/items?limit=2&cursor="+url.QueryEscape(cursor), nil)
	page, next, _, err := paginate(secondRequest, []int{1, 2, 3})
	if err != nil || len(page) != 1 || page[0] != 3 || next != "" {
		t.Fatalf("second page=%v cursor=%q err=%v", page, next, err)
	}
	invalid := httptest.NewRequest(http.MethodGet, "/items?limit=101", nil)
	if _, _, _, err := paginate(invalid, []int{}); err == nil {
		t.Fatal("accepted oversized limit")
	}
}
