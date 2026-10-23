package xstudio

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const postAnalyticsResponse = `{"data":{"viewer_v2":{"user_results":{"result":{"core":{"screen_name":"btcplusplus"},"tweets_results":[{"result":{"__typename":"Tweet","rest_id":"123","details":{"full_text":"A test post","created_at_ms":1767268800000},"organic_metrics_total":[{"metric_type":"Impressions","metric_value":1500}],"url_entities":[{"expanded_url":"https://btcpp.dev/nairobi"}]}},{}]}}}}}`

type postTransport func(*http.Request) (*http.Response, error)

func (f postTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPostAnalyticsParse(t *testing.T) {
	account, posts, unavailable, err := parseAnalyticsPosts([]byte(postAnalyticsResponse))
	if err != nil || account != "btcplusplus" || len(posts) != 1 || posts[0].Impressions != 1500 || unavailable != 1 {
		t.Fatalf("bad parse %s %+v %d %v", account, posts, unavailable, err)
	}
	for _, raw := range []string{`{}`, `{"errors":[{"message":"denied"}]}`, strings.Replace(postAnalyticsResponse, `"Impressions"`, `"Likes"`, 1)} {
		if _, _, _, err := parseAnalyticsPosts([]byte(raw)); err == nil {
			t.Fatal("incomplete response accepted")
		}
	}
}
func TestPostAnalyticsSessionIsolation(t *testing.T) {
	requests := 0
	client, _ := New(Config{Cookie: "auth_token=secret; ct0=csrf", IngestID: "unused", HTTPClient: &http.Client{Transport: postTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		body := ""
		switch {
		case r.URL.Host == "abs.twimg.com":
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
				t.Fatal("session leaked to asset host")
			}
			if strings.Contains(r.URL.Path, "main.") {
				body = "AAAAA" + strings.Repeat("a", 60)
			} else {
				body = `params:{id:"query-id",metadata:{},name:"contentPageQuery"`
			}
		case strings.Contains(r.URL.Path, "graphql"):
			if r.Header.Get("x-csrf-token") != "csrf" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Fatal("missing session headers")
			}
			body = postAnalyticsResponse
		default:
			body = `<script src="https://abs.twimg.com/responsive-web/client-web/main.123a.js"></script>1:"bundle.AccountAnalytics",1:"0123456789abcdef"`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}})
	result, err := client.PostAnalytics(context.Background(), 2026, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || len(result.Posts) != 1 || requests != 4 {
		t.Fatalf("refresh result %+v; requests %d; %v", result, requests, err)
	}
}
