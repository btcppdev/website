package imgproc

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
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

// SocialCardID fingerprints both the product image and the HTML template.
// Editing the template therefore produces a new object key instead of leaving
// social networks and CDNs pointed at a cached rendering of the old layout.
func SocialCardID(data []byte) string {
	hash := sha256.New()
	hash.Write([]byte(socialCardTemplateSource))
	hash.Write([]byte{0})
	hash.Write(socialCardLogo)
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

//go:embed bitcoin_plus_plus_logo.svg
var socialCardLogo []byte

var socialCardTemplate = template.Must(template.New("merch_social_card.html").Parse(socialCardTemplateSource))

var socialCardChromeSlots = make(chan struct{}, 2)

type socialCardTemplateData struct {
	ProductImageURL template.URL
	LogoImageURL    template.URL
}

func renderSocialCardHTML(productImageURL, logoImageURL template.URL) ([]byte, error) {
	var rendered bytes.Buffer
	if err := socialCardTemplate.Execute(&rendered, socialCardTemplateData{
		ProductImageURL: productImageURL,
		LogoImageURL:    logoImageURL,
	}); err != nil {
		return nil, fmt.Errorf("render social card template: %w", err)
	}
	return rendered.Bytes(), nil
}

// RenderSocialCardHTML renders the editable card template for browser previews.
// It accepts site-relative and HTTP(S) product-image URLs; local file URLs are
// reserved for the internal image-generation path below.
func RenderSocialCardHTML(productImageURL string) ([]byte, error) {
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
		template.URL("/static/img/logo_blk.svg"),
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
func MakeSocialCardJPEG(data []byte) ([]byte, error) {
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
	logoURL := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(socialCardLogo)
	renderedHTML, err := renderSocialCardHTML(template.URL(imageURL), template.URL(logoURL))
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
