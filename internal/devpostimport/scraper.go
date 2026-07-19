package devpostimport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

type Scraper struct {
	Client *http.Client
	Logf   func(string, ...any)
	Delay  time.Duration
}

var (
	nonSlugPattern     = regexp.MustCompile(`[^a-z0-9]+`)
	satoshiPattern     = regexp.MustCompile(`(?i)([0-9][0-9,.]*)\s*(million|m|thousand|k)?\s*(?:satoshis?|sats?)`)
	winnerCountPattern = regexp.MustCompile(`(?i)(\d+)\s+winners?`)
)

func NewScraper() *Scraper {
	return &Scraper{
		Client: &http.Client{Timeout: 30 * time.Second},
		Delay:  250 * time.Millisecond,
	}
}

func (s *Scraper) Scrape(ctx context.Context, sourceURL, conferenceTag string) (*Manifest, error) {
	root, err := normalizeEventURL(sourceURL)
	if err != nil {
		return nil, err
	}
	eventDoc, err := s.fetchDocument(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("fetch event: %w", err)
	}
	manifest := &Manifest{
		Version:       ManifestVersion,
		ScrapedAt:     time.Now().UTC(),
		SourceURL:     root,
		ConferenceTag: strings.TrimSpace(conferenceTag),
		Competition:   parseCompetition(eventDoc),
		Awards:        parseAwards(eventDoc),
		Judges:        parseJudges(eventDoc),
	}

	galleryURL := strings.TrimRight(root, "/") + "/project-gallery"
	projectRefs, err := s.scrapeGallery(ctx, galleryURL)
	if err != nil {
		return nil, err
	}
	for index, ref := range projectRefs {
		if s.Logf != nil {
			s.Logf("project %d/%d: %s", index+1, len(projectRefs), ref.URL)
		}
		if index > 0 && s.Delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.Delay):
			}
		}
		doc, err := s.fetchDocument(ctx, ref.URL)
		if err != nil {
			return nil, fmt.Errorf("fetch project %s: %w", ref.URL, err)
		}
		project := parseProject(doc, ref)
		manifest.Projects = append(manifest.Projects, project)
	}
	mapAwardWinners(manifest)
	return manifest, nil
}

type projectRef struct {
	ID               string
	URL              string
	Title            string
	ShortDescription string
	ThumbnailURL     string
}

func (s *Scraper) scrapeGallery(ctx context.Context, firstURL string) ([]projectRef, error) {
	seenPages := map[string]bool{}
	seenProjects := map[string]bool{}
	var projects []projectRef
	pageURL := firstURL
	for pageURL != "" && !seenPages[pageURL] {
		seenPages[pageURL] = true
		doc, err := s.fetchDocument(ctx, pageURL)
		if err != nil {
			return nil, fmt.Errorf("fetch gallery %s: %w", pageURL, err)
		}
		for _, item := range findAll(doc, func(n *xhtml.Node) bool { return hasClass(n, "gallery-item") }) {
			link := findFirst(item, func(n *xhtml.Node) bool { return hasClass(n, "link-to-software") })
			href := absoluteURL(pageURL, attr(link, "href"))
			if href == "" || seenProjects[href] {
				continue
			}
			seenProjects[href] = true
			projects = append(projects, projectRef{
				ID:               attr(item, "data-software-id"),
				URL:              href,
				Title:            nodeText(findFirst(item, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "h5" })),
				ShortDescription: nodeText(findFirst(item, func(n *xhtml.Node) bool { return hasClass(n, "tagline") })),
				ThumbnailURL:     absoluteURL(pageURL, attr(findFirst(item, func(n *xhtml.Node) bool { return hasClass(n, "software_thumbnail_image") }), "src")),
			})
		}
		next := findFirst(doc, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "a" && attr(n, "rel") == "next" && hasAncestorClass(n, "pagination")
		})
		pageURL = absoluteURL(pageURL, attr(next, "href"))
		if pageURL != "" && s.Delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.Delay):
			}
		}
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("no projects found at %s", firstURL)
	}
	return projects, nil
}

func (s *Scraper) fetchDocument(ctx context.Context, pageURL string) (*xhtml.Node, error) {
	client := s.Client
	if client == nil {
		client = NewScraper().Client
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "btcpp-devpost-importer/1.0 (+https://btcpp.dev)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, err
	}
	return xhtml.Parse(bytes.NewReader(body))
}

func parseCompetition(doc *xhtml.Node) Competition {
	var out Competition
	if script := findByID(doc, "challenge-json-ld"); script != nil {
		var payload struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			StartDate   string `json:"startDate"`
			EndDate     string `json:"endDate"`
			Image       string `json:"image"`
			Location    struct {
				Address struct {
					Locality string `json:"addressLocality"`
					Region   string `json:"addressRegion"`
					Street   string `json:"streetAddress"`
				} `json:"address"`
			} `json:"location"`
		}
		if json.Unmarshal([]byte(rawNodeText(script)), &payload) == nil {
			out.Title = strings.TrimSpace(payload.Name)
			out.Description = stdhtml.UnescapeString(payload.Description)
			out.Start = parseTimePtr(payload.StartDate)
			out.End = parseTimePtr(payload.EndDate)
			out.HeroImage.SourceURL = normalizeRemoteURL(payload.Image)
			out.Location = joinNonEmpty(", ", payload.Location.Address.Locality, payload.Location.Address.Region, payload.Location.Address.Street)
		}
	}
	if out.Title == "" {
		out.Title = nodeText(findFirst(findByID(doc, "challenge-header"), func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "img" }))
	}
	description := findByID(doc, "challenge-description")
	if out.Description == "" && description != nil {
		out.Description = innerHTML(description)
	}
	meta := findFirst(doc, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "meta" && attr(n, "name") == "description"
	})
	out.Tagline = strings.TrimSpace(attr(meta, "content"))
	return out
}

func parseAwards(doc *xhtml.Node) []Award {
	container := findByID(doc, "prizes")
	var out []Award
	for _, node := range findAll(container, func(n *xhtml.Node) bool { return hasClass(n, "prize") && strings.HasPrefix(attr(n, "id"), "prize_") }) {
		descriptionNode := findFirst(findFirst(node, func(n *xhtml.Node) bool { return hasClass(n, "prize-content") }), func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "p" })
		description := nodeText(descriptionNode)
		valueText := nodeText(findFirst(node, func(n *xhtml.Node) bool { return hasClass(n, "prize-value") }))
		winnerText := nodeText(findFirst(node, func(n *xhtml.Node) bool { return hasClass(n, "prize-winners") }))
		award := Award{
			DevpostID:    strings.TrimPrefix(attr(node, "id"), "prize_"),
			Title:        nodeText(findFirst(node, func(n *xhtml.Node) bool { return hasClass(n, "prize-title") })),
			Description:  description,
			ValueText:    firstNonEmpty(satoshiText(description), valueText, description),
			SatoshiValue: parseSatoshiValue(description + " " + valueText),
		}
		if match := winnerCountPattern.FindStringSubmatch(winnerText); len(match) == 2 {
			award.WinnerCount, _ = strconv.Atoi(match[1])
		}
		if award.Title != "" {
			out = append(out, award)
		}
	}
	return out
}

func parseJudges(doc *xhtml.Node) []Person {
	container := findByID(doc, "judges")
	var out []Person
	for _, node := range findAll(container, func(n *xhtml.Node) bool { return hasClass(n, "challenge_judge") }) {
		name := nodeText(findFirst(node, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "strong" }))
		if name == "" {
			continue
		}
		out = append(out, Person{
			Name:    name,
			Company: nodeText(findFirst(node, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "i" })),
			Photo:   Asset{SourceURL: absoluteURL("https://devpost.com", attr(findFirst(node, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "img" }), "src"))},
		})
	}
	return out
}

func parseProject(doc *xhtml.Node, ref projectRef) Project {
	project := Project{
		DevpostID:        ref.ID,
		SourceURL:        ref.URL,
		Slug:             slugFromURL(ref.URL),
		Title:            firstNonEmpty(nodeText(findByID(doc, "app-title")), ref.Title),
		ShortDescription: ref.ShortDescription,
	}
	header := findByID(doc, "software-header")
	if tagline := findFirst(header, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "p" }); tagline != nil {
		project.ShortDescription = nodeText(tagline)
	}
	left := findByID(doc, "app-details-left")
	var story bytes.Buffer
	if left != nil {
		for child := left.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == xhtml.ElementNode && (attr(child, "id") == "gallery" || attr(child, "id") == "built-with") {
				continue
			}
			_ = xhtml.Render(&story, child)
		}
	}
	project.DescriptionHTML = strings.TrimSpace(story.String())

	gallery := findByID(doc, "gallery")
	for _, link := range findAll(gallery, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "a" && attr(n, "data-lightbox") != ""
	}) {
		source := absoluteURL(ref.URL, attr(link, "href"))
		if source == "" {
			continue
		}
		project.Images = append(project.Images, Asset{SourceURL: source, Caption: attr(link, "data-title")})
	}
	if len(project.Images) == 0 && ref.ThumbnailURL != "" {
		project.Images = append(project.Images, Asset{SourceURL: ref.ThumbnailURL})
	}
	if frame := findFirst(gallery, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "iframe" }); frame != nil {
		project.VideoURL = absoluteURL(ref.URL, attr(frame, "src"))
	}
	builtWith := findByID(doc, "built-with")
	for _, tag := range findAll(builtWith, func(n *xhtml.Node) bool { return hasClass(n, "cp-tag") }) {
		if value := nodeText(tag); value != "" {
			project.Tags = appendUnique(project.Tags, value)
		}
	}
	team := findByID(doc, "app-team")
	for _, member := range findAll(team, func(n *xhtml.Node) bool { return hasClass(n, "software-team-member") }) {
		profile := findFirst(member, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "a" && hasClass(n, "user-profile-link")
		})
		name := nodeText(profile)
		if name == "" {
			name = attr(findFirst(member, func(n *xhtml.Node) bool { return hasClass(n, "software-member-photo") }), "title")
		}
		if name != "" {
			project.Members = append(project.Members, Person{Name: name, DevpostURL: absoluteURL(ref.URL, attr(profile, "href"))})
		}
	}
	submissions := findByID(doc, "submissions")
	for _, winner := range findAll(submissions, func(n *xhtml.Node) bool { return hasClass(n, "winner") }) {
		label := strings.TrimSpace(strings.TrimPrefix(nodeText(winner.Parent), "Winner"))
		if label != "" {
			project.AwardTitles = appendUnique(project.AwardTitles, label)
		}
	}
	parseProjectLinks(doc, &project)
	if project.DocsURL == "" {
		project.DocsURL = project.SourceURL
	}
	return project
}

func parseProjectLinks(doc *xhtml.Node, project *Project) {
	for _, node := range findAll(doc, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "a" && hasAncestorAttr(n, "data-role", "software-urls")
	}) {
		href := absoluteURL(project.SourceURL, attr(node, "href"))
		lower := strings.ToLower(href + " " + nodeText(node))
		switch {
		case strings.Contains(lower, "github.com"):
			project.GitHubURL = href
		case strings.Contains(lower, "youtube.com") || strings.Contains(lower, "youtu.be") || strings.Contains(lower, "vimeo.com"):
			project.VideoURL = href
		case strings.Contains(lower, "slide"):
			project.SlidesURL = href
		case strings.Contains(lower, "doc"):
			project.DocsURL = href
		default:
			project.DemoURL = href
		}
	}
}

func mapAwardWinners(manifest *Manifest) {
	byTitle := make(map[string]*Award, len(manifest.Awards))
	for index := range manifest.Awards {
		byTitle[normalizeTitle(manifest.Awards[index].Title)] = &manifest.Awards[index]
	}
	for _, project := range manifest.Projects {
		for _, title := range project.AwardTitles {
			if award := byTitle[normalizeTitle(title)]; award != nil {
				award.Winners = appendUnique(award.Winners, project.Slug)
			}
		}
	}
}

func normalizeEventURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Devpost URL %q", raw)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = "/"
	return parsed.String(), nil
}

func slugFromURL(raw string) string {
	parsed, _ := url.Parse(raw)
	value := path.Base(strings.Trim(parsed.Path, "/"))
	return slugify(value)
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = nonSlugPattern.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func parseSatoshiValue(value string) int64 {
	match := satoshiPattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return 0
	}
	number, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(match[1], ",", ""), " ", ""), 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(match[2]) {
	case "million", "m":
		number *= 1_000_000
	case "thousand", "k":
		number *= 1_000
	}
	return int64(number + 0.5)
}

func satoshiText(value string) string {
	return strings.TrimSpace(satoshiPattern.FindString(value))
}

func parseTimePtr(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}

func findByID(root *xhtml.Node, id string) *xhtml.Node {
	return findFirst(root, func(n *xhtml.Node) bool { return attr(n, "id") == id })
}

func findFirst(root *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, match); found != nil {
			return found
		}
	}
	return nil
}

func findAll(root *xhtml.Node, match func(*xhtml.Node) bool) []*xhtml.Node {
	var out []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n == nil {
			return
		}
		if match(n) {
			out = append(out, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}

func attr(node *xhtml.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func hasClass(node *xhtml.Node, class string) bool {
	for _, value := range strings.Fields(attr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func hasAncestorClass(node *xhtml.Node, class string) bool {
	for current := node; current != nil; current = current.Parent {
		if hasClass(current, class) {
			return true
		}
	}
	return false
}

func hasAncestorAttr(node *xhtml.Node, key, value string) bool {
	for current := node; current != nil; current = current.Parent {
		if attr(current, key) == value {
			return true
		}
	}
	return false
}

func rawNodeText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.TrimSpace(b.String())
}

func nodeText(node *xhtml.Node) string {
	return strings.Join(strings.Fields(rawNodeText(node)), " ")
}

func innerHTML(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	var b bytes.Buffer
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		_ = xhtml.Render(&b, child)
	}
	return strings.TrimSpace(b.String())
}

func absoluteURL(base, ref string) string {
	ref = normalizeRemoteURL(ref)
	if ref == "" {
		return ""
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return baseURL.ResolveReference(parsed).String()
}

func normalizeRemoteURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	return value
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func normalizeTitle(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func joinNonEmpty(separator string, values ...string) string {
	var out []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return strings.Join(out, separator)
}
