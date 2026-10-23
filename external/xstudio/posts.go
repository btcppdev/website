package xstudio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PostAnalytics is a dated snapshot of organic lifetime counters for posts
// published during the selected year, not impressions earned only in that year.
type PostAnalytics struct {
	Account     string          `json:"account"`
	Collected   string          `json:"collected"`
	Year        int             `json:"year"`
	Unavailable int             `json:"unavailable"`
	Posts       []AnalyticsPost `json:"posts"`
}
type AnalyticsPost struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Created     string   `json:"created"`
	Impressions int64    `json:"impressions"`
	Links       []string `json:"links"`
}

// PostAnalytics uses the same authenticated browser session as Studio. Discover
// the current public client operation rather than assuming its query ID is stable.
func (c *Client) PostAnalytics(ctx context.Context, year int, now time.Time) (PostAnalytics, error) {
	out := PostAnalytics{Year: year, Collected: now.UTC().Format(time.RFC3339)}
	if year < 2020 || year > now.Year() {
		return out, fmt.Errorf("invalid analytics year")
	}
	client := *c.config.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	read := func(raw string, session bool, bearer string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.config.UserAgent)
		if session {
			req.Header.Set("Cookie", c.config.Cookie)
			req.Header.Set("Referer", "https://x.com/i/account_analytics")
			if bearer != "" {
				req.Header.Set("Authorization", "Bearer "+bearer)
				req.Header.Set("x-twitter-auth-type", "OAuth2Session")
				req.Header.Set("x-twitter-active-user", "yes")
				cookies := &http.Request{Header: http.Header{"Cookie": {c.config.Cookie}}}
				csrf, err := cookies.Cookie("ct0")
				if err != nil {
					return nil, fmt.Errorf("X session requires CSRF cookie")
				}
				req.Header.Set("x-csrf-token", csrf.Value)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("X analytics request failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("X analytics HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
		if err != nil || len(data) > 32<<20 {
			return nil, fmt.Errorf("invalid X analytics response")
		}
		return data, nil
	}
	html, err := read("https://x.com/i/account_analytics", true, "")
	if err != nil {
		return out, err
	}
	main := regexp.MustCompile(`https://abs\.twimg\.com/responsive-web/client-web/main\.[A-Za-z0-9]+\.js`).Find(html)
	bundle := regexp.MustCompile(`([0-9]+):"bundle.AccountAnalytics"`).FindSubmatch(html)
	if len(main) == 0 || len(bundle) != 2 {
		return out, fmt.Errorf("X analytics client could not be discovered")
	}
	hash := regexp.MustCompile(string(bundle[1]) + `:"([a-f0-9]{16})"`).FindSubmatch(html)
	if len(hash) != 2 {
		return out, fmt.Errorf("X analytics bundle hash missing")
	}
	script, err := read(string(main), false, "")
	if err != nil {
		return out, err
	}
	bearer := regexp.MustCompile(`AAAAA[A-Za-z0-9%_-]{50,}`).Find(script)
	script, err = read("https://abs.twimg.com/responsive-web/client-web/bundle.AccountAnalytics."+string(hash[1])+"a.js", false, "")
	if err != nil {
		return out, err
	}
	operation := regexp.MustCompile(`params:\{id:"([A-Za-z0-9_-]+)",metadata:\{\},name:"contentPageQuery"`).FindSubmatch(script)
	if len(bearer) == 0 || len(operation) != 2 {
		return out, fmt.Errorf("X analytics query could not be discovered")
	}
	seen := map[string]AnalyticsPost{}
	for from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC); from.Before(now) && from.Year() == year; {
		to := from.AddDate(0, 1, 0)
		if to.After(now) {
			to = now.UTC()
		}
		vars, _ := json.Marshal(map[string]any{"from_time": from.Format("2006-01-02T15:04:05.000Z"), "to_time": to.Format("2006-01-02T15:04:05.000Z"), "max_results": 1000, "query_page_size": 100, "requested_metrics": []string{"Impressions"}})
		endpoint := "https://x.com/i/api/graphql/" + string(operation[1]) + "/contentPageQuery?" + url.Values{"variables": {string(vars)}}.Encode()
		raw, err := read(endpoint, true, string(bearer))
		if err != nil {
			return out, err
		}
		account, posts, unavailable, err := parseAnalyticsPosts(raw)
		if err != nil {
			return out, fmt.Errorf("X analytics month %d: %w", from.Month(), err)
		}
		if out.Account != "" && account != out.Account {
			return out, fmt.Errorf("X analytics account changed")
		}
		out.Account = account
		out.Unavailable += unavailable
		for _, p := range posts {
			created, err := time.Parse(time.RFC3339, p.Created)
			if err != nil || created.Before(from) || !created.Before(to) {
				return out, fmt.Errorf("X returned a post outside the requested window")
			}
			seen[p.ID] = p
		}
		from = to
	}
	for _, p := range seen {
		out.Posts = append(out.Posts, p)
	}
	sort.Slice(out.Posts, func(i, j int) bool { return out.Posts[i].ID < out.Posts[j].ID })
	return out, nil
}

func parseAnalyticsPosts(raw []byte) (string, []AnalyticsPost, int, error) {
	var d struct {
		Errors []json.RawMessage `json:"errors"`
		Data   struct {
			Viewer struct {
				Users struct {
					Result *struct {
						Core struct {
							ScreenName string `json:"screen_name"`
						} `json:"core"`
						Tweets []struct {
							Result *struct {
								Type    string `json:"__typename"`
								ID      string `json:"rest_id"`
								Details struct {
									Text    string `json:"full_text"`
									Created int64  `json:"created_at_ms"`
								} `json:"details"`
								Metrics []struct {
									Type  string `json:"metric_type"`
									Value int64  `json:"metric_value"`
								} `json:"organic_metrics_total"`
								URLs []struct {
									URL string `json:"expanded_url"`
								} `json:"url_entities"`
							} `json:"result"`
						} `json:"tweets_results"`
					} `json:"result"`
				} `json:"user_results"`
			} `json:"viewer_v2"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &d) != nil || len(d.Errors) > 0 || d.Data.Viewer.Users.Result == nil {
		return "", nil, 0, fmt.Errorf("incomplete analytics response")
	}
	user := d.Data.Viewer.Users.Result
	if user.Core.ScreenName == "" {
		return "", nil, 0, fmt.Errorf("missing account identity")
	}
	if len(user.Tweets) >= 1000 {
		return "", nil, 0, fmt.Errorf("monthly post limit reached; snapshot not replaced")
	}
	var posts []AnalyticsPost
	unavailable := 0
	for _, row := range user.Tweets {
		t := row.Result
		if t == nil || t.Type != "Tweet" {
			unavailable++
			continue
		}
		p := AnalyticsPost{ID: t.ID, Text: t.Details.Text, Created: time.UnixMilli(t.Details.Created).UTC().Format(time.RFC3339)}
		valid := false
		for _, m := range t.Metrics {
			if m.Type == "Impressions" {
				p.Impressions = m.Value
				valid = true
			}
		}
		if !valid || p.Impressions < 0 || t.Details.Created <= 0 || !regexp.MustCompile(`^[0-9]+$`).MatchString(p.ID) {
			return "", nil, 0, fmt.Errorf("missing or invalid post metrics")
		}
		for _, u := range t.URLs {
			if strings.HasPrefix(u.URL, "https://") {
				p.Links = append(p.Links, u.URL)
			}
		}
		posts = append(posts, p)
	}
	return user.Core.ScreenName, posts, unavailable, nil
}
