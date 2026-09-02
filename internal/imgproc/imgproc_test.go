package imgproc

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"testing"
)

func makeTestJPEG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encoding test jpeg: %v", err)
	}
	return buf.Bytes()
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH; skipping AVIF roundtrip test")
	}
}

func requireChrome(t *testing.T) {
	t.Helper()
	for _, executable := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if _, err := exec.LookPath(executable); err == nil {
			return
		}
	}
	for _, executable := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if _, err := os.Stat(executable); err == nil {
			return
		}
	}
	t.Skip("Chrome or Chromium not installed; skipping HTML social-card render test")
}

func TestMakeAVIF_RoundTrip(t *testing.T) {
	requireFFmpeg(t)

	in := makeTestJPEG(t, 1024)

	for _, size := range []int{800, 400} {
		size := size
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			out, err := MakeAVIF(in, size)
			if err != nil {
				t.Fatalf("MakeAVIF(%d): %v", size, err)
			}
			if len(out) < 100 {
				t.Errorf("output suspiciously small: %d bytes", len(out))
			}
			// ISO BMFF: first 4 bytes are box length, then "ftypavif"
			if len(out) < 12 || !bytes.Equal(out[4:12], []byte("ftypavif")) {
				t.Errorf("output is not AVIF; header=% x", out[:min(16, len(out))])
			}
		})
	}
}

func TestMakeAVIF_BadInput(t *testing.T) {
	requireFFmpeg(t)

	_, err := MakeAVIF([]byte("definitely not an image"), 400)
	if err == nil {
		t.Fatal("expected error for non-image input, got nil")
	}
}

func TestMakeAVIF_EmptyInput(t *testing.T) {
	requireFFmpeg(t)

	_, err := MakeAVIF(nil, 400)
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}

func TestMakeSocialCardJPEG(t *testing.T) {
	requireChrome(t)

	out, err := MakeSocialCardJPEG(makeTestJPEG(t, 417))
	if err != nil {
		t.Fatalf("MakeSocialCardJPEG: %v", err)
	}
	card, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode social card: %v", err)
	}
	if got := card.Bounds().Dx(); got != SocialCardWidth {
		t.Fatalf("social card width = %d, want %d", got, SocialCardWidth)
	}
	if got := card.Bounds().Dy(); got != SocialCardHeight {
		t.Fatalf("social card height = %d, want %d", got, SocialCardHeight)
	}
	if len(out) >= 3<<20 {
		t.Fatalf("social card size = %d bytes, want less than 3 MiB", len(out))
	}
	corner := color.RGBAModel.Convert(card.At(0, 0)).(color.RGBA)
	if !closeColor(corner, color.RGBA{R: 0xf6, G: 0xf3, B: 0xee, A: 0xff}, 4) {
		t.Fatalf("social card corner = %#v, want template background #f6f3ee", corner)
	}
	center := color.RGBAModel.Convert(card.At(SocialCardWidth/2, SocialCardHeight/2)).(color.RGBA)
	if closeColor(center, corner, 8) {
		t.Fatalf("social card center = %#v, want rendered product image", center)
	}
}

func closeColor(got, want color.RGBA, tolerance uint8) bool {
	closeChannel := func(a, b uint8) bool {
		if a > b {
			return a-b <= tolerance
		}
		return b-a <= tolerance
	}
	return closeChannel(got.R, want.R) && closeChannel(got.G, want.G) && closeChannel(got.B, want.B)
}

func TestMakeSocialCardJPEGFromAVIF(t *testing.T) {
	requireChrome(t)
	requireFFmpeg(t)

	avif, err := MakeAVIF(makeTestJPEG(t, 417), 0)
	if err != nil {
		t.Fatalf("MakeAVIF: %v", err)
	}
	if _, err := MakeSocialCardJPEG(avif); err != nil {
		t.Fatalf("MakeSocialCardJPEG from AVIF: %v", err)
	}
}

func TestMakeSocialCardJPEGRejectsBadInput(t *testing.T) {
	requireChrome(t)
	if _, err := MakeSocialCardJPEG([]byte("not an image")); err == nil {
		t.Fatal("MakeSocialCardJPEG accepted invalid image bytes")
	}
}

func TestShortID_Deterministic(t *testing.T) {
	a := []byte("hello world")
	if got, want := ShortID(a), ShortID(a); got != want {
		t.Errorf("ShortID is not deterministic: %s vs %s", got, want)
	}
}

func TestShortID_DifferentInputs(t *testing.T) {
	if ShortID([]byte("foo")) == ShortID([]byte("bar")) {
		t.Error("distinct inputs collided to the same ShortID")
	}
}

func TestShortID_Format(t *testing.T) {
	id := ShortID([]byte("anything"))
	if len(id) != 12 {
		t.Errorf("expected 12-char hex; got %d-char %q", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex char %q in ShortID %q", c, id)
		}
	}
}

func TestSocialCardID(t *testing.T) {
	data := []byte("product image")
	if got, want := SocialCardID(data), SocialCardID(data); got != want {
		t.Fatalf("SocialCardID is not deterministic: %s vs %s", got, want)
	}
	if got := SocialCardID(data); len(got) != 12 {
		t.Fatalf("SocialCardID length = %d, want 12", len(got))
	}
	if SocialCardID(data) == ShortID(data) {
		t.Fatal("SocialCardID does not include the template fingerprint")
	}
}

func TestSiteSocialCardIDTracksContent(t *testing.T) {
	card := SiteSocialCard{Kind: "conference", Title: "bitcoin++ Vienna"}
	first := SiteSocialCardID(card)
	if got := SiteSocialCardID(card); got != first {
		t.Fatalf("SiteSocialCardID is not deterministic: %q != %q", got, first)
	}
	card.Title = "bitcoin++ Austin"
	if got := SiteSocialCardID(card); got == first {
		t.Fatal("SiteSocialCardID did not change with rendered content")
	}
}

func TestRenderSiteSocialCardHTML(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{
		Kind: "person", Eyebrow: "community profile", Title: "Mara Chen",
		Subtitle: "Talk: Building Bitcoin", Stats: []SiteSocialCardStat{{Value: "3", Label: "talks"}},
	})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	for _, want := range []string{"Mara Chen", "Building Bitcoin", "community profile", `data-card-ready="false"`} {
		if !bytes.Contains(html, []byte(want)) {
			t.Fatalf("rendered site card missing %q", want)
		}
	}
}

func TestMakeSiteSocialCardJPEG(t *testing.T) {
	requireChrome(t)
	out, err := MakeSiteSocialCardJPEG(SiteSocialCard{
		Kind: "home", Eyebrow: "bitcoin++", Title: "Developing the frontier of bitcoin.",
		Subtitle: "Technical conferences for bitcoin builders.",
		Images:   []string{"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(makeTestJPEG(t, 417))},
	})
	if err != nil {
		t.Fatalf("MakeSiteSocialCardJPEG: %v", err)
	}
	card, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode site social card: %v", err)
	}
	if got := card.Bounds().Dx(); got != SocialCardWidth {
		t.Fatalf("site social card width = %d, want %d", got, SocialCardWidth)
	}
	if got := card.Bounds().Dy(); got != SocialCardHeight {
		t.Fatalf("site social card height = %d, want %d", got, SocialCardHeight)
	}
}

func TestRenderSocialCardHTML(t *testing.T) {
	rendered, err := RenderSocialCardHTML("/static/img/merch/core-hat.avif")
	if err != nil {
		t.Fatalf("RenderSocialCardHTML: %v", err)
	}
	if !bytes.Contains(rendered, []byte(`src="/static/img/merch/core-hat.avif"`)) {
		t.Fatalf("rendered social card does not contain seeded image URL: %s", rendered)
	}
	if !bytes.Contains(rendered, []byte(`src="/static/img/logo_blk.svg"`)) {
		t.Fatalf("rendered social card does not contain bitcoin++ logo: %s", rendered)
	}
	if !bytes.Contains(rendered, []byte(`>merch shop</p>`)) {
		t.Fatalf("rendered social card does not contain merch shop label: %s", rendered)
	}
	if _, err := RenderSocialCardHTML("javascript:alert(1)"); err == nil {
		t.Fatal("RenderSocialCardHTML accepted unsafe URL scheme")
	}
}
