package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"btcpp-web/external/coingecko"
	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
)

var pages []string = []string{"index", "timeline", "vegas25", "terms", "privacy"}

const (
	maxFormBodyBytes         = 1 << 20  // 1 MiB
	maxMultipartBodyBytes    = 12 << 20 // 12 MiB
	maxUploadFileBytes       = 10 << 20 // 10 MiB
	maxPresentationBytes     = 40 << 20 // 40 MiB
	maxPresentationBodyBytes = 42 << 20 // 42 MiB
	maxWebhookBodyBytes      = 1 << 20  // 1 MiB
)

var errUploadTooLarge = errors.New("uploaded file is too large")

var whoIsCache = struct {
	sync.Mutex
	app     *config.AppContext
	people  []*WhoIsPerson
	expires time.Time
}{}

func limitRequestBody(w http.ResponseWriter, r *http.Request, max int64) {
	r.Body = http.MaxBytesReader(w, r.Body, max)
}

func fieldGroup(name string, v interface{}, isRange bool) EmailFieldGroup {
	fields := getStructFields(v)
	prefix := name + "."
	if isRange {
		prefix = "."
	}
	items := make([]string, len(fields))
	for i, f := range fields {
		items[i] = prefix + f
	}
	return EmailFieldGroup{Name: name, Items: items, IsRange: isRange}
}

func getStructFields(v interface{}) []string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	var fields []string
	for i := 0; i < t.NumField(); i++ {
		fields = append(fields, t.Field(i).Name)
	}
	return fields
}

func newFormDecoder() *schema.Decoder {
	dec := schema.NewDecoder()
	dec.IgnoreUnknownKeys(true)
	dec.RegisterConverter("", func(value string) reflect.Value {
		return reflect.ValueOf(strings.TrimSpace(value))
	})
	dec.RegisterConverter(types.Twitter{}, func(value string) reflect.Value {
		return reflect.ValueOf(types.ParseTwitter(value))
	})
	return dec
}

/* Thank you StackOverflow https://stackoverflow.com/a/50581032 */
func findAndParseTemplates(rootDir string, funcMap template.FuncMap) (*template.Template, error) {
	cleanRoot := filepath.Clean(rootDir)
	pfx := len(cleanRoot) + 1
	root := template.New("")

	err := filepath.Walk(cleanRoot, func(path string, info os.FileInfo, e1 error) error {
		if !info.IsDir() && strings.HasSuffix(path, ".tmpl") {
			if e1 != nil {
				return e1
			}

			b, e2 := ioutil.ReadFile(path)
			if e2 != nil {
				return e2
			}

			name := path[pfx:]
			t := root.New(name).Funcs(funcMap)
			_, e2 = t.Parse(string(b))
			if e2 != nil {
				return e2
			}
		}

		return nil
	})

	return root, err
}

func loadTemplates(ctx *config.AppContext) error {

	var err error
	funcMap := template.FuncMap{
		"safeURL": func(s string) template.URL {
			u := strings.TrimSpace(s)
			switch {
			case u == "":
				return ""
			case strings.HasPrefix(u, "/"):
				return template.URL(u)
			case strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://"):
				return template.URL(u)
			case strings.HasPrefix(u, "data:image/png;base64,"):
				return template.URL(u)
			default:
				return ""
			}
		},
		"absoluteURL": func(base, path string) string {
			path = strings.TrimSpace(path)
			if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "data:") {
				return path
			}
			base = strings.TrimRight(strings.TrimSpace(base), "/")
			if strings.HasPrefix(path, "/") {
				return base + path
			}
			return base + "/" + path
		},
		"safeCSS": func(s string) template.CSS {
			return template.CSS(s)
		},
		"instagramURL": func(s string) template.URL {
			return template.URL(instagramURL(s))
		},
		"githubURL": func(s string) template.URL {
			return template.URL(profileURL(s, "github.com"))
		},
		"linkedinURL": func(s string) template.URL {
			return template.URL(profileURL(s, "linkedin.com/in"))
		},
		"leetcodeURL": func(s string) template.URL {
			return template.URL(profileURL(s, "leetcode.com"))
		},
		"websiteURL": func(s string) template.URL {
			return template.URL(websiteURL(s))
		},
		"speakerPublicPath": func(s *types.Speaker) template.URL {
			if s == nil {
				return ""
			}
			return template.URL("/whois/" + publicSpeakerSlug(s))
		},
		"confImage": func(tag, base string) template.URL {
			return template.URL(confImagePath(tag, base))
		},
		"confVenueImages":         confVenueImages,
		"archiveTalks":            archiveTalks,
		"archiveResourcesAllowed": archiveResourcesAllowed,
		"avifSibling": func(s string) string {
			u := strings.TrimSpace(s)
			if !strings.HasSuffix(strings.ToLower(u), ".png") {
				return ""
			}
			return u[:len(u)-4] + ".avif"
		},
		"css": func(s string) template.HTML {
			return template.HTML(fmt.Sprintf(`<style type="text/css">%s</style>`, s))
		},
		"isLast": func(index int, count int) bool {
			return index+1 == count
		},
		"ishtml": func(s string) template.HTML {
			return template.HTML(s)
		},
		"hackathonDescription": hackathonDescriptionHTML,
		"hackathonRichText":    hackathonRichTextHTML,
		"mul": func(a, b int) int {
			return a * b
		},
		"int": func(v interface{}) int {
			switch n := v.(type) {
			case int:
				return n
			case uint:
				return int(n)
			case int64:
				return int(n)
			case uint64:
				return int(n)
			default:
				return 0
			}
		},
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"usub": func(a, b uint) uint {
			if b >= a {
				return 0
			}
			return a - b
		},
		"add": func(a, b int) int {
			return a + b
		},
		"inSlice": func(needle string, haystack []string) bool {
			for _, s := range haystack {
				if s == needle {
					return true
				}
			}
			return false
		},
		"hasPrefix": strings.HasPrefix,
		"lower":     strings.ToLower,
		"trim":      strings.TrimSpace,
		"truncateText": func(s string, limit int) string {
			s = strings.TrimSpace(s)
			if limit <= 0 || len([]rune(s)) <= limit {
				return s
			}
			r := []rune(s)
			return strings.TrimSpace(string(r[:limit])) + "..."
		},
		"limitHotels": func(hotels []*types.Hotel, limit int) []*types.Hotel {
			if limit < 0 || len(hotels) <= limit {
				return hotels
			}
			return hotels[:limit]
		},
		"limitSpeakers": func(speakers []*types.Speaker, limit int) []*types.Speaker {
			if limit < 0 || len(speakers) <= limit {
				return speakers
			}
			return speakers[:limit]
		},
		// dict builds a map[string]any from variadic key/value pairs
		// — enables passing named params to template blocks (e.g.
		// {{ template "cal_picker" (dict "Title" .Name "Start" ...) }}).
		// Errors out at template-exec time on an odd number of args
		// or a non-string key, so misuse fails loudly instead of
		// silently truncating.
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict requires an even number of arguments")
			}
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				k, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict key %d not a string", i)
				}
				m[k] = values[i+1]
			}
			return m, nil
		},
		// mapVenue resolves the raw venue slug ("one"/"two"/...) to
		// the human-readable label ("Main Stage" / ...). Thin
		// template-side wrapper around ics.MapVenue so the cal_picker
		// block can render a meaningful Location instead of leaking
		// the internal slug.
		"mapVenue": ics.MapVenue,
		// jsonStr returns a JSON-encoded, double-quoted string —
		// used by event_jsonld.tmpl to safely embed user-supplied
		// titles / descriptions / venue names into a <script
		// type="application/ld+json"> block. template.JS bypasses
		// html/template's JS-context escaping; the JSON itself is
		// already script-safe.
		"jsonStr": func(s string) template.JS {
			b, _ := json.Marshal(s)
			return template.JS(b)
		},
		// jsonDate formats a time.Time / *time.Time as a
		// JSON-encoded RFC 3339 string, or `null` when zero / nil.
		"jsonDate": func(t interface{}) template.JS {
			format := func(tt time.Time) template.JS {
				if tt.IsZero() {
					return template.JS("null")
				}
				b, _ := json.Marshal(tt.Format(time.RFC3339))
				return template.JS(b)
			}
			switch v := t.(type) {
			case time.Time:
				return format(v)
			case *time.Time:
				if v == nil {
					return template.JS("null")
				}
				return format(*v)
			}
			return template.JS("null")
		},
		"confSocialImage": confSocialImage,
		"absoluteSEOURL":  absoluteSEOURL,
		// rfc3339 formats a time.Time / *time.Time as RFC 3339.
		// Returns "" for a nil pointer or zero time so templates can
		// safely emit it into a data-* attribute without leaking
		// "0001-01-01T00:00:00Z". Used by the cal_picker block to
		// hand structured timestamps to the JS picker.
		"rfc3339": func(t interface{}) string {
			switch v := t.(type) {
			case nil:
				return ""
			case time.Time:
				if v.IsZero() {
					return ""
				}
				return v.Format(time.RFC3339)
			case *time.Time:
				if v == nil || v.IsZero() {
					return ""
				}
				return v.Format(time.RFC3339)
			default:
				return ""
			}
		},
		"dollars": func(cents int64) string {
			// "%.2f" with the dollars+cents split keeps negative
			// values rendering correctly (e.g. -$1.50). Used by
			// the dashboard affiliate stats — values stored in
			// cents, displayed as $X.XX.
			whole := cents / 100
			frac := cents % 100
			if frac < 0 {
				frac = -frac
			}
			return fmt.Sprintf("%d.%02d", whole, frac)
		},
		"sats": func(sats int64) string {
			// Group with thousands separators so "1,234,567 sats"
			// reads more easily than "1234567". Negative values
			// keep the minus before the digits.
			return groupSatsCommas(sats)
		},
		"satsBitcoin": func(sats int64) template.HTML {
			// Renders sats as BTC decimal notation ("0.00012345"),
			// with the "0." prefix + leading zeros wrapped in a
			// text-gray-400 span so only the significant digits
			// inherit the surrounding paragraph color (green for
			// saved, indigo for earned).
			return formatBitcoinAmount(sats)
		},
		"siteStats": func() siteStatsView {
			return formatSiteStats(getters.FetchSiteStats(ctx))
		},
		"ge": func(a, b int) bool {
			return a >= b
		},
		"mod": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a % b
		},
		"iterRange": func(start, end int) []int {
			if end <= start {
				return nil
			}
			out := make([]int, 0, end-start+1)
			for i := start; i <= end; i++ {
				out = append(out, i)
			}
			return out
		},
		"ganttLeft": func(times *types.Times, dayMin, dayMax int) float64 {
			if times == nil {
				return 0
			}
			startMin := float64(times.Start.Hour()*60 + times.Start.Minute())
			dayStartMin := float64(dayMin * 60)
			dayWidth := float64((dayMax - dayMin) * 60)
			if dayWidth == 0 {
				return 0
			}
			return (startMin - dayStartMin) / dayWidth * 100
		},
		"ganttWidth": func(times *types.Times, dayMin, dayMax int) float64 {
			if times == nil || times.End == nil {
				return 0
			}
			startMin := float64(times.Start.Hour()*60 + times.Start.Minute())
			endMin := float64(times.End.Hour()*60 + times.End.Minute())
			dayWidth := float64((dayMax - dayMin) * 60)
			if dayWidth == 0 {
				return 0
			}
			return (endMin - startMin) / dayWidth * 100
		},
		"hourPct": func(hour, dayMin, dayMax int) float64 {
			width := float64(dayMax - dayMin)
			if width == 0 {
				return 0
			}
			return float64(hour-dayMin) / width * 100
		},
		"shiftStartHHMM": func(s *types.WorkShift) string {
			if s == nil || s.ShiftTime == nil {
				return ""
			}
			return s.ShiftTime.Start.Format("15:04")
		},
		"shiftEndHHMM": func(s *types.WorkShift) string {
			if s == nil || s.ShiftTime == nil || s.ShiftTime.End == nil {
				return ""
			}
			return s.ShiftTime.End.Format("15:04")
		},
		"spacesURL": func(key string) string {
			return spaces.PublicURL(key)
		},
		"satelliteTimeLabel":    satelliteEventTimeLabel,
		"satelliteInputTime":    satelliteEventInputTime,
		"satelliteFormValue":    satelliteFormValue,
		"satelliteFormStartsAt": satelliteFormStartsAt,
		"satelliteFormEndsAt":   satelliteFormEndsAt,
		"formatHourMin":         FormatHourMin,
		"hourLabels":            HourLabels,
		"venueChipClass":        VenueChipClasses,
		"venueLabel": func(raw string) string {
			// Resolves the raw venue slug ("one" / "two" / "three")
			// to the human-readable stage label. Falls back to the
			// raw value when the mapping doesn't recognize it, so
			// custom venues from older confs still render
			// something sensible.
			if label := ics.MapVenue(raw); label != "" {
				return label
			}
			return raw
		},
		"agendaSessionsForVenue": agendaSessionsForVenue,
		"agendaTypeClass":        agendaTypeClass,
		"agendaTypeLabel":        agendaTypeLabel,
		"agendaDayHeight":        agendaDayHeight,
		"agendaSessionTop":       agendaSessionTop,
		"agendaSessionHeight":    agendaSessionHeight,
		"agendaHourMarks":        agendaHourMarks,
		"navConfs": func() NavConfList {
			return buildNavConfList(ctx)
		},
		"sponsorTiers": func(conf *types.Conf) []*SponsorTier {
			if conf == nil {
				return nil
			}
			return SponsorTiersForConf(ctx, conf.Ref)
		},
		"sponsorDisplayRank":          sponsorDisplayRank,
		"showTicketPriceIncreaseDate": showTicketPriceIncreaseDate,
		"sponsorBanner": func(conf *types.Conf) []*types.Sponsorship {
			if conf == nil {
				return nil
			}
			return SponsorBannerForConf(ctx, conf.Ref)
		},
		"merchImage":                  merchImage,
		"merchProductStock":           merchProductStock,
		"shopOrderItemImage":          shopOrderItemImage,
		"shopFulfillmentLabel":        shopFulfillmentLabel,
		"shopOrderHasFulfillment":     shopOrderHasFulfillment,
		"shopOrderFulfillmentSummary": shopOrderFulfillmentSummary,
		"merchPrice":                  merchPrice,
		"merchMoney":                  merchMoney,
		"merchInches":                 merchInches,
		"merchSats":                   merchSats,
		"merchVariantPrice":           merchVariantPrice,
		"merchVariantAvailable": func(v *types.MerchVariant) bool {
			return merchVariantAvailable(v, 1)
		},
		"merchProductSoldOut": merchProductSoldOut,
		"merchJSON":           merchJSON,
		"isShopPage": func(v any) bool {
			_, ok := v.(*shopPage)
			return ok
		},
		"speakerPhoto": func(photo string) string {
			if photo == "" {
				return spaces.PublicURL("speakers/default.avif")
			}
			return spaces.PublicURL("speakers/" + photo)
		},
		"talkClipart": func(filename string) string {
			// Empty filename → empty URL so templates render a
			// broken/empty image rather than a "talks/" path that
			// 404s. Most call sites already gate on the field
			// being non-empty before rendering the <img> at all.
			if filename == "" {
				return ""
			}
			return spaces.PublicURL("talks/" + filename)
		},
		"inviteLink": func(p *types.Proposal) string {
			if p == nil {
				return ""
			}
			return helpers.InviteLink(ctx, p.ID, p.InviteToken)
		},
		"formatTime":    formatRunOfShowTime,
		"signedMinutes": formatSignedMinutes,
		"shirtSizes":    types.ShirtSizeOptions,
		"inDev": func() bool {
			return !ctx.Env.Prod
		},
		"isFuture": func(t time.Time) bool {
			return t.After(time.Now())
		},
	}
	ctx.TemplateCache, err = findAndParseTemplates("templates", funcMap)
	return err
}

func contains(list []string, item string) bool {
	for _, x := range list {
		if item == x {
			return true
		}
	}
	return false
}

func findTicket(app *config.AppContext, tixID string) (*types.ConfTicket, *types.Conf) {
	confs, err := getters.ListConfs(app)
	if err != nil {
		app.Err.Printf("unable to find ticket?? %s", err)
		return nil, nil
	}

	for _, conf := range confs {
		for _, tix := range conf.Tickets {
			if tix.ID == tixID {
				return tix, conf
			}
		}
	}

	return nil, nil
}

func determineTixKind(tixSlug string) (string, string, error) {
	tixParts := strings.Split(tixSlug, "+")
	if len(tixParts) != 1 && len(tixParts) != 2 {
		return "", "", fmt.Errorf("invalid ticket slug %s", tixSlug)
	}
	if len(tixParts) == 1 {
		return tixParts[0], types.TicketTypeGeneral, nil
	}
	switch tixParts[1] {
	case types.TicketTypeLocal:
		return tixParts[0], types.TicketTypeLocal, nil
	case "sponsor", types.TicketTypeSponsored:
		return tixParts[0], types.TicketTypeSponsored, nil
	default:
		return "", "", fmt.Errorf("type %s is not supported", tixParts[1])
	}
}

func determineTixSelection(ctx *config.AppContext, tixSlug string) (*types.Conf, *types.ConfTicket, string, error) {
	tixID, ticketKind, err := determineTixKind(tixSlug)
	if err != nil {
		return nil, nil, "", err
	}
	tix, conf := findTicket(ctx, tixID)
	if tix == nil {
		return nil, nil, "", fmt.Errorf("Unable to find tix %s", tixID)
	}
	return conf, tix, ticketKind, nil
}

func determineTixPrice(ctx *config.AppContext, tixSlug string) (*types.Conf, *types.ConfTicket, uint, string, error) {
	conf, tix, ticketKind, err := determineTixSelection(ctx, tixSlug)
	if err != nil {
		return nil, nil, 0, "", err
	}
	if ticketKind == types.TicketTypeLocal {
		return conf, tix, tix.Price(true), ticketKind, nil
	}
	return conf, tix, tix.Price(false), ticketKind, nil
}

/* Find ticket where current sold + date > inputs */
func findCurrTix(conf *types.Conf, soldCount uint) *types.ConfTicket {
	now := time.Now()
	/* Sort the tickets! */
	tixs := types.ConfTickets(conf.Tickets)
	sort.Sort(&tixs)
	for _, tix := range tixs {
		if tix.Expires.Start.Before(now) {
			continue
		}
		if tix.Max <= soldCount {
			continue
		}
		return tix
	}

	/* No tix available! */
	return nil
}

/* Find ticket where current sold + date > inputs */
func findMaxTix(conf *types.Conf) *types.ConfTicket {
	/* Sort the tickets! */
	tixs := types.ConfTickets(conf.Tickets)
	sort.Sort(&tixs)

	if len(tixs) <= 0 {
		return nil
	}

	maxTix := tixs[0]
	for _, tix := range tixs {
		if tix.StandardPrice() > maxTix.StandardPrice() {
			maxTix = tix
		}
	}

	return maxTix
}

func showTicketPriceIncreaseDate(conf *types.Conf, tix *types.ConfTicket) bool {
	if conf == nil || tix == nil || tix.Expires == nil {
		return false
	}
	return tix.Expires.Start.Before(conf.StartDate)
}

func confImagePath(tag, base string) string {
	tag = strings.Trim(strings.TrimSpace(tag), "/")
	base = strings.Trim(strings.TrimSpace(base), "/")
	if tag == "" || base == "" {
		return ""
	}

	for _, ext := range []string{"avif", "png", "jpg", "jpeg", "webp"} {
		path := filepath.Join("static", "img", tag, base+"."+ext)
		if _, err := os.Stat(path); err == nil {
			return "/" + filepath.ToSlash(path)
		}
	}

	if base == "leading" {
		if fallback := confHeroFallback(tag); fallback != "" {
			return fallback
		}
		return "/static/img/rebrand/light-sketch-bg.avif"
	}
	return ""
}

func confVenueImages(tag string) []string {
	images := make([]string, 0, 4)
	for _, base := range []string{"one", "two", "three", "four"} {
		if src := confImagePath(tag, base); src != "" {
			images = append(images, src)
		}
	}
	return images
}

func confHeroFallback(tag string) string {
	switch tag {
	case "atx22":
		return "/static/img/atx22/leading.png"
	case "atx24":
		return "/static/img/atx24.png"
	case "atx25":
		return "/static/img/atx25_promo.png"
	case "cdmx22":
		return "/static/img/cdmx22/leading.png"
	case "berlin24":
		return "/static/img/berlin24/leading.png"
	case "berlin25":
		return "/static/img/berlin/leading.png"
	case "floripa":
		return "/static/img/floripa/exterior_one.avif"
	case "istanbul":
		return "/static/img/istanbul/leading.png"
	}
	return ""
}

// Routes sets up the routes for the application
// siteStatsView is the about-page-friendly shape of the cached site
// stats, exposed to templates via the {{ siteStats }} function.
type siteStatsView struct {
	Confs     int    // raw integer — no rounding (e.g. "14")
	Talks     string // rounded down to nearest 50 with "+" suffix (e.g. "400+")
	Attendees string // same, formatted as "X.Yk+" once over 1000 (e.g. "3.2k+")
}

func formatSiteStats(s getters.SiteStatsValues) siteStatsView {
	return siteStatsView{
		Confs:     s.PastConfs,
		Talks:     formatRoundedDownPlus(s.PastTalks),
		Attendees: formatRoundedDownPlus(s.Attendees),
	}
}

// formatRoundedDownPlus floors n to the nearest 50, then renders with a
// "+" suffix. Above 1000 it switches to "X.Yk+" (one decimal, trailing
// zero trimmed).
func formatRoundedDownPlus(n int) string {
	if n < 50 {
		return strconv.Itoa(n)
	}
	r := (n / 50) * 50
	if r >= 1000 {
		whole := r / 1000
		hundreds := (r % 1000) / 100
		if hundreds == 0 {
			return fmt.Sprintf("%dk+", whole)
		}
		return fmt.Sprintf("%d.%dk+", whole, hundreds)
	}
	return fmt.Sprintf("%d+", r)
}

// statusRecorder wraps http.ResponseWriter so requestLog can read the
// final status code after the handler returns. WriteHeader stores the
// code and forwards; if the handler never calls WriteHeader explicitly,
// we default to 200 (matching net/http's behavior).
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.status = code
	s.wroteHeader = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.WriteHeader(http.StatusOK)
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

type requestIDContextKey struct{}
type requestAppContextKey struct{}

var requestCounter uint64

func nextRequestID() string {
	n := atomic.AddUint64(&requestCounter, 1)
	return fmt.Sprintf("%x-%06x", time.Now().UnixNano(), n)
}

func requestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	id, _ := r.Context().Value(requestIDContextKey{}).(string)
	return id
}

func withRequestApp(app *config.AppContext, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scoped := app.WithDatabaseContext(r.Context())
		r = r.WithContext(context.WithValue(r.Context(), requestAppContextKey{}, scoped))
		h.ServeHTTP(w, r)
	})
}

func requestApp(r *http.Request, fallback *config.AppContext) *config.AppContext {
	if r != nil {
		if app, ok := r.Context().Value(requestAppContextKey{}).(*config.AppContext); ok && app != nil {
			return app
		}
	}
	return fallback
}

func withOptionalIdentity(app *config.AppContext, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = auth.WithIdentityResolver(r, requestApp(r, app))
		h.ServeHTTP(w, r)
	})
}

// requestLog is a middleware that logs each incoming request's start
// and completion (method, path, status, duration). It also emits watchdog
// messages while a request is still running so hung requests are visible
// even when they never reach completion logging.
func requestLog(ctx *config.AppContext, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			h.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		id := nextRequestID()
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id))
		done := make(chan struct{})
		go requestWatchdog(ctx, r, id, start, done)

		remote := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		if remote == "" {
			remote = r.RemoteAddr
		} else if idx := strings.Index(remote, ","); idx >= 0 {
			remote = strings.TrimSpace(remote[:idx])
		}
		ctx.Infos.Printf("→ request id=%s method=%s path=%s remote=%s", id, r.Method, r.URL.Path, remote)
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			close(done)
			ctx.Infos.Printf("← request id=%s method=%s path=%s status=%d bytes=%d duration=%s", id, r.Method, r.URL.Path, sr.status, sr.bytes, time.Since(start))
		}()
		h.ServeHTTP(sr, r)
	})
}

func requestWatchdog(ctx *config.AppContext, r *http.Request, id string, start time.Time, done <-chan struct{}) {
	for _, threshold := range []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second} {
		timer := time.NewTimer(time.Until(start.Add(threshold)))
		select {
		case <-done:
			timer.Stop()
			return
		case <-timer.C:
			logSlowRequest(ctx, r, id, start)
		}
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			logSlowRequest(ctx, r, id, start)
		}
	}
}

func logSlowRequest(ctx *config.AppContext, r *http.Request, id string, start time.Time) {
	if ctx == nil || ctx.DB == nil {
		return
	}
	stats := ctx.DB.Stat()
	ctx.Err.Printf("request still running id=%s method=%s path=%s duration=%s db_acquired=%d db_idle=%d db_total=%d db_empty_acquires=%d db_canceled_acquires=%d db_acquire_wait=%s",
		id, r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond),
		stats.AcquiredConns(), stats.IdleConns(), stats.TotalConns(), stats.EmptyAcquireCount(),
		stats.CanceledAcquireCount(), stats.AcquireDuration().Round(time.Millisecond))
}

func redirectTrailingSlash(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/static/") && strings.HasSuffix(r.URL.Path, "/") {
			target := *r.URL
			target.Path = strings.TrimRight(r.URL.Path, "/")
			target.RawPath = ""
			http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func getFaviconHandler(name string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, fmt.Sprintf("static/favicon/%s", name))
	}
}

func addFaviconRoutes(r *mux.Router) error {
	files, err := ioutil.ReadDir("static/favicon/")
	if err != nil {
		return err
	}

	/* If asked for a favicon, we'll serve it up */
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		r.HandleFunc(fmt.Sprintf("/%s", file.Name()), getFaviconHandler(file.Name())).Methods("GET")
	}

	return nil
}

func listConfs(w http.ResponseWriter, ctx *config.AppContext) []*types.Conf {
	var confs types.ConfList
	var err error
	confs, err = getters.ListConfs(ctx)
	if err != nil {
		// FIXME add an internal error page
		http.Error(w, "Unable to load confereneces, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/conf-reload conf load failed ! %s", err.Error())
		return nil
	}

	sort.Sort(&confs)
	return confs
}

func listVolunteerConfs(w http.ResponseWriter, ctx *config.AppContext) []*types.Conf {
	confs := listConfs(w, ctx)
	if confs == nil {
		return nil
	}
	var out []*types.Conf
	for _, conf := range confs {
		if conf != nil && conf.VolunteerOpen() {
			out = append(out, conf)
		}
	}
	return out
}

func listJobs(w http.ResponseWriter, ctx *config.AppContext) []*types.JobType {
	var jobs types.JobsList
	var err error
	jobs, err = getters.ListJobTypes(ctx)
	if err != nil {
		// FIXME add an internal error page
		http.Error(w, "Unable to load jobs, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("jobs load failed ! %s", err.Error())
		return nil
	}

	sort.Sort(&jobs)
	return jobs
}

func handle404(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(http.StatusNotFound)
	ctx.Infos.Printf("404'd: %s", r.URL.Path)

	RenderPage(w, r, ctx, "404")
}

// discountSessionKey is the SCS session key under which a per-conf
// discount code from a /{tag}?code= visit is stashed. Per-conf
// scoping keeps codes from one event from leaking into another's
// checkout flow when a visitor browses multiple confs in the same
// session.
func discountSessionKey(confTag string) string {
	return "disc:" + confTag
}

// affiliateSessionKey is the parallel slot for silent (%0) affiliate
// codes — the buyer never sees a discount UI, but the code rides
// through to checkout via a hidden form input so the affiliate still
// gets credit.
func affiliateSessionKey(confTag string) string {
	return "aff:" + confTag
}

func checkoutEmailSessionKey(confTag string) string {
	return "checkout-email:" + confTag
}

// affiliateMath returns (saved, earned) for one checkout. Inputs +
// outputs share a single unit — sats in this codebase, but the math
// is unit-agnostic. preDiscountPerTicket is the per-ticket list
// price BEFORE any discount; paidTotal is what the buyer actually
// paid; count is the number of tickets. Inputs should be in the same
// unit, usually fiat cents. The 20% ceiling is fixed: affiliates earn
// whatever's left after the buyer's actual savings come out of that
// ceiling. Both outputs are floored at zero to avoid negatives leaking
// into Notion.
func affiliateMath(preDiscountPerTicket, count, paidTotal int64) (saved, earned int64) {
	original := preDiscountPerTicket * count
	ceiling := original * 20 / 100
	saved = original - paidTotal
	if saved < 0 {
		saved = 0
	}
	earned = ceiling - saved
	if earned < 0 {
		earned = 0
	}
	return saved, earned
}

// recordAffiliateUsageFromCheckout writes one AffiliateUsage row to
// Notion when a successful checkout consumed a discount code that
// has an AffiliateEmail set. The list price + paid total arrive in
// fiat cents (whatever currency the tier was priced in). Saved/Earned
// are split in fiat cents first, then converted to sats. Doing the
// split before BTC conversion keeps a %20 buyer discount from leaving
// tiny EarnedSats remainders due to spot-price or rounding drift.
//
// preDiscountCentsStr is a string from webhook metadata (Stripe map
// / OpenNode struct); missing or unparseable means we skip recording
// rather than guessing. CoinGecko fetch failures also skip — a
// missing row is recoverable (re-run a backfill); a bogus row is
// not. Failures are logged, never fatal.
func recordAffiliateUsageFromCheckout(ctx *config.AppContext, conf *types.Conf, entry *types.Entry, preDiscountCentsStr string) {
	if conf == nil || entry == nil || entry.DiscountRef == "" {
		return
	}
	disc, err := getters.GetDiscountByRef(ctx, entry.DiscountRef)
	if err != nil || disc == nil || disc.AffiliateEmail == "" {
		// Non-affiliate code — nothing to record. Errors are
		// silent because the discount might just be missing
		// from cache mid-refresh.
		return
	}
	preDiscountCents, err := strconv.ParseInt(strings.TrimSpace(preDiscountCentsStr), 10, 64)
	if err != nil || preDiscountCents <= 0 {
		ctx.Err.Printf("affiliate usage skip %s: missing pre-discount-cents (%q)", disc.CodeName, preDiscountCentsStr)
		return
	}
	count := int64(len(entry.Items))
	if count <= 0 {
		return
	}
	currency := strings.TrimSpace(entry.Currency)
	if currency == "" {
		ctx.Err.Printf("affiliate usage skip %s: empty entry.Currency", disc.CodeName)
		return
	}
	savedCents, earnedCents := affiliateMath(preDiscountCents, count, entry.Total)
	savedSats, err := coingecko.CentsToSats(savedCents, currency)
	if err != nil {
		ctx.Err.Printf("affiliate usage skip %s: coingecko saved cents→sats (%s): %s", disc.CodeName, currency, err)
		return
	}
	earnedSats, err := coingecko.CentsToSats(earnedCents, currency)
	if err != nil {
		ctx.Err.Printf("affiliate usage skip %s: coingecko earned cents→sats (%s): %s", disc.CodeName, currency, err)
		return
	}
	err = getters.RecordAffiliateUsage(ctx, getters.AffiliateUsageInput{
		CodeName:       disc.CodeName,
		AffiliateEmail: disc.AffiliateEmail,
		ConfTag:        conf.Tag,
		SavedSats:      savedSats,
		EarnedSats:     earnedSats,
		TicketsCount:   uint(count),
	})
	if err != nil {
		ctx.Err.Printf("affiliate usage record %s for %s: %s", disc.CodeName, disc.AffiliateEmail, err)
	}
}

// groupSatsCommas formats an int64 sat amount with thousands
// separators, e.g. 1234567 → "1,234,567". Negative values keep the
// minus before the grouped digits.
func groupSatsCommas(sats int64) string {
	neg := sats < 0
	if neg {
		sats = -sats
	}
	str := strconv.FormatInt(sats, 10)
	n := len(str)
	if n <= 3 {
		if neg {
			return "-" + str
		}
		return str
	}
	var b strings.Builder
	pre := n % 3
	if pre > 0 {
		b.WriteString(str[:pre])
		if n > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < n; i += 3 {
		b.WriteString(str[i : i+3])
		if i+3 < n {
			b.WriteByte(',')
		}
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// formatBitcoinAmount renders a sat amount as a fixed-precision
// BTC decimal string ("0.00012345"), with the leading zeros + the
// "0." prefix wrapped in <span class="text-gray-400"> so the
// surrounding paragraph color (e.g. text-green-700) only reaches
// the significant digits. Amounts ≥ 1 BTC start with the integer
// part and render in full color, no leading-zero span.
func formatBitcoinAmount(sats int64) template.HTML {
	neg := sats < 0
	if neg {
		sats = -sats
	}
	whole := sats / 100_000_000
	frac := sats % 100_000_000
	full := fmt.Sprintf("%d.%08d", whole, frac)

	// First non-zero digit (the decimal point and leading zeros all
	// stay in the grey prefix; everything from the first 1-9 onward
	// inherits the paragraph color).
	splitIdx := -1
	for i := 0; i < len(full); i++ {
		c := full[i]
		if c >= '1' && c <= '9' {
			splitIdx = i
			break
		}
	}
	prefix := ""
	if neg {
		prefix = "-"
	}
	if splitIdx < 0 {
		// 0.00000000 — fully zero.
		return template.HTML(fmt.Sprintf(`%s<span class="text-gray-400">%s</span>`, prefix, full))
	}
	if splitIdx == 0 {
		// ≥ 1 BTC — starts with a significant digit.
		return template.HTML(prefix + full)
	}
	return template.HTML(fmt.Sprintf(`%s<span class="text-gray-400">%s</span>%s`, prefix, full[:splitIdx], full[splitIdx:]))
}

func calcTixHMAC(ctx *config.AppContext, conf *types.Conf, tixPrice uint, discountPrice uint, discountCode string) string {
	mac := hmac.New(sha256.New, ctx.Env.HMACKey[:])
	mac.Write([]byte(conf.Ref))
	priceBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(priceBytes, uint64(tixPrice))
	mac.Write(priceBytes)
	binary.LittleEndian.PutUint64(priceBytes, uint64(discountPrice))
	mac.Write(priceBytes)
	mac.Write([]byte(discountCode))
	return hex.EncodeToString(mac.Sum(nil))
}

// effectiveDiscountCode picks which of the two form-carried codes
// drives checkout: a buyer-typed Discount wins over a silent
// AffiliateCode. The override semantic — "type a different code
// and the prior affiliate's credit is dropped" — falls out of this:
// the discount-ref recorded on the entry is whatever the effective
// code resolves to, so RecordAffiliateUsage only fires for the
// code that was actually applied.
func effectiveDiscountCode(typedCode, affiliateCode string) string {
	if strings.TrimSpace(typedCode) != "" {
		return typedCode
	}
	return affiliateCode
}

func ReloadConf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	confs, err := getters.ListConfs(ctx)
	if err != nil {
		http.Error(w, "Unable to load confereneces, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/conf-reload conf load failed ! %s", err.Error())
		return
	}
	for _, conf := range confs {
		getters.UpdateSoldTix(ctx, conf)
	}

	/* We redirect to home on success */
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
