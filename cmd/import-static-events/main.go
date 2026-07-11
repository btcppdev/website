package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/spaces"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/speakerphotos"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type staticTalk struct {
	ProposalID  string
	ConfTalkID  string
	RecordingID string
	Event       string
	Index       int
	Anchor      string
	Title       string
	Description string
	Category    string
	Start       time.Time
	End         time.Time
	DurationMin int
	Venue       string
	ClipartPath string
	YouTubeURL  string
	Speaker     staticSpeaker
}

type staticSpeaker struct {
	PersonID   string
	Name       string
	Email      string
	Company    string
	PhotoPath  string
	SourceURL  string
	Twitter    string
	GithubURL  string
	WebsiteURL string
	Nostr      string
}

var fetchClient = &http.Client{Timeout: 6 * time.Second}

func main() {
	envPath := flag.String("env", ".env", "env file to load")
	eventsRaw := flag.String("events", "atx23", "comma-separated events to import")
	fromCSV := flag.String("from-csv", "", "import reviewed CSV rows from one or more comma-separated local paths or URLs")
	atx22URL := flag.String("atx22-url", "https://www.youtube.com/playlist?list=PLHhfnB1Uefkolyc9z03BKsWsnzvZoKYKf", "ATX22 YouTube playlist URL")
	cdmx22URL := flag.String("cdmx22-url", "https://www.youtube.com/playlist?list=PLHhfnB1Uefkor98E-ikci_sUtUKKYYSDA", "CDMX22 YouTube playlist URL")
	berlinURL := flag.String("berlin-url", "https://btcpp.dev/berlin23", "Berlin23 source URL")
	csvPath := flag.String("csv", "", "write parsed rows to CSV path, or '-' for stdout")
	dryRun := flag.Bool("dry-run", false, "parse and report without writing")
	rollback := flag.Bool("rollback", false, "run database import and roll back instead of committing")
	skipUpload := flag.Bool("skip-upload", false, "do not upload photos or cliparts to Spaces")
	flag.Parse()

	env, err := envconfig.Load(*envPath)
	if err != nil {
		log.Fatal(err)
	}
	spaces.Init(env.Spaces)

	var talks []staticTalk
	if strings.TrimSpace(*fromCSV) != "" {
		for _, source := range strings.Split(*fromCSV, ",") {
			source = strings.TrimSpace(source)
			if source == "" {
				continue
			}
			parsed, err := parseReviewedCSV(source)
			if err != nil {
				log.Fatal(err)
			}
			talks = append(talks, parsed...)
		}
	} else {
		for _, event := range strings.Split(*eventsRaw, ",") {
			event = strings.TrimSpace(event)
			if event == "" {
				continue
			}
			switch event {
			case "atx22":
				parsed, err := parseYouTubePlaylistEvent("atx22", *atx22URL)
				if err != nil {
					log.Fatal(err)
				}
				talks = append(talks, parsed...)
			case "cdmx22":
				parsed, err := parseYouTubePlaylistEvent("cdmx22", *cdmx22URL)
				if err != nil {
					log.Fatal(err)
				}
				talks = append(talks, parsed...)
			case "atx23":
				parsed, err := parseATX23("static/atx23/talks.html")
				if err != nil {
					log.Fatal(err)
				}
				talks = append(talks, parsed...)
			case "berlin23":
				parsed, err := parseBerlin23URL(*berlinURL)
				if err != nil {
					log.Fatal(err)
				}
				talks = append(talks, parsed...)
			default:
				log.Fatalf("unsupported event %q", event)
			}
		}
	}

	if len(talks) == 0 {
		log.Fatal("no talks parsed")
	}
	log.Printf("parsed %d static talks", len(talks))
	if *csvPath != "" {
		if err := writeCSV(*csvPath, talks); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *dryRun {
		report(talks)
		return
	}
	if !*skipUpload && !spaces.IsConfigured() {
		log.Fatal("Spaces is not configured; rerun with Spaces env vars or use -skip-upload")
	}

	pool, err := db.Open(context.Background(), env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	tx, err := pool.Begin(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	if err := importTalks(context.Background(), tx, talks, !*skipUpload); err != nil {
		log.Fatal(err)
	}
	if *rollback {
		if err := tx.Rollback(context.Background()); err != nil {
			log.Fatal(err)
		}
		log.Printf("import rolled back")
		return
	}
	if err := tx.Commit(context.Background()); err != nil {
		log.Fatal(err)
	}
	log.Printf("import complete")
}

func report(talks []staticTalk) {
	byEvent := map[string]int{}
	recordings := 0
	speakers := map[string]bool{}
	for _, talk := range talks {
		byEvent[talk.Event]++
		if talk.YouTubeURL != "" {
			recordings++
		}
		speakers[strings.ToLower(talk.Speaker.Name)] = true
	}
	for event, count := range byEvent {
		log.Printf("%s: %d talks", event, count)
	}
	log.Printf("%d unique speaker names, %d recordings", len(speakers), recordings)
}

func writeCSV(path string, talks []staticTalk) error {
	var out io.Writer
	var file *os.File
	if path == "-" {
		out = os.Stdout
	} else {
		var err error
		file, err = os.Create(path)
		if err != nil {
			return fmt.Errorf("create CSV %s: %w", path, err)
		}
		defer file.Close()
		out = file
	}
	w := csv.NewWriter(out)
	defer w.Flush()
	header := []string{
		"event",
		"talk_index",
		"talk_anchor",
		"proposal_id",
		"conf_talk_id",
		"speaker_person_id",
		"speaker_name",
		"speaker_email",
		"speaker_company",
		"speaker_twitter",
		"speaker_github_url",
		"speaker_website_url",
		"speaker_nostr",
		"speaker_photo_source",
		"talk_title",
		"talk_description",
		"talk_category",
		"talk_type",
		"venue",
		"scheduled_start",
		"scheduled_end",
		"duration_min",
		"clipart_source",
		"youtube_url",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, talk := range talks {
		proposalID := stableID(fmt.Sprintf("static-event:%s:proposal:%03d:%s", talk.Event, talk.Index, talk.Anchor))
		confTalkID := stableID("static-event:" + talk.Event + ":conf-talk:" + proposalID)
		personID := staticSpeakerID(talk)
		end := talk.Start.Add(time.Duration(talk.DurationMin) * time.Minute)
		if !talk.End.IsZero() {
			end = talk.End
		}
		startText := ""
		endText := ""
		durationText := ""
		if !talk.Start.IsZero() {
			startText = talk.Start.Format(time.RFC3339)
			endText = end.Format(time.RFC3339)
			durationText = fmt.Sprintf("%d", int(end.Sub(talk.Start).Minutes()))
		}
		row := []string{
			talk.Event,
			fmt.Sprintf("%d", talk.Index),
			talk.Anchor,
			proposalID,
			confTalkID,
			personID,
			talk.Speaker.Name,
			"",
			talk.Speaker.Company,
			talk.Speaker.Twitter,
			talk.Speaker.GithubURL,
			talk.Speaker.WebsiteURL,
			talk.Speaker.Nostr,
			talk.Speaker.PhotoPath,
			talk.Title,
			talk.Description,
			talk.Category,
			talkTypeFor(talk.Category, talk.Title),
			talk.Venue,
			startText,
			endText,
			durationText,
			talk.ClipartPath,
			talk.YouTubeURL,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	if err := w.Error(); err != nil {
		return err
	}
	if file != nil {
		log.Printf("wrote %d rows to %s", len(talks), path)
	}
	return nil
}

func parseReviewedCSV(source string) ([]staticTalk, error) {
	raw, err := readCSVSource(source)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(string(raw)))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV %s: %w", source, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s is empty", source)
	}
	header := map[string]int{}
	for i, name := range rows[0] {
		header[strings.TrimSpace(name)] = i
	}
	get := func(row []string, name string) string {
		i, ok := header[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	required := []string{"event", "talk_index", "talk_anchor", "speaker_name", "speaker_email", "speaker_company", "speaker_twitter", "speaker_github_url", "speaker_website_url", "speaker_nostr", "speaker_photo_source", "talk_title", "talk_description", "talk_category", "talk_type", "venue", "scheduled_start", "scheduled_end", "duration_min", "clipart_source", "youtube_url"}
	for _, name := range required {
		if _, ok := header[name]; !ok {
			return nil, fmt.Errorf("%s missing required column %q", source, name)
		}
	}
	out := make([]staticTalk, 0, len(rows)-1)
	for rowNum, row := range rows[1:] {
		if len(row) == 0 || strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		index, err := parseOptionalInt(get(row, "talk_index"))
		if err != nil {
			return nil, fmt.Errorf("%s row %d talk_index: %w", source, rowNum+2, err)
		}
		start, err := parseOptionalTime(get(row, "scheduled_start"))
		if err != nil {
			return nil, fmt.Errorf("%s row %d scheduled_start: %w", source, rowNum+2, err)
		}
		end, err := parseOptionalTime(get(row, "scheduled_end"))
		if err != nil {
			return nil, fmt.Errorf("%s row %d scheduled_end: %w", source, rowNum+2, err)
		}
		duration, err := parseOptionalInt(get(row, "duration_min"))
		if err != nil {
			return nil, fmt.Errorf("%s row %d duration_min: %w", source, rowNum+2, err)
		}
		talk := staticTalk{
			ProposalID:  get(row, "proposal_id"),
			ConfTalkID:  get(row, "conf_talk_id"),
			Event:       get(row, "event"),
			Index:       index,
			Anchor:      get(row, "talk_anchor"),
			Title:       get(row, "talk_title"),
			Description: get(row, "talk_description"),
			Category:    get(row, "talk_category"),
			Start:       start,
			End:         end,
			DurationMin: duration,
			Venue:       get(row, "venue"),
			ClipartPath: get(row, "clipart_source"),
			YouTubeURL:  get(row, "youtube_url"),
			Speaker: staticSpeaker{
				PersonID:   get(row, "speaker_person_id"),
				Name:       get(row, "speaker_name"),
				Email:      get(row, "speaker_email"),
				Company:    get(row, "speaker_company"),
				Twitter:    get(row, "speaker_twitter"),
				GithubURL:  get(row, "speaker_github_url"),
				WebsiteURL: get(row, "speaker_website_url"),
				Nostr:      get(row, "speaker_nostr"),
				PhotoPath:  get(row, "speaker_photo_source"),
			},
		}
		if talk.Event == "" {
			return nil, fmt.Errorf("%s row %d missing event", source, rowNum+2)
		}
		if talk.Title == "" {
			return nil, fmt.Errorf("%s row %d missing talk_title", source, rowNum+2)
		}
		if talk.Speaker.Name == "" {
			talk.Speaker.Name = "bitcoin++"
		}
		out = append(out, talk)
	}
	log.Printf("read %d reviewed rows from %s", len(out), source)
	return out, nil
}

func readCSVSource(source string) ([]byte, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		text, err := fetchText(source)
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	}
	return os.ReadFile(source)
}

func parseOptionalInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}

func parseOptionalTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return parseSchedDate(raw)
}

func parseATX23(path string) ([]staticTalk, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc := string(raw)
	articleRe := regexp.MustCompile(`(?is)<article\b[^>]*\bid="([^"]+)"[^>]*>(.*?)</article>`)
	matches := articleRe.FindAllStringSubmatch(doc, -1)
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return nil, err
	}
	out := make([]staticTalk, 0, len(matches))
	for i, m := range matches {
		block := m[2]
		start, err := parseATXTime(firstMatch(block, `(?is)<time\b[^>]*>(.*?)</time>`), loc)
		if err != nil {
			return nil, fmt.Errorf("article %s time: %w", m[1], err)
		}
		category := cleanHTML(firstMatch(block, `(?is)<span\b[^>]*class="[^"]*bg-gray-50[^"]*"[^>]*>(.*?)</span>`))
		title := cleanHTML(firstMatch(block, `(?is)<h3\b[^>]*>(.*?)</h3>`))
		desc := cleanHTML(firstMatch(block, `(?is)<p\b[^>]*class="[^"]*mt-5[^"]*"[^>]*>(.*?)</p>`))
		speakerBlock := firstMatch(block, `(?is)<div\b[^>]*class="[^"]*border-t[^"]*"[^>]*>(.*?)</div>\s*</div>\s*$`)
		speakerName := cleanHTML(firstMatch(speakerBlock, `(?is)<p\b[^>]*font-semibold[^>]*>(.*?)</p>`))
		company := cleanHTML(firstMatch(speakerBlock, `(?is)<p\b[^>]*text-gray-600[^>]*>(.*?)</p>`))
		sourceURL := firstNonYouTubeHref(speakerBlock)
		speaker := staticSpeaker{
			Name:      speakerName,
			Company:   company,
			PhotoPath: firstMatch(speakerBlock, `(?is)<img\b[^>]*src="([^"]+)"`),
			SourceURL: sourceURL,
		}
		classifySpeakerURL(&speaker)
		if speaker.Name == "" {
			speaker.Name = "bitcoin++"
		}
		if title == "" {
			return nil, fmt.Errorf("article %s has no title", m[1])
		}
		out = append(out, staticTalk{
			Event:       "atx23",
			Index:       i + 1,
			Anchor:      m[1],
			Title:       title,
			Description: desc,
			Category:    category,
			Start:       start,
			DurationMin: durationFor(category, title),
			Venue:       venueFor(category, title),
			ClipartPath: firstMatch(block, `(?is)<img\b[^>]*src="([^"]*/img/talks/[^"]+)"`),
			YouTubeURL:  firstMatch(block, `(?is)href="(https://(?:www\.)?(?:youtube\.com|youtu\.be)[^"]+)"`),
			Speaker:     speaker,
		})
	}
	return out, nil
}

type youtubePlayerResponse struct {
	VideoDetails struct {
		VideoID          string `json:"videoId"`
		Title            string `json:"title"`
		ShortDescription string `json:"shortDescription"`
		Thumbnail        struct {
			Thumbnails []struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"thumbnails"`
		} `json:"thumbnail"`
	} `json:"videoDetails"`
}

func parseYouTubePlaylistEvent(eventTag, sourceURL string) ([]staticTalk, error) {
	doc, err := fetchText(sourceURL)
	if err != nil {
		return nil, err
	}
	videoIDs := youtubeVideoIDsInOrder(doc)
	if len(videoIDs) == 0 {
		return nil, fmt.Errorf("no YouTube video IDs found in %s", sourceURL)
	}
	out := make([]staticTalk, 0, len(videoIDs))
	for i, videoID := range videoIDs {
		watchURL := "https://www.youtube.com/watch?v=" + videoID
		watch, err := fetchText(watchURL)
		if err != nil {
			return nil, err
		}
		player, err := parseYouTubePlayerResponse(watch)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", watchURL, err)
		}
		title := strings.TrimSpace(player.VideoDetails.Title)
		desc := strings.TrimSpace(player.VideoDetails.ShortDescription)
		talkTitle, speaker := inferATX22Speaker(title, desc)
		if speaker.Name == "" {
			speaker.Name = ""
		}
		talk := staticTalk{
			Event:       eventTag,
			Index:       i + 1,
			Anchor:      videoID,
			Title:       talkTitle,
			Description: desc,
			Category:    "YouTube Playlist",
			Venue:       "",
			ClipartPath: bestYouTubeThumbnail(player),
			YouTubeURL:  watchURL,
			Speaker:     speaker,
		}
		if schedURL := firstSchedEventURL(desc); schedURL != "" {
			if err := enrichFromSchedEvent(&talk, schedURL); err != nil {
				log.Printf("warning: %s sched metadata skipped: %v", watchURL, err)
			}
		}
		out = append(out, talk)
	}
	return out, nil
}

func firstSchedEventURL(desc string) string {
	re := regexp.MustCompile(`https://[A-Za-z0-9.-]+\.sched\.com/event/[^\s)"<>]+`)
	raw := re.FindString(desc)
	if raw == "" {
		return ""
	}
	return strings.TrimRight(raw, ".,;")
}

func enrichFromSchedEvent(talk *staticTalk, schedURL string) error {
	doc, err := fetchText(schedURL)
	if err != nil {
		return err
	}
	ev, err := parseSchemaEvent(doc)
	if err != nil {
		return err
	}
	start, err := parseSchedDate(ev.StartDate)
	if err != nil {
		return fmt.Errorf("start %q: %w", ev.StartDate, err)
	}
	end, err := parseSchedDate(ev.EndDate)
	if err != nil {
		return fmt.Errorf("end %q: %w", ev.EndDate, err)
	}
	talk.Start = start
	talk.End = end
	talk.DurationMin = int(end.Sub(start).Minutes())
	if talk.DurationMin <= 0 {
		talk.DurationMin = 30
	}
	if talk.Venue == "" {
		talk.Venue = schedLocationName(ev.Location)
	}
	return nil
}

func parseSchedDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04:05-0700", raw)
}

func schedLocationName(location map[string]any) string {
	if len(location) == 0 {
		return ""
	}
	if name, ok := location["name"].(string); ok {
		return strings.TrimSpace(name)
	}
	if address, ok := location["address"].(map[string]any); ok {
		if street, ok := address["streetAddress"].(string); ok {
			return strings.TrimSpace(street)
		}
	}
	return ""
}

func fetchText(sourceURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request %s: %w", sourceURL, err)
	}
	req.Header.Set("User-Agent", "btcpp-web-static-import/1.0")
	resp, err := fetchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", sourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %s: status %s", sourceURL, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", sourceURL, err)
	}
	return string(raw), nil
}

func youtubeVideoIDsInOrder(doc string) []string {
	re := regexp.MustCompile(`"videoId":"([^"]+)"`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}

func parseYouTubePlayerResponse(doc string) (youtubePlayerResponse, error) {
	raw := extractJSONObjectAfter(doc, "ytInitialPlayerResponse")
	if raw == "" {
		return youtubePlayerResponse{}, fmt.Errorf("ytInitialPlayerResponse not found")
	}
	var out youtubePlayerResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return youtubePlayerResponse{}, fmt.Errorf("parse ytInitialPlayerResponse: %w", err)
	}
	return out, nil
}

func inferATX22Speaker(title, desc string) (string, staticSpeaker) {
	title = strings.TrimSpace(title)
	desc = strings.TrimSpace(desc)
	talkTitle := title
	var names []string
	var speaker staticSpeaker
	if before, after, ok := strings.Cut(title, " w/ "); ok {
		talkTitle = strings.TrimSpace(before)
		names = append(names, strings.TrimSpace(after))
	}
	for _, line := range strings.Split(desc, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "http") {
			continue
		}
		if before, after, ok := strings.Cut(line, " (https://"); ok && plausibleSpeakerLabel(before) {
			label := strings.TrimSpace(before)
			linkTail, _, _ := strings.Cut(after, ")")
			link := "https://" + strings.TrimSpace(linkTail)
			if !coveredByExistingName(names, label) {
				names = append(names, label)
			}
			tmp := staticSpeaker{SourceURL: link}
			classifySpeakerURL(&tmp)
			if speaker.Twitter == "" {
				speaker.Twitter = tmp.Twitter
			}
			if speaker.GithubURL == "" {
				speaker.GithubURL = tmp.GithubURL
			}
			if speaker.WebsiteURL == "" {
				speaker.WebsiteURL = tmp.WebsiteURL
			}
			if speaker.Nostr == "" {
				speaker.Nostr = tmp.Nostr
			}
			continue
		}
		label, link, ok := strings.Cut(line, ":")
		if !ok {
			if len(names) == 0 {
				if before, _, found := strings.Cut(line, " ("); found {
					if plausibleSpeakerLabel(before) && !coveredByExistingName(names, before) {
						names = append(names, strings.TrimSpace(before))
					}
				}
			}
			continue
		}
		label = strings.TrimSpace(label)
		label = strings.TrimPrefix(label, "With ")
		link = strings.TrimSpace(link)
		if !plausibleSpeakerLabel(label) {
			continue
		}
		if !coveredByExistingName(names, label) {
			names = append(names, label)
		}
		tmp := staticSpeaker{SourceURL: link}
		classifySpeakerURL(&tmp)
		if speaker.Twitter == "" {
			speaker.Twitter = tmp.Twitter
		}
		if speaker.GithubURL == "" {
			speaker.GithubURL = tmp.GithubURL
		}
		if speaker.WebsiteURL == "" {
			speaker.WebsiteURL = tmp.WebsiteURL
		}
		if speaker.Nostr == "" {
			speaker.Nostr = tmp.Nostr
		}
	}
	speaker.Name = strings.Join(names, " + ")
	return talkTitle, speaker
}

func plausibleSpeakerLabel(label string) bool {
	label = strings.TrimSpace(label)
	lower := strings.ToLower(label)
	if label == "" || len(label) > 64 {
		return false
	}
	switch lower {
	case "https", "http", "setup", "replit", "slides", "github", "repo", "source", "website", "twitter", "x", "speaker twitter", "event schedule", "btcpp website", "btcpp twitter":
		return false
	}
	if strings.Contains(lower, "btcpp2022") || strings.Contains(lower, "sched.com") {
		return false
	}
	return len(strings.Fields(label)) <= 5
}

func coveredByExistingName(vals []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return true
	}
	for _, val := range vals {
		val = strings.TrimSpace(val)
		if strings.EqualFold(val, needle) {
			return true
		}
		if len(needle) >= 3 && strings.Contains(strings.ToLower(val), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func bestYouTubeThumbnail(player youtubePlayerResponse) string {
	thumbs := player.VideoDetails.Thumbnail.Thumbnails
	if len(thumbs) == 0 {
		if player.VideoDetails.VideoID == "" {
			return ""
		}
		return "https://i.ytimg.com/vi/" + player.VideoDetails.VideoID + "/hqdefault.jpg"
	}
	best := thumbs[0]
	for _, thumb := range thumbs[1:] {
		if thumb.Width*thumb.Height > best.Width*best.Height {
			best = thumb
		}
	}
	return best.URL
}

func parseBerlin23URL(sourceURL string) ([]staticTalk, error) {
	doc, err := fetchText(sourceURL)
	if err != nil {
		return nil, err
	}
	return parseBerlin23HTML(doc)
}

type schemaEvent struct {
	Name        string            `json:"name"`
	StartDate   string            `json:"startDate"`
	EndDate     string            `json:"endDate"`
	Performer   []schemaPerformer `json:"performer"`
	SubEventRaw json.RawMessage   `json:"subEvent"`
	SubEvent    []schemaEvent     `json:"-"`
	Location    map[string]any    `json:"location"`
	Image       string            `json:"image"`
	Organizer   map[string]string `json:"organizer"`
	Context     string            `json:"@context"`
	Type        string            `json:"@type"`
}

type schemaPerformer struct {
	Name string `json:"name"`
}

func parseBerlin23HTML(doc string) ([]staticTalk, error) {
	root, err := parseSchemaEvent(doc)
	if err != nil {
		return nil, err
	}
	speakers := parseBerlinSpeakers(doc)
	agenda := parseBerlinAgendaLinks(doc)
	out := make([]staticTalk, 0, len(root.SubEvent))
	for i, ev := range root.SubEvent {
		title := strings.TrimSpace(ev.Name)
		if title == "" {
			continue
		}
		start, err := time.Parse(time.RFC3339, ev.StartDate)
		if err != nil {
			return nil, fmt.Errorf("berlin23 %q start: %w", title, err)
		}
		end, err := time.Parse(time.RFC3339, ev.EndDate)
		if err != nil {
			return nil, fmt.Errorf("berlin23 %q end: %w", title, err)
		}
		sp := staticSpeaker{Name: "bitcoin++", Company: "bitcoin++"}
		if len(ev.Performer) > 0 && strings.TrimSpace(ev.Performer[0].Name) != "" {
			name := strings.TrimSpace(ev.Performer[0].Name)
			sp = speakers[strings.ToLower(name)]
			if sp.Name == "" {
				sp.Name = name
			}
		}
		link := agenda[title]
		out = append(out, staticTalk{
			Event:       "berlin23",
			Index:       i + 1,
			Anchor:      firstNonEmpty(link.Anchor, slug(title)),
			Title:       title,
			Description: "",
			Category:    "Main Stage",
			Start:       start,
			End:         end,
			DurationMin: int(end.Sub(start).Minutes()),
			Venue:       "main",
			YouTubeURL:  link.YouTubeURL,
			Speaker:     sp,
		})
	}
	return out, nil
}

func parseSchemaEvent(doc string) (schemaEvent, error) {
	raw := firstMatch(doc, `(?is)<script\b[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)
	if raw == "" {
		return schemaEvent{}, fmt.Errorf("berlin23 source has no JSON-LD event data")
	}
	var root schemaEvent
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return schemaEvent{}, fmt.Errorf("parse Berlin23 JSON-LD: %w", err)
	}
	if len(root.SubEventRaw) == 0 || string(root.SubEventRaw) == "null" {
		return root, nil
	}
	if root.SubEventRaw[0] == '[' {
		if err := json.Unmarshal(root.SubEventRaw, &root.SubEvent); err != nil {
			return schemaEvent{}, fmt.Errorf("parse Berlin23 subEvent array: %w", err)
		}
		return root, nil
	}
	var one schemaEvent
	if err := json.Unmarshal(root.SubEventRaw, &one); err != nil {
		return schemaEvent{}, fmt.Errorf("parse Berlin23 subEvent: %w", err)
	}
	root.SubEvent = []schemaEvent{one}
	return root, nil
}

func parseBerlinSpeakers(doc string) map[string]staticSpeaker {
	out := map[string]staticSpeaker{}
	section := firstMatch(doc, `(?is)<section id="speakers">(.*?)</section>`)
	if section == "" {
		return out
	}
	chunks := strings.Split(section, `<img class="mx-auto h-56 w-56 rounded-full object-cover"`)
	for _, chunk := range chunks[1:] {
		card := `<img class="mx-auto h-56 w-56 rounded-full object-cover"` + chunk
		name := cleanHTML(firstMatch(card, `(?is)<h3\b[^>]*>(.*?)</h3>`))
		if name == "" {
			continue
		}
		sp := staticSpeaker{
			Name:      name,
			Company:   cleanHTML(firstMatch(card, `(?is)<p\b[^>]*text-sm leading-6 text-gray-600[^>]*>(.*?)</p>`)),
			PhotoPath: firstMatch(card, `(?is)<img\b[^>]*src="([^"]+)"`),
		}
		hrefRe := regexp.MustCompile(`(?is)<a\b[^>]*href="([^"]+)"`)
		for _, m := range hrefRe.FindAllStringSubmatch(card, -1) {
			if len(m) < 2 {
				continue
			}
			candidate := strings.TrimSpace(html.UnescapeString(m[1]))
			if candidate == "" {
				continue
			}
			tmp := staticSpeaker{SourceURL: candidate}
			classifySpeakerURL(&tmp)
			if tmp.Twitter != "" && sp.Twitter == "" {
				sp.Twitter = tmp.Twitter
			}
			if tmp.GithubURL != "" && sp.GithubURL == "" {
				sp.GithubURL = tmp.GithubURL
			}
			if tmp.Nostr != "" && sp.Nostr == "" {
				sp.Nostr = tmp.Nostr
			}
			if tmp.WebsiteURL != "" && sp.WebsiteURL == "" {
				sp.WebsiteURL = tmp.WebsiteURL
			}
		}
		out[strings.ToLower(name)] = sp
	}
	return out
}

type agendaLink struct {
	Anchor     string
	YouTubeURL string
}

func parseBerlinAgendaLinks(doc string) map[string]agendaLink {
	out := map[string]agendaLink{}
	for _, block := range strings.Split(doc, `<li class="flex items-start gap-3 px-4 py-3 hover:bg-gray-50">`)[1:] {
		title := cleanHTML(firstMatch(block, `(?is)<a\b[^>]*href="/berlin23/agenda#[^"]+"[^>]*>(.*?)</a>`))
		anchor := firstMatch(block, `(?is)<a\b[^>]*href="/berlin23/agenda#([^"]+)"`)
		if title == "" {
			continue
		}
		out[title] = agendaLink{
			Anchor:     anchor,
			YouTubeURL: firstMatch(block, `(?is)href="(https://youtu\.be/[^"]+)"`),
		}
	}
	return out
}

func parseATXTime(raw string, loc *time.Location) (time.Time, error) {
	raw = cleanHTML(raw)
	raw = strings.ReplaceAll(raw, "Sept.", "Sep.")
	return time.ParseInLocation("Mon. Jan 2, 2006 @ 3:04 pm", raw, loc)
}

func importTalks(ctx context.Context, tx pgx.Tx, talks []staticTalk, uploadAssets bool) error {
	confIDs := map[string]string{}
	for _, talk := range talks {
		if _, ok := confIDs[talk.Event]; ok {
			continue
		}
		var confID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM conferences WHERE tag = $1`, talk.Event).Scan(&confID); err != nil {
			return fmt.Errorf("conference %s must exist before import: %w", talk.Event, err)
		}
		confIDs[talk.Event] = confID
	}
	if err := importStaticSponsors(ctx, tx, confIDs); err != nil {
		return err
	}
	if err := importStaticPeople(ctx, tx); err != nil {
		return err
	}

	if err := validateMultiSpeakerEmails(ctx, tx, talks); err != nil {
		return err
	}

	for _, talk := range talks {
		confID := confIDs[talk.Event]
		proposalID := firstNonEmpty(talk.ProposalID, stableID(fmt.Sprintf("static-event:%s:proposal:%03d:%s", talk.Event, talk.Index, talk.Anchor)))
		confTalkID := firstNonEmpty(talk.ConfTalkID, stableID("static-event:"+talk.Event+":conf-talk:"+proposalID))
		recordingID := firstNonEmpty(talk.RecordingID, stableID("static-event:"+talk.Event+":recording:"+proposalID))

		if err := upsertProposal(ctx, tx, proposalID, confID, talk); err != nil {
			return err
		}
		clipart, err := uploadTalkClipart(talk, uploadAssets)
		if err != nil {
			return err
		}
		if err := upsertConfTalk(ctx, tx, confTalkID, confID, proposalID, clipart, talk); err != nil {
			return err
		}
		if err := clearProposalSpeakers(ctx, tx, proposalID); err != nil {
			return err
		}

		importedSpeakers, err := importTalkSpeakers(ctx, tx, confID, proposalID, talk, uploadAssets)
		if err != nil {
			return err
		}
		if talk.YouTubeURL != "" {
			if err := upsertRecording(ctx, tx, recordingID, confTalkID, talk); err != nil {
				return err
			}
		}
		log.Printf("imported %s: %s (%s)", talk.Event, talk.Title, strings.Join(importedSpeakers, ", "))
	}
	return nil
}

type staticSponsor struct {
	Name    string
	Website string
	Logo    string
	Level   string
	Label   string
	Status  string
}

type staticPersonSeed struct {
	Email   string
	Name    string
	Company string
	Twitter string
	Github  string
}

var staticPeople = []staticPersonSeed{
	{Email: "btcplusplus@arik.io", Name: "Arik", Company: "Spiral", Twitter: "arikaleph"},
	{Email: "conor@spiral.xyz", Name: "Conor Okus", Company: "Spiral", Twitter: "ConorOkus"},
	{Email: "jkczyz@spiral.xyz", Name: "Jeff Czyz", Company: "Spiral", Twitter: "jkczyz", Github: "https://github.com/jkczyz"},
	{Email: "nate@voltage.cloud", Name: "Nate", Company: "Voltage"},
	{Email: "paul@paul.lol", Name: "Paul", Company: "Mutiny", Twitter: "futurepaul"},
	{Email: "burak@bitmatrix.app", Name: "Burak", Company: "Bitmatrix", Twitter: "brqgoo"},
}

var atx23Sponsors = []staticSponsor{
	{Name: "Mempool.space", Website: "https://mempool.space/", Logo: "/static/atx23/img/mempool.svg"},
	{Name: "C=", Website: "https://cequals.xyz/", Logo: "/static/atx23/img/ceq.svg"},
	{Name: "Spiral", Website: "https://spiral.xyz/", Logo: "/static/atx23/img/spiral.svg"},
	{Name: "Base58", Website: "https://base58.school", Logo: "/static/atx23/img/base58.svg"},
	{Name: "Unchained", Website: "https://unchained.com/", Logo: "/static/atx23/img/unchained.svg"},
	{Name: "Fedi", Website: "https://www.fedi.xyz/", Logo: "/static/atx23/img/fedi.svg"},
	{Name: "Trammell Venture Partners", Website: "https://tvp.fund/", Logo: "/static/atx23/img/trammel.svg"},
	{Name: "Fulgur Ventures", Website: "https://fulgur.ventures/", Logo: "/static/atx23/img/fulgar.svg"},
	{Name: "Rigly", Website: "https://rigly.io/", Logo: "/static/atx23/img/rigly.svg"},
	{Name: "OpenNode", Website: "https://www.opennode.com/", Logo: "/static/atx23/img/opennode.svg"},
	{Name: "OpenSats", Website: "https://opensats.org/", Logo: "/static/atx23/img/opensats.svg"},
	{Name: "Wasabi Wallet", Website: "https://wasabiwallet.io/", Logo: "/static/atx23/img/wasabi.svg"},
	{Name: "ATL BitLab", Website: "https://atlbitlab.com/", Logo: "/static/atx23/img/atlbitlab.jpg"},
	{Name: "PlebLab", Website: "https://www.pleblab.com/", Logo: "/static/atx23/img/pleblab.jpg"},
}

func importStaticPeople(ctx context.Context, tx pgx.Tx) error {
	for _, person := range staticPeople {
		if err := upsertStaticPersonSeed(ctx, tx, person); err != nil {
			return err
		}
	}
	return nil
}

func upsertStaticPersonSeed(ctx context.Context, tx pgx.Tx, person staticPersonSeed) error {
	email := canonicalEmail(person.Email)
	if email == "" {
		return nil
	}
	personID, err := resolveStaticPersonSeedID(ctx, tx, person)
	if err != nil {
		return err
	}
	if personID == "" {
		personID = stableID("static-event:speaker-email:" + email)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO people (
			id, name, email, norm_photo_path, phone, signal, telegram,
			twitter_handle, nostr, github_url, instagram, linkedin, leetcode,
			website_url, company, org_logo_path, bio, avail_to_hire,
			looking_to_hire, tshirt
		)
		VALUES (
			$1::uuid, $2, $3, '', '', '', '',
			$4, '', $5, '', '', '',
			'', $6, '', '', false, false, ''
		)
		ON CONFLICT (id) DO UPDATE SET
			email = CASE WHEN people.email IS NULL OR people.email = '' THEN EXCLUDED.email ELSE people.email END,
			twitter_handle = CASE WHEN people.twitter_handle = '' THEN EXCLUDED.twitter_handle ELSE people.twitter_handle END,
			github_url = CASE WHEN people.github_url = '' THEN EXCLUDED.github_url ELSE people.github_url END,
			company = CASE WHEN people.company = '' THEN EXCLUDED.company ELSE people.company END
	`, personID, person.Name, email, person.Twitter, person.Github, person.Company)
	if err != nil {
		return fmt.Errorf("upsert static person %q: %w", email, err)
	}
	return nil
}

func resolveStaticPersonSeedID(ctx context.Context, tx pgx.Tx, person staticPersonSeed) (string, error) {
	queries := []struct {
		label string
		sql   string
		arg   string
	}{
		{`twitter`, `SELECT id::text FROM people WHERE lower(twitter_handle) = lower($1) LIMIT 1`, person.Twitter},
		{`github`, `SELECT id::text FROM people WHERE lower(github_url) = lower($1) LIMIT 1`, person.Github},
		{`email`, `SELECT id::text FROM people WHERE lower(email::text) = lower($1) LIMIT 1`, canonicalEmail(person.Email)},
	}
	for _, q := range queries {
		if strings.TrimSpace(q.arg) == "" {
			continue
		}
		var id string
		err := tx.QueryRow(ctx, q.sql, q.arg).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != pgx.ErrNoRows {
			return "", fmt.Errorf("find static person by %s %q: %w", q.label, q.arg, err)
		}
	}
	return "", nil
}

func importStaticSponsors(ctx context.Context, tx pgx.Tx, confIDs map[string]string) error {
	if confIDs["atx23"] == "" {
		return nil
	}
	for _, sponsor := range atx23Sponsors {
		if sponsor.Level == "" {
			sponsor.Level = "Bronze"
		}
		if sponsor.Label == "" {
			sponsor.Label = "Sponsors"
		}
		if sponsor.Status == "" {
			sponsor.Status = "Paid"
		}
		orgID, err := upsertStaticSponsorOrg(ctx, tx, sponsor)
		if err != nil {
			return err
		}
		if err := upsertStaticSponsorship(ctx, tx, confIDs["atx23"], orgID, sponsor); err != nil {
			return err
		}
		log.Printf("imported atx23 sponsor: %s", sponsor.Name)
	}
	return nil
}

func upsertStaticSponsorOrg(ctx context.Context, tx pgx.Tx, sponsor staticSponsor) (string, error) {
	var orgID string
	err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM organizations
		WHERE lower(name) = lower($1)
		LIMIT 1
	`, sponsor.Name).Scan(&orgID)
	if err != nil && err != pgx.ErrNoRows {
		return "", fmt.Errorf("find atx23 sponsor org %q: %w", sponsor.Name, err)
	}
	if orgID != "" {
		_, err = tx.Exec(ctx, `
			UPDATE organizations
			SET website_url = CASE WHEN website_url = '' THEN $2 ELSE website_url END,
				logo_light_url = CASE WHEN logo_light_url = '' THEN $3 ELSE logo_light_url END,
				logo_dark_url = CASE WHEN logo_dark_url = '' THEN $3 ELSE logo_dark_url END
			WHERE id = $1::uuid
		`, orgID, sponsor.Website, sponsor.Logo)
		if err != nil {
			return "", fmt.Errorf("update atx23 sponsor org %q: %w", sponsor.Name, err)
		}
		return orgID, nil
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO organizations (
			id, name, tagline, logo_light_url, logo_dark_url, email, website_url,
			linkedin_url, instagram_url, youtube_url, github_url, twitter_handle,
			nostr, matrix, hiring, notes
		)
		VALUES (
			$1::uuid, $2, '', $3, $3, NULL, $4,
			'', '', '', '', '',
			'', '', false, ''
		)
		RETURNING id::text
	`, stableID("static-event:atx23:org:"+slug(sponsor.Name)), sponsor.Name, sponsor.Logo, sponsor.Website).Scan(&orgID)
	if err != nil {
		return "", fmt.Errorf("upsert atx23 sponsor org %q: %w", sponsor.Name, err)
	}
	return orgID, nil
}

func upsertStaticSponsorship(ctx context.Context, tx pgx.Tx, confID, orgID string, sponsor staticSponsor) error {
	var sponsorshipID string
	err := tx.QueryRow(ctx, `
		SELECT s.id::text
		FROM sponsorships s
		JOIN sponsorships_conferences sc ON sc.sponsorship_id = s.id
		WHERE sc.conference_id = $1::uuid
			AND s.organization_id = $2::uuid
			AND s.archived_at IS NULL
		LIMIT 1
	`, confID, orgID).Scan(&sponsorshipID)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("find atx23 sponsorship %q: %w", sponsor.Name, err)
	}
	if sponsorshipID == "" {
		sponsorshipID = stableID("static-event:atx23:sponsorship:" + slug(sponsor.Name))
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO sponsorships (
			id, organization_id, name, level, label, status, is_vendor, notes, archived_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, false, '', NULL)
		ON CONFLICT (id) DO UPDATE SET
			organization_id = EXCLUDED.organization_id,
			name = EXCLUDED.name,
			level = CASE WHEN sponsorships.level = '' THEN EXCLUDED.level ELSE sponsorships.level END,
			label = CASE WHEN sponsorships.label = '' THEN EXCLUDED.label ELSE sponsorships.label END,
			status = CASE WHEN sponsorships.status = '' THEN EXCLUDED.status ELSE sponsorships.status END,
			archived_at = NULL
	`, sponsorshipID, orgID, sponsor.Name, sponsor.Level, sponsor.Label, sponsor.Status)
	if err != nil {
		return fmt.Errorf("upsert atx23 sponsorship %q: %w", sponsor.Name, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO sponsorships_conferences (sponsorship_id, conference_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, sponsorshipID, confID)
	if err != nil {
		return fmt.Errorf("link atx23 sponsorship %q: %w", sponsor.Name, err)
	}
	return nil
}

type existingPerson struct {
	ID      string
	Name    string
	Photo   string
	Company string
	Email   string
}

func importTalkSpeakers(ctx context.Context, tx pgx.Tx, confID, proposalID string, talk staticTalk, uploadAssets bool) ([]string, error) {
	emails := splitSpeakerEmails(talk.Speaker.Email)
	if len(emails) > 1 {
		names := make([]string, 0, len(emails))
		for _, email := range emails {
			person, err := findExistingPersonByEmail(ctx, tx, email)
			if err != nil {
				return nil, fmt.Errorf("%s %q speaker %s: %w", talk.Event, talk.Title, email, err)
			}
			speakerTalk := talk
			speakerTalk.Speaker = staticSpeaker{
				PersonID: person.ID,
				Name:     person.Name,
				Email:    person.Email,
				Company:  person.Company,
			}
			speakerConfID := stableID("static-event:" + talk.Event + ":speaker-conf:" + person.ID)
			if err := upsertSpeakerConf(ctx, tx, speakerConfID, person.ID, confID, speakerTalk); err != nil {
				return nil, err
			}
			if err := linkProposalSpeaker(ctx, tx, proposalID, speakerConfID); err != nil {
				return nil, err
			}
			if person.Photo != "" {
				names = append(names, fmt.Sprintf("%s, photo=%s", person.Name, person.Photo))
			} else {
				names = append(names, person.Name)
			}
		}
		return names, nil
	}

	name, err := importTalkSpeaker(ctx, tx, confID, proposalID, talk, uploadAssets)
	if err != nil {
		return nil, err
	}
	return []string{name}, nil
}

func importTalkSpeaker(ctx context.Context, tx pgx.Tx, confID, proposalID string, talk staticTalk, uploadAssets bool) (string, error) {
	personID, photo, err := upsertPerson(ctx, tx, talk, uploadAssets)
	if err != nil {
		return "", err
	}
	speakerConfID := stableID("static-event:" + talk.Event + ":speaker-conf:" + personID)
	if err := upsertSpeakerConf(ctx, tx, speakerConfID, personID, confID, talk); err != nil {
		return "", err
	}
	if err := linkProposalSpeaker(ctx, tx, proposalID, speakerConfID); err != nil {
		return "", err
	}
	if photo != "" {
		return fmt.Sprintf("%s, photo=%s", talk.Speaker.Name, photo), nil
	}
	return talk.Speaker.Name, nil
}

func validateMultiSpeakerEmails(ctx context.Context, tx pgx.Tx, talks []staticTalk) error {
	var missing []string
	for _, talk := range talks {
		emails := splitSpeakerEmails(talk.Speaker.Email)
		if len(emails) <= 1 {
			continue
		}
		for _, email := range emails {
			if _, err := findExistingPersonByEmail(ctx, tx, email); err != nil {
				missing = append(missing, fmt.Sprintf("%s %q: %s", talk.Event, talk.Title, email))
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("multi-speaker rows are missing existing people by exact email:\n- %s", strings.Join(missing, "\n- "))
	}
	return nil
}

func splitSpeakerEmails(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		out = append(out, email)
	}
	return out
}

func canonicalEmail(raw string) string {
	emails := splitSpeakerEmails(raw)
	if len(emails) == 0 {
		return ""
	}
	return emails[0]
}

func findExistingPersonByEmail(ctx context.Context, tx pgx.Tx, email string) (existingPerson, error) {
	var person existingPerson
	err := tx.QueryRow(ctx, `
		SELECT id::text, name, COALESCE(norm_photo_path, ''), COALESCE(company, ''), COALESCE(email::text, '')
		FROM people
		WHERE lower(email::text) = lower($1)
		ORDER BY
			(twitter_handle <> '') DESC,
			(github_url <> '') DESC,
			(norm_photo_path <> '') DESC,
			updated_at DESC
		LIMIT 1
	`, email).Scan(&person.ID, &person.Name, &person.Photo, &person.Company, &person.Email)
	if err == pgx.ErrNoRows {
		seed, ok := staticPersonSeedByEmail(email)
		if ok {
			person, err = findExistingPersonByStaticSeed(ctx, tx, seed)
		}
	}
	if err == pgx.ErrNoRows {
		return person, fmt.Errorf("existing person not found")
	}
	return person, err
}

func staticPersonSeedByEmail(email string) (staticPersonSeed, bool) {
	email = canonicalEmail(email)
	for _, seed := range staticPeople {
		if canonicalEmail(seed.Email) == email {
			return seed, true
		}
	}
	return staticPersonSeed{}, false
}

func findExistingPersonByStaticSeed(ctx context.Context, tx pgx.Tx, seed staticPersonSeed) (existingPerson, error) {
	var person existingPerson
	personID, err := resolveStaticPersonSeedID(ctx, tx, seed)
	if err != nil {
		return person, err
	}
	if personID == "" {
		return person, pgx.ErrNoRows
	}
	err = tx.QueryRow(ctx, `
		SELECT id::text, name, COALESCE(norm_photo_path, ''), COALESCE(company, ''), COALESCE(email::text, '')
		FROM people
		WHERE id::text = $1
		LIMIT 1
	`, personID).Scan(&person.ID, &person.Name, &person.Photo, &person.Company, &person.Email)
	return person, err
}

func upsertPerson(ctx context.Context, tx pgx.Tx, talk staticTalk, uploadAssets bool) (string, string, error) {
	photo := ""
	if talk.Speaker.PhotoPath != "" {
		raw, ct, ext, err := readStaticImage(talk.Speaker.PhotoPath)
		if err != nil {
			return "", "", err
		}
		photo = speakerphotos.CanonicalPhotoFilename(raw)
		if uploadAssets {
			if err := speakerphotos.New(log.Printf, nil).Mirror(raw, ct, ext); err != nil {
				return "", "", err
			}
		}
	}

	personID, err := resolvePersonID(ctx, tx, talk.Speaker)
	if err != nil {
		return "", "", err
	}
	if personID == "" {
		personID = staticSpeakerID(talk)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO people (
			id, name, email, norm_photo_path, phone, signal, telegram,
			twitter_handle, nostr, github_url, instagram, linkedin, leetcode,
			website_url, company, org_logo_path, bio, avail_to_hire,
			looking_to_hire, tshirt
		)
		VALUES (
			$1::uuid, $2, NULLIF($3, ''), $4, '', '', '',
			$5, $6, $7, '', '', '', $8, $9, '', '', false, false, ''
		)
		ON CONFLICT (id) DO UPDATE SET
			norm_photo_path = CASE WHEN people.norm_photo_path = '' THEN EXCLUDED.norm_photo_path ELSE people.norm_photo_path END,
			email = CASE WHEN people.email IS NULL OR people.email = '' THEN EXCLUDED.email ELSE people.email END,
			twitter_handle = CASE WHEN people.twitter_handle = '' THEN EXCLUDED.twitter_handle ELSE people.twitter_handle END,
			nostr = CASE WHEN people.nostr = '' THEN EXCLUDED.nostr ELSE people.nostr END,
			github_url = CASE WHEN people.github_url = '' THEN EXCLUDED.github_url ELSE people.github_url END,
			website_url = CASE WHEN people.website_url = '' THEN EXCLUDED.website_url ELSE people.website_url END,
			company = CASE WHEN people.company = '' THEN EXCLUDED.company ELSE people.company END
	`, personID, talk.Speaker.Name, canonicalEmail(talk.Speaker.Email), photo, talk.Speaker.Twitter, talk.Speaker.Nostr, talk.Speaker.GithubURL, talk.Speaker.WebsiteURL, talk.Speaker.Company)
	return personID, photo, err
}

func resolvePersonID(ctx context.Context, tx pgx.Tx, sp staticSpeaker) (string, error) {
	queries := []struct {
		sql string
		arg string
	}{
		{`SELECT id::text FROM people WHERE lower(email::text) = lower($1) LIMIT 1`, canonicalEmail(sp.Email)},
		{`SELECT id::text FROM people WHERE lower(twitter_handle) = lower($1) LIMIT 1`, sp.Twitter},
		{`SELECT id::text FROM people WHERE lower(github_url) = lower($1) LIMIT 1`, sp.GithubURL},
		{`SELECT id::text FROM people WHERE lower(name) = lower($1) LIMIT 1`, sp.Name},
	}
	for _, q := range queries {
		if strings.TrimSpace(q.arg) == "" {
			continue
		}
		var id string
		err := tx.QueryRow(ctx, q.sql, q.arg).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != pgx.ErrNoRows {
			return "", err
		}
	}
	return "", nil
}

func staticSpeakerID(talk staticTalk) string {
	email := canonicalEmail(talk.Speaker.Email)
	if email != "" {
		return stableID("static-event:speaker-email:" + email)
	}
	return stableID("static-event:" + talk.Event + ":speaker:" + slug(talk.Speaker.Name))
}

func upsertSpeakerConf(ctx context.Context, tx pgx.Tx, id, personID, confID string, talk staticTalk) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO speaker_confs (
			id, speaker_id, coming_from, availability, record_ok, visa,
			first_event, dinner_rsvp, sponsor, company, org_photo_path,
			invited_at, viewed_at, accepted_at
		)
		VALUES (
			$1::uuid, $2::uuid, '', '{}'::text[], 'RecordingOK', 'Not needed',
			false, false, false, $3, '', now(), now(), now()
		)
		ON CONFLICT (id) DO UPDATE SET
			speaker_id = EXCLUDED.speaker_id,
			record_ok = EXCLUDED.record_ok,
			visa = EXCLUDED.visa,
			company = CASE WHEN speaker_confs.company = '' THEN EXCLUDED.company ELSE speaker_confs.company END,
			accepted_at = EXCLUDED.accepted_at
	`, id, personID, talk.Speaker.Company)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO speaker_confs_conferences (speaker_conf_id, conference_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, id, confID)
	return err
}

func upsertProposal(ctx context.Context, tx pgx.Tx, id, confID string, talk staticTalk) error {
	duration := talk.DurationMin
	if duration <= 0 && !talk.Start.IsZero() && !talk.End.IsZero() {
		duration = int(talk.End.Sub(talk.Start).Minutes())
	}
	if duration <= 0 {
		duration = 30
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO proposals (
			id, conference_id, title, description, setup, comments, talk_type,
			status, desired_duration_min, avail_duration_min, invite_token
		)
		VALUES (
			$1::uuid, $2::uuid, $3, $4, '', 'Imported from static ATX23 archive.',
			$5, 'Scheduled', $6, $6, ''
		)
		ON CONFLICT (id) DO UPDATE SET
			conference_id = EXCLUDED.conference_id,
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			talk_type = EXCLUDED.talk_type,
			status = EXCLUDED.status,
			desired_duration_min = EXCLUDED.desired_duration_min,
			avail_duration_min = EXCLUDED.avail_duration_min
	`, id, confID, talk.Title, talk.Description, talkTypeFor(talk.Category, talk.Title), duration)
	return err
}

func upsertConfTalk(ctx context.Context, tx pgx.Tx, id, confID, proposalID, clipart string, talk staticTalk) error {
	var start any
	var end any
	if !talk.Start.IsZero() {
		start = talk.Start
		calculatedEnd := talk.Start.Add(time.Duration(talk.DurationMin) * time.Minute)
		if !talk.End.IsZero() {
			calculatedEnd = talk.End
		}
		end = calculatedEnd
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO conf_talks (
			id, conference_id, proposal_id, clipart_path, scheduled_start,
			scheduled_end, production_notes, venue, section, cal_notif,
			social_card_path, archived_at
		)
		VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5::timestamptz, $6::timestamptz,
			'', $7, $8, '', '', NULL
		)
		ON CONFLICT (id) DO UPDATE SET
			conference_id = EXCLUDED.conference_id,
			proposal_id = EXCLUDED.proposal_id,
			clipart_path = EXCLUDED.clipart_path,
			scheduled_start = EXCLUDED.scheduled_start,
			scheduled_end = EXCLUDED.scheduled_end,
			venue = EXCLUDED.venue,
			section = EXCLUDED.section,
			archived_at = NULL
	`, id, confID, proposalID, clipart, start, end, talk.Venue, talk.Category)
	return err
}

func linkProposalSpeaker(ctx context.Context, tx pgx.Tx, proposalID, speakerConfID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO proposals_speaker_confs (proposal_id, speaker_conf_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, proposalID, speakerConfID)
	return err
}

func clearProposalSpeakers(ctx context.Context, tx pgx.Tx, proposalID string) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM proposals_speaker_confs
		WHERE proposal_id = $1::uuid
	`, proposalID)
	return err
}

func upsertRecording(ctx context.Context, tx pgx.Tx, id, confTalkID string, talk staticTalk) error {
	var publishAt any
	if !talk.Start.IsZero() {
		publishAt = talk.Start
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO recordings (
			id, conf_talk_id, talk_name, youtube_url, x_url, x_reply_url,
			file_uri, publish_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, '', '', '', $5::timestamptz)
		ON CONFLICT (conf_talk_id) DO UPDATE SET
			talk_name = EXCLUDED.talk_name,
			youtube_url = EXCLUDED.youtube_url,
			publish_at = EXCLUDED.publish_at
	`, id, confTalkID, talk.Title, talk.YouTubeURL, publishAt)
	return err
}

func uploadTalkClipart(talk staticTalk, uploadAssets bool) (string, error) {
	if talk.ClipartPath == "" {
		return "", nil
	}
	raw, contentType, _, err := readStaticImage(talk.ClipartPath)
	if err != nil {
		return "", err
	}
	ext := imageExtForContentType(contentType)
	if ext == "" {
		return "", fmt.Errorf("%s is %s, expected image content", talk.ClipartPath, contentType)
	}
	name := fmt.Sprintf("%s-%03d-%s%s", talk.Event, talk.Index, slug(talk.Anchor), ext)
	if !uploadAssets {
		return name, nil
	}
	avif, err := imgproc.MakeAVIF(raw, 0)
	if err != nil {
		return "", fmt.Errorf("make clipart avif %s: %w", talk.ClipartPath, err)
	}
	pngKey := "talks/" + name
	avifName := strings.TrimSuffix(name, ".png") + ".avif"
	avifKey := "talks/" + avifName
	if !spaces.Exists(pngKey) {
		if _, err := spaces.Upload(pngKey, raw, "image/png", ""); err != nil {
			return "", err
		}
	}
	if !spaces.Exists(avifKey) {
		if _, err := spaces.Upload(avifKey, avif, "image/avif", ""); err != nil {
			return "", err
		}
	}
	manifest, err := spaces.LoadJSONMap(spaces.TalkManifestKey)
	if err != nil {
		manifest = map[string]string{}
	}
	manifest[name] = contentHashShort(raw)
	manifest[avifName] = contentHashShort(avif)
	if err := spaces.SaveJSONMap(spaces.TalkManifestKey, manifest); err != nil {
		return "", err
	}
	return name, nil
}

func imageExtForContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func readStaticImage(src string) ([]byte, string, string, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		req, err := http.NewRequest(http.MethodGet, src, nil)
		if err != nil {
			return nil, "", "", fmt.Errorf("build request %s: %w", src, err)
		}
		req.Header.Set("User-Agent", "btcpp-web-static-import/1.0")
		resp, err := fetchClient.Do(req)
		if err != nil {
			return nil, "", "", fmt.Errorf("fetch %s: %w", src, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, "", "", fmt.Errorf("fetch %s: status %s", src, resp.Status)
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", "", fmt.Errorf("read %s: %w", src, err)
		}
		parsed, _ := url.Parse(src)
		ext := strings.ToLower(filepath.Ext(parsed.Path))
		ct := resp.Header.Get("Content-Type")
		if i := strings.Index(ct, ";"); i >= 0 {
			ct = ct[:i]
		}
		if ct == "" || ct == "application/octet-stream" {
			ct = http.DetectContentType(raw)
		}
		return raw, ct, ext, nil
	}
	path := strings.TrimPrefix(src, "/")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("read %s: %w", path, err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	ct := http.DetectContentType(raw)
	if ct == "application/octet-stream" {
		switch ext {
		case ".png":
			ct = "image/png"
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".webp":
			ct = "image/webp"
		}
	}
	return raw, ct, ext, nil
}

func classifySpeakerURL(sp *staticSpeaker) {
	u := strings.TrimSpace(sp.SourceURL)
	if u == "" {
		return
	}
	parsed, err := url.Parse(u)
	if err != nil {
		sp.WebsiteURL = u
		return
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	path := strings.Trim(parsed.Path, "/")
	switch {
	case host == "twitter.com" || host == "x.com":
		sp.Twitter = strings.TrimPrefix(path, "@")
	case host == "github.com":
		sp.GithubURL = u
	case host == "iris.to":
		sp.Nostr = u
	default:
		sp.WebsiteURL = u
	}
}

func firstMatch(s, expr string) string {
	re := regexp.MustCompile(expr)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(m[1]))
}

func firstNonEmpty(vals ...string) string {
	for _, val := range vals {
		if strings.TrimSpace(val) != "" {
			return val
		}
	}
	return ""
}

func extractJSONObjectAfter(doc, marker string) string {
	idx := strings.Index(doc, marker)
	if idx < 0 {
		return ""
	}
	start := strings.Index(doc[idx:], "{")
	if start < 0 {
		return ""
	}
	start += idx
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(doc); i++ {
		ch := doc[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return doc[start : i+1]
			}
		}
	}
	return ""
}

func firstNonYouTubeHref(s string) string {
	re := regexp.MustCompile(`(?is)href="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		if len(m) < 2 {
			continue
		}
		href := html.UnescapeString(m[1])
		if strings.Contains(href, "youtu") {
			continue
		}
		return href
	}
	return ""
}

func cleanHTML(s string) string {
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe := regexp.MustCompile(`\s+`)
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

func stableID(seed string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed)).String()
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func durationFor(category, title string) int {
	cat := strings.ToLower(category)
	name := strings.ToLower(title)
	switch {
	case strings.Contains(name, "project demos"):
		return 180
	case strings.Contains(name, "project time"):
		return 120
	case strings.Contains(name, "prizes"):
		return 30
	case strings.Contains(cat, "ice breaker"):
		return 60
	case strings.Contains(cat, "workshop"):
		return 60
	case strings.Contains(cat, "keynote"):
		return 30
	default:
		return 30
	}
}

func venueFor(category, title string) string {
	cat := strings.ToLower(category)
	name := strings.ToLower(title)
	switch {
	case strings.Contains(cat, "workshop"):
		return "workshop"
	case strings.Contains(cat, "hackathon") || strings.Contains(name, "hackathon"):
		return "hackathon"
	default:
		return "main"
	}
}

func talkTypeFor(category, title string) string {
	cat := strings.ToLower(category)
	name := strings.ToLower(title)
	switch {
	case strings.Contains(cat, "workshop"):
		return "workshop"
	case strings.Contains(cat, "hackathon") || strings.Contains(name, "hackathon"):
		return "hackathon"
	case strings.Contains(cat, "keynote"):
		return "keynote"
	default:
		return "talk"
	}
}

func contentHashShort(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
