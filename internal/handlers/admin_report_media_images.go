package handlers

import (
	"btcpp-web/external/spaces"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Studio thumbnail URLs expire. Store small image bytes in the dated snapshot
// instead of publishing signed URLs or depending on their future availability.
func reportThumbnail(ctx context.Context, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !(strings.HasSuffix(host, ".pscp.tv") || host == "pbs.twimg.com" || host == "i.ytimg.com") {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ""
	}
	client := http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	rawImage, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(rawImage) > 2<<20 {
		return ""
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(rawImage))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 16000000 {
		return ""
	}
	source, _, err := image.Decode(bytes.NewReader(rawImage))
	if err != nil {
		return ""
	}
	width := 480
	height := width * cfg.Height / cfg.Width
	if height < 1 || height > 960 {
		return ""
	}
	thumb := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := source.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			thumb.Set(x, y, source.At(bounds.Min.X+x*cfg.Width/width, bounds.Min.Y+y*cfg.Height/height))
		}
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, thumb, &jpeg.Options{Quality: 78}) != nil {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}

func (m reportMedia) ImageURL() template.URL {
	if thumbnail := m.thumbnailURL(); thumbnail != "" {
		return thumbnail
	}
	raw := strings.TrimSpace(m.SocialCard)
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		raw = spaces.PublicURL(strings.TrimPrefix(raw, "/"))
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return template.URL(u.String())
}

func (m reportMedia) thumbnailURL() template.URL {
	const prefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(m.Thumbnail, prefix) {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(m.Thumbnail, prefix))
	if err != nil {
		return ""
	}
	if _, err = jpeg.DecodeConfig(bytes.NewReader(raw)); err != nil {
		return ""
	}
	return template.URL(fmt.Sprintf("%s%s", prefix, base64.StdEncoding.EncodeToString(raw)))
}
