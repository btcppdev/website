package getters

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

const insiderWeeklyFeedURL = "https://insider.btcpp.dev/feed"

type InsiderWeeklyIssue struct {
	Title       string
	Link        string
	PublishedAt time.Time
	Bullets     []InsiderWeeklyBullet
}

type InsiderWeeklyBullet struct {
	Text string
	Link string
}

type insiderRSS struct {
	Items []insiderRSSItem `xml:"channel>item"`
}

type insiderRSSItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
	Content string `xml:"encoded"`
}

// LatestInsiderWeeklyIssue returns the Last Week in Bitcoin issue published on
// the Monday immediately before issueSendAt. A missing Monday issue is not an
// error and returns nil.
func LatestInsiderWeeklyIssue(ctx context.Context, issueSendAt time.Time) (*InsiderWeeklyIssue, error) {
	return fetchInsiderWeeklyIssue(ctx, httpClient, insiderWeeklyFeedURL, issueSendAt)
}

func fetchInsiderWeeklyIssue(ctx context.Context, client *http.Client, feedURL string, issueSendAt time.Time) (*InsiderWeeklyIssue, error) {
	if client == nil {
		return nil, fmt.Errorf("Insider RSS HTTP client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create Insider RSS request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9")
	req.Header.Set("User-Agent", "btcpp-web weekly newsletter")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Insider RSS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch Insider RSS: unexpected status %s", resp.Status)
	}
	const maxFeedBytes = 5 << 20
	var feed insiderRSS
	decoder := xml.NewDecoder(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err := decoder.Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode Insider RSS: %w", err)
	}

	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return nil, fmt.Errorf("load newsletter timezone: %w", err)
	}
	localIssue := issueSendAt.In(loc)
	monday := time.Date(localIssue.Year(), localIssue.Month(), localIssue.Day()-1, 0, 0, 0, 0, loc)
	for _, item := range feed.Items {
		if !strings.Contains(strings.ToLower(item.Title), "last week in bitcoin") {
			continue
		}
		publishedAt, err := time.Parse(time.RFC1123Z, strings.TrimSpace(item.PubDate))
		if err != nil {
			publishedAt, err = time.Parse(time.RFC1123, strings.TrimSpace(item.PubDate))
		}
		if err != nil || !sameLocalDate(publishedAt, monday, loc) {
			continue
		}
		bullets, err := insiderHighlightBullets(item.Content, 3)
		if err != nil {
			return nil, fmt.Errorf("parse Insider highlights for %q: %w", item.Title, err)
		}
		if len(bullets) == 0 {
			return nil, fmt.Errorf("parse Insider highlights for %q: no top-level highlights found", item.Title)
		}
		for i := range bullets {
			bullets[i].Link = resolveInsiderLink(item.Link, bullets[i].Link)
		}
		return &InsiderWeeklyIssue{
			Title:       strings.TrimSpace(item.Title),
			Link:        strings.TrimSpace(item.Link),
			PublishedAt: publishedAt,
			Bullets:     bullets,
		}, nil
	}
	return nil, nil
}

func sameLocalDate(a, b time.Time, loc *time.Location) bool {
	a = a.In(loc)
	b = b.In(loc)
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func insiderHighlightBullets(content string, limit int) ([]InsiderWeeklyBullet, error) {
	if limit <= 0 || strings.TrimSpace(content) == "" {
		return nil, nil
	}
	doc, err := xhtml.Parse(strings.NewReader(content))
	if err != nil {
		return nil, err
	}
	heading := findInsiderHighlightsHeading(doc)
	if heading == nil {
		return nil, nil
	}
	var list *xhtml.Node
	for node := nextHTMLNode(heading); node != nil; node = nextHTMLNode(node) {
		if node.Type != xhtml.ElementNode {
			continue
		}
		if isHTMLHeading(node.Data) {
			break
		}
		if node.Data == "ul" {
			list = node
			break
		}
	}
	if list == nil {
		return nil, nil
	}
	bullets := make([]InsiderWeeklyBullet, 0, limit)
	for child := list.FirstChild; child != nil && len(bullets) < limit; child = child.NextSibling {
		if child.Type != xhtml.ElementNode || child.Data != "li" {
			continue
		}
		text := normalizeInsiderText(insiderNodeText(child, true))
		if text != "" {
			bullets = append(bullets, InsiderWeeklyBullet{
				Text: text,
				Link: insiderNodeLink(child, true),
			})
		}
	}
	return bullets, nil
}

func insiderNodeLink(node *xhtml.Node, skipNestedLists bool) string {
	if node == nil {
		return ""
	}
	if skipNestedLists && node.Type == xhtml.ElementNode && (node.Data == "ul" || node.Data == "ol") {
		return ""
	}
	if node.Type == xhtml.ElementNode && node.Data == "a" {
		for _, attr := range node.Attr {
			if attr.Key == "href" {
				return strings.TrimSpace(attr.Val)
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if link := insiderNodeLink(child, skipNestedLists); link != "" {
			return link
		}
	}
	return ""
}

func resolveInsiderLink(issueLink, bulletLink string) string {
	bulletLink = strings.TrimSpace(bulletLink)
	if bulletLink == "" {
		return ""
	}
	ref, err := url.Parse(bulletLink)
	if err != nil {
		return ""
	}
	if !ref.IsAbs() {
		base, err := url.Parse(strings.TrimSpace(issueLink))
		if err != nil {
			return ""
		}
		ref = base.ResolveReference(ref)
	}
	if (ref.Scheme != "http" && ref.Scheme != "https") || ref.Host == "" {
		return ""
	}
	return ref.String()
}

func findInsiderHighlightsHeading(node *xhtml.Node) *xhtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == xhtml.ElementNode && isHTMLHeading(node.Data) {
		text := strings.ToLower(normalizeInsiderText(insiderNodeText(node, false)))
		if strings.Contains(text, "highlights from the bitcoin developer ecosystem") {
			return node
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findInsiderHighlightsHeading(child); found != nil {
			return found
		}
	}
	return nil
}

func nextHTMLNode(node *xhtml.Node) *xhtml.Node {
	if node == nil {
		return nil
	}
	if node.FirstChild != nil {
		return node.FirstChild
	}
	for current := node; current != nil; current = current.Parent {
		if current.NextSibling != nil {
			return current.NextSibling
		}
	}
	return nil
}

func isHTMLHeading(name string) bool {
	return len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6'
}

func insiderNodeText(node *xhtml.Node, skipNestedLists bool) string {
	if node == nil {
		return ""
	}
	if node.Type == xhtml.TextNode {
		return node.Data
	}
	if skipNestedLists && node.Type == xhtml.ElementNode && (node.Data == "ul" || node.Data == "ol") {
		return ""
	}
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(" ")
		b.WriteString(insiderNodeText(child, skipNestedLists))
	}
	return b.String()
}

func normalizeInsiderText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
