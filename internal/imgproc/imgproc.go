package imgproc

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"image/jpeg"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ShortID returns a 12-char hex content fingerprint (first 6 bytes of SHA-256).
// Same bytes always produce the same ID, so it doubles as a Spaces dedupe key:
// 48 bits of address space is plenty for our speaker photo volume.
func ShortID(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:6])
}

// SocialCardID fingerprints the product image, displayed name and price, and HTML template.
// Editing the template therefore produces a new object key instead of leaving
// social networks and CDNs pointed at a cached rendering of the old layout.
func SocialCardID(data []byte, productName, productPrice string) string {
	hash := sha256.New()
	hash.Write([]byte(socialCardTemplateSource))
	hash.Write([]byte{0})
	hash.Write(socialCardBrandFont)
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(productName)))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(productPrice)))
	hash.Write([]byte{0})
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil)[:6])
}

const ffmpegTimeout = 60 * time.Second
const socialCardRenderTimeout = 60 * time.Second

const (
	SocialCardWidth  = 1200
	SocialCardHeight = 630
)

//go:embed merch_social_card.html
var socialCardTemplateSource string

//go:embed site_social_card.html
var siteSocialCardTemplateSource string

//go:embed ubuntu_bold_italic.ttf
var socialCardBrandFont []byte

//go:embed permanent_marker.ttf
var socialCardChalkFont []byte

var socialCardTemplate = template.Must(template.New("merch_social_card.html").Parse(socialCardTemplateSource))
var siteSocialCardTemplate = template.Must(template.New("site_social_card.html").Parse(siteSocialCardTemplateSource))

var socialCardChromeSlots = make(chan struct{}, 2)

type socialCardTemplateData struct {
	ProductImageURL template.URL
	ProductName     string
	ProductPrice    string
	BrandFontURL    template.URL
}

type SiteSocialCardStat struct {
	Value string
	Label string
}

type SiteSocialCardMapPoint struct {
	X        float64
	Y        float64
	Upcoming bool
}

// SiteSocialCard describes the shared large-image unfurl used by public site
// landing pages. Images must be public HTTP(S) URLs or site-relative paths when
// RenderSiteSocialCardHTML is used for an in-browser preview.
type SiteSocialCard struct {
	Kind           string
	Eyebrow        string
	Title          string
	TitleSuffix    string
	Subtitle       string
	Location       string
	Footer         string
	AccentColor    string
	TextColor      string
	ValueLabel     string
	Value          string
	ValueSuffix    string
	Callout        string
	HeroImage      string
	SponsorLabel   string
	SponsorLogos   []string
	SponsorNames   []string
	PoweredByLogos []string
	PoweredByNames []string
	Images         []string
	ImageLabels    []string
	Details        []string
	Badges         []string
	Stats          []SiteSocialCardStat
	MapImage       string
	MapPoints      []SiteSocialCardMapPoint
	ProfileHandle  string
	XHandle        string
	GitHubHandle   string
}

type siteSocialCardTemplateImage struct {
	URL   template.URL
	Label string
}

type siteSocialCardTemplateMapPoint struct {
	Style    template.CSS
	Upcoming bool
}

type siteSocialCardTemplateData struct {
	SiteSocialCard
	BrandFontURL      template.URL
	ChalkFontURL      template.URL
	CardHeroImage     template.URL
	CardSponsors      []siteSocialCardTemplateImage
	CardPoweredBy     []siteSocialCardTemplateImage
	CardImages        []siteSocialCardTemplateImage
	CardMapImage      template.URL
	CardMapPoints     []siteSocialCardTemplateMapPoint
	CardStyle         template.CSS
	AwardValueWhole   string
	AwardValueDecimal string
	AwardValueUnit    string
}

// SiteSocialCardID fingerprints the editable template, logo, and public page
// data. It is used as the metadata URL version so social-network caches refresh
// when either the content or the design changes.
func SiteSocialCardID(card SiteSocialCard) string {
	hash := sha256.New()
	hash.Write([]byte(siteSocialCardTemplateSource))
	hash.Write([]byte{0})
	hash.Write(socialCardBrandFont)
	hash.Write([]byte{0})
	hash.Write(socialCardChalkFont)
	cardJSON, err := json.Marshal(card)
	if err != nil {
		panic(fmt.Sprintf("fingerprint site social card: %s", err))
	}
	hash.Write([]byte{0})
	hash.Write(cardJSON)
	return hex.EncodeToString(hash.Sum(nil)[:6])
}

func renderSiteSocialCardHTML(card SiteSocialCard, brandFontURL, chalkFontURL template.URL) ([]byte, error) {
	images := make([]siteSocialCardTemplateImage, 0, len(card.Images))
	for index, raw := range card.Images {
		if strings.TrimSpace(raw) != "" {
			label := ""
			if index < len(card.ImageLabels) {
				label = card.ImageLabels[index]
			}
			images = append(images, siteSocialCardTemplateImage{URL: template.URL(raw), Label: label})
		}
	}
	sponsors := make([]siteSocialCardTemplateImage, 0, len(card.SponsorLogos))
	for index, raw := range card.SponsorLogos {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		name := ""
		if index < len(card.SponsorNames) {
			name = card.SponsorNames[index]
		}
		sponsors = append(sponsors, siteSocialCardTemplateImage{URL: template.URL(raw), Label: name})
	}
	poweredBy := make([]siteSocialCardTemplateImage, 0, len(card.PoweredByLogos))
	for index, raw := range card.PoweredByLogos {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		name := ""
		if index < len(card.PoweredByNames) {
			name = card.PoweredByNames[index]
		}
		poweredBy = append(poweredBy, siteSocialCardTemplateImage{URL: template.URL(raw), Label: name})
	}
	mapPoints := make([]siteSocialCardTemplateMapPoint, 0, len(card.MapPoints))
	for _, point := range card.MapPoints {
		x := max(0, min(100, point.X))
		y := max(0, min(100, point.Y))
		mapPoints = append(mapPoints, siteSocialCardTemplateMapPoint{
			Style:    template.CSS(fmt.Sprintf("left:%.2f%%;top:%.2f%%;", x, y)),
			Upcoming: point.Upcoming,
		})
	}
	var rendered bytes.Buffer
	accent := socialCardColor(card.AccentColor, "#0a0a0a")
	ink := socialCardColor(card.TextColor, "#f6f1e8")
	awardValueWhole, awardValueDecimal, awardValueUnit := splitCompactSocialCardValue(card.Value)
	if err := siteSocialCardTemplate.Execute(&rendered, siteSocialCardTemplateData{
		SiteSocialCard:    card,
		BrandFontURL:      brandFontURL,
		ChalkFontURL:      chalkFontURL,
		CardHeroImage:     template.URL(card.HeroImage),
		CardSponsors:      sponsors,
		CardPoweredBy:     poweredBy,
		CardImages:        images,
		CardMapImage:      template.URL(card.MapImage),
		CardMapPoints:     mapPoints,
		CardStyle:         template.CSS("--card-accent:" + accent + ";--card-ink:" + ink + ";"),
		AwardValueWhole:   awardValueWhole,
		AwardValueDecimal: awardValueDecimal,
		AwardValueUnit:    awardValueUnit,
	}); err != nil {
		return nil, fmt.Errorf("render site social card template: %w", err)
	}
	return rendered.Bytes(), nil
}

func splitCompactSocialCardValue(value string) (whole, decimal, unit string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", ""
	}
	last := value[len(value)-1]
	if last == 'M' || last == 'k' || last == 'K' {
		unit = value[len(value)-1:]
		value = value[:len(value)-1]
	}
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		whole = value[:dot]
		decimal = value[dot:]
		if strings.Trim(decimal[1:], "0") == "" {
			decimal = ""
		}
	} else {
		whole = value
	}
	if whole == "" {
		whole = "0"
	}
	return whole, decimal, unit
}

func socialCardColor(value, fallback string) string {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[0] != '#' {
		return fallback
	}
	for _, r := range value[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return fallback
		}
	}
	return value
}

func RenderSiteSocialCardHTML(card SiteSocialCard) ([]byte, error) {
	return renderSiteSocialCardHTML(card, template.URL("/static/fonts/Ubuntu-BoldItalic.ttf"), template.URL("/static/fonts/PermanentMarker-Regular.ttf"))
}

func renderSocialCardHTML(productImageURL template.URL, productName, productPrice string, brandFontURL template.URL) ([]byte, error) {
	var rendered bytes.Buffer
	if err := socialCardTemplate.Execute(&rendered, socialCardTemplateData{
		ProductImageURL: productImageURL,
		ProductName:     strings.TrimSpace(productName),
		ProductPrice:    strings.TrimSpace(productPrice),
		BrandFontURL:    brandFontURL,
	}); err != nil {
		return nil, fmt.Errorf("render social card template: %w", err)
	}
	return rendered.Bytes(), nil
}

// RenderSocialCardHTML renders the editable card template for browser previews.
// It accepts site-relative and HTTP(S) product-image URLs; local file URLs are
// reserved for the internal image-generation path below.
func RenderSocialCardHTML(productImageURL, productName, productPrice string) ([]byte, error) {
	productImageURL = strings.TrimSpace(productImageURL)
	if productImageURL == "" {
		return nil, fmt.Errorf("product image URL is required")
	}
	parsed, err := url.Parse(productImageURL)
	if err != nil {
		return nil, fmt.Errorf("parse product image URL: %w", err)
	}
	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported product image URL scheme %q", parsed.Scheme)
	}
	return renderSocialCardHTML(
		template.URL(productImageURL),
		productName,
		productPrice,
		template.URL("/static/fonts/Ubuntu-BoldItalic.ttf"),
	)
}

// MakeAVIF transcodes any image bytes into AVIF. When `size > 0`, the
// output is force-scaled to size×size with lanczos resampling (used
// by the speaker-photo pipeline, where photos are pre-cropped square).
// When `size <= 0`, the original aspect ratio is preserved — used by
// talk cliparts which aren't all square.
func MakeAVIF(data []byte, size int) ([]byte, error) {
	in, err := os.CreateTemp("", "imgproc-in-*")
	if err != nil {
		return nil, fmt.Errorf("tempfile: %w", err)
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(data); err != nil {
		in.Close()
		return nil, fmt.Errorf("write input: %w", err)
	}
	in.Close()

	out, err := os.CreateTemp("", "imgproc-out-*.avif")
	if err != nil {
		return nil, fmt.Errorf("tempfile: %w", err)
	}
	outName := out.Name()
	out.Close()
	defer os.Remove(outName)

	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", in.Name(),
	}
	if size > 0 {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:%d:flags=lanczos", size, size))
	}
	args = append(args,
		"-c:v", "libaom-av1",
		"-still-picture", "1",
		"-cpu-used", "8",
		outName,
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if size > 0 {
			return nil, fmt.Errorf("ffmpeg %dx%d: %w (stderr: %s)", size, size, err, stderr.String())
		}
		return nil, fmt.Errorf("ffmpeg avif: %w (stderr: %s)", err, stderr.String())
	}
	return os.ReadFile(outName)
}

// MakeSocialCardJPEG renders merch_social_card.html in headless Chrome and
// returns a large-card JPEG suitable for X and Open Graph link previews.
func MakeSocialCardJPEG(data []byte, productName, productPrice string) ([]byte, error) {
	in, err := os.CreateTemp("", "imgproc-social-in-*")
	if err != nil {
		return nil, fmt.Errorf("tempfile: %w", err)
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(data); err != nil {
		in.Close()
		return nil, fmt.Errorf("write input: %w", err)
	}
	in.Close()

	htmlFile, err := os.CreateTemp("", "imgproc-social-card-*.html")
	if err != nil {
		return nil, fmt.Errorf("tempfile: %w", err)
	}
	htmlName := htmlFile.Name()
	defer os.Remove(htmlName)

	imageURL := (&url.URL{Scheme: "file", Path: in.Name()}).String()
	brandFontURL := "data:font/ttf;base64," + base64.StdEncoding.EncodeToString(socialCardBrandFont)
	renderedHTML, err := renderSocialCardHTML(template.URL(imageURL), productName, productPrice, template.URL(brandFontURL))
	if err != nil {
		htmlFile.Close()
		return nil, err
	}
	if _, err := htmlFile.Write(renderedHTML); err != nil {
		htmlFile.Close()
		return nil, fmt.Errorf("write social card template: %w", err)
	}
	if err := htmlFile.Close(); err != nil {
		return nil, fmt.Errorf("close social card template: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), socialCardRenderTimeout)
	defer cancel()
	select {
	case socialCardChromeSlots <- struct{}{}:
		defer func() { <-socialCardChromeSlots }()
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for social card renderer: %w", ctx.Err())
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.Flag("allow-file-access-from-files", true))
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	pageURL := (&url.URL{Scheme: "file", Path: htmlName}).String()
	var imageLoaded bool
	var screenshot []byte
	err = chromedp.Run(browserCtx,
		emulation.SetDeviceMetricsOverride(SocialCardWidth, SocialCardHeight, 1, false),
		chromedp.Navigate(pageURL),
		chromedp.Evaluate(`(() => {
			const image = document.getElementById("product-image");
			return Boolean(image && image.complete && image.naturalWidth > 0 && image.naturalHeight > 0);
		})()`, &imageLoaded),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var captureErr error
			screenshot, captureErr = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				WithFromSurface(true).
				WithClip(&page.Viewport{
					X:      0,
					Y:      0,
					Width:  SocialCardWidth,
					Height: SocialCardHeight,
					Scale:  1,
				}).
				Do(ctx)
			return captureErr
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("render social card HTML: %w", err)
	}
	if !imageLoaded {
		return nil, fmt.Errorf("render social card HTML: product image could not be loaded")
	}
	card, err := png.Decode(bytes.NewReader(screenshot))
	if err != nil {
		return nil, fmt.Errorf("decode social card screenshot: %w", err)
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, card, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode social card JPEG: %w", err)
	}
	return output.Bytes(), nil
}

// MakeSiteSocialCardJPEG renders the shared page-card template in headless
// Chrome. Unlike the merch renderer, its source images are already-public URLs.
func MakeSiteSocialCardJPEG(card SiteSocialCard) ([]byte, error) {
	htmlFile, err := os.CreateTemp("", "imgproc-site-social-card-*.html")
	if err != nil {
		return nil, fmt.Errorf("tempfile: %w", err)
	}
	htmlName := htmlFile.Name()
	defer os.Remove(htmlName)

	brandFontURL := "data:font/ttf;base64," + base64.StdEncoding.EncodeToString(socialCardBrandFont)
	chalkFontURL := "data:font/ttf;base64," + base64.StdEncoding.EncodeToString(socialCardChalkFont)
	renderedHTML, err := renderSiteSocialCardHTML(card, template.URL(brandFontURL), template.URL(chalkFontURL))
	if err != nil {
		htmlFile.Close()
		return nil, err
	}
	if _, err := htmlFile.Write(renderedHTML); err != nil {
		htmlFile.Close()
		return nil, fmt.Errorf("write site social card template: %w", err)
	}
	if err := htmlFile.Close(); err != nil {
		return nil, fmt.Errorf("close site social card template: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), socialCardRenderTimeout)
	defer cancel()
	select {
	case socialCardChromeSlots <- struct{}{}:
		defer func() { <-socialCardChromeSlots }()
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for social card renderer: %w", ctx.Err())
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.Flag("allow-file-access-from-files", true))
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	pageURL := (&url.URL{Scheme: "file", Path: htmlName}).String()
	var screenshot []byte
	err = chromedp.Run(browserCtx,
		emulation.SetDeviceMetricsOverride(SocialCardWidth, SocialCardHeight, 1, false),
		chromedp.Navigate(pageURL),
		chromedp.WaitVisible(`[data-card-ready="true"]`, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var captureErr error
			screenshot, captureErr = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				WithFromSurface(true).
				WithClip(&page.Viewport{Width: SocialCardWidth, Height: SocialCardHeight, Scale: 1}).
				Do(ctx)
			return captureErr
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("render site social card HTML: %w", err)
	}
	decoded, err := png.Decode(bytes.NewReader(screenshot))
	if err != nil {
		return nil, fmt.Errorf("decode site social card screenshot: %w", err)
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode site social card JPEG: %w", err)
	}
	return output.Bytes(), nil
}
