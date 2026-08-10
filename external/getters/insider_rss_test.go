package getters

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type insiderRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn insiderRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFetchInsiderWeeklyIssueUsesMondayHighlights(t *testing.T) {
	feed := `<?xml version="1.0"?><rss xmlns:content="http://purl.org/rss/1.0/modules/content/" version="2.0"><channel>
<item><title><![CDATA[The Red Team — Last Week in Bitcoin (Aug 03 - 09)]]></title><link>https://insider.example/current</link><pubDate>Mon, 10 Aug 2026 14:02:22 GMT</pubDate><content:encoded><![CDATA[
<h1>Highlights from the bitcoin developer ecosystem</h1><p>Intro.</p><ul>
<li><p><a href="https://example.com/one">PR8913</a> exposes atomic-swap APIs.</p><ul><li>Nested detail must not be copied.</li></ul></li>
<li><p>CDK is adding support for Iroh.</p><ul><li><a href="https://example.com/nested">Nested link must not be used.</a></li></ul></li><li><p><a href="/three">Marmot upgraded to version 2.</a></p></li><li><p>Fourth item.</p></li>
</ul><h1>Other News</h1><ul><li>Wrong list.</li></ul>]]></content:encoded></item>
</channel></rss>`
	client := &http.Client{Transport: insiderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(feed)), Header: make(http.Header)}, nil
	})}
	issueAt := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.FixedZone("CDT", -5*60*60))
	issue, err := fetchInsiderWeeklyIssue(context.Background(), client, "https://insider.example/feed", issueAt)
	if err != nil {
		t.Fatalf("fetchInsiderWeeklyIssue: %v", err)
	}
	if issue == nil || issue.Link != "https://insider.example/current" {
		t.Fatalf("issue = %#v", issue)
	}
	want := []InsiderWeeklyBullet{
		{Text: "PR8913 exposes atomic-swap APIs.", Link: "https://example.com/one"},
		{Text: "CDK is adding support for Iroh."},
		{Text: "Marmot upgraded to version 2.", Link: "https://insider.example/three"},
	}
	if len(issue.Bullets) != len(want) {
		t.Fatalf("bullets = %#v", issue.Bullets)
	}
	for i := range want {
		if issue.Bullets[i] != want[i] {
			t.Errorf("bullet %d = %#v, want %#v", i, issue.Bullets[i], want[i])
		}
	}
}

func TestFetchInsiderWeeklyIssueSkipsNonMondayIssue(t *testing.T) {
	feed := `<?xml version="1.0"?><rss xmlns:content="http://purl.org/rss/1.0/modules/content/" version="2.0"><channel><item><title>Last Week in Bitcoin</title><link>https://insider.example/old</link><pubDate>Mon, 03 Aug 2026 14:02:22 GMT</pubDate><content:encoded><![CDATA[<h1>Highlights from the bitcoin developer ecosystem</h1><ul><li>Old.</li></ul>]]></content:encoded></item></channel></rss>`
	client := &http.Client{Transport: insiderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(feed)), Header: make(http.Header)}, nil
	})}
	issueAt := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.FixedZone("CDT", -5*60*60))
	issue, err := fetchInsiderWeeklyIssue(context.Background(), client, "https://insider.example/feed", issueAt)
	if err != nil {
		t.Fatalf("fetchInsiderWeeklyIssue: %v", err)
	}
	if issue != nil {
		t.Fatalf("issue = %#v, want nil", issue)
	}
}
