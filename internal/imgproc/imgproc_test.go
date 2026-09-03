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
	"reflect"
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

	out, err := MakeSocialCardJPEG(makeTestJPEG(t, 417), "Core Hat", "$35")
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
	corner := color.RGBAModel.Convert(card.At(10, 10)).(color.RGBA)
	if !closeColor(corner, color.RGBA{R: 0xf9, G: 0xaf, B: 0x5e, A: 0xff}, 4) {
		t.Fatalf("social card background = %#v, want template background #f9af5e", corner)
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
	if _, err := MakeSocialCardJPEG(avif, "Core Hat", "$35"); err != nil {
		t.Fatalf("MakeSocialCardJPEG from AVIF: %v", err)
	}
}

func TestMakeSocialCardJPEGRejectsBadInput(t *testing.T) {
	requireChrome(t)
	if _, err := MakeSocialCardJPEG([]byte("not an image"), "Core Hat", "$35"); err == nil {
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
	if got, want := SocialCardID(data, "Core Hat", "$35"), SocialCardID(data, "Core Hat", "$35"); got != want {
		t.Fatalf("SocialCardID is not deterministic: %s vs %s", got, want)
	}
	if got := SocialCardID(data, "Core Hat", "$35"); len(got) != 12 {
		t.Fatalf("SocialCardID length = %d, want 12", len(got))
	}
	if SocialCardID(data, "Core Hat", "$35") == ShortID(data) {
		t.Fatal("SocialCardID does not include the template fingerprint")
	}
	if SocialCardID(data, "Core Hat", "$35") == SocialCardID(data, "Libre Relay Hat", "$35") {
		t.Fatal("SocialCardID does not include the product name")
	}
	if SocialCardID(data, "Core Hat", "$35") == SocialCardID(data, "Core Hat", "$40") {
		t.Fatal("SocialCardID does not include the product price")
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
	first = SiteSocialCardID(card)
	card.ImageLabels = []string{"economics / edition 17"}
	if got := SiteSocialCardID(card); got == first {
		t.Fatal("SiteSocialCardID did not change with an image label")
	}
	first = SiteSocialCardID(card)
	card.Location = "Austin · Assembly Hall"
	if got := SiteSocialCardID(card); got == first {
		t.Fatal("SiteSocialCardID did not change with location content")
	}
	first = SiteSocialCardID(card)
	card.MapImage = "/static/img/home/worldmap.svg"
	card.MapPoints = []SiteSocialCardMapPoint{{X: 24, Y: 42, Upcoming: true}}
	if got := SiteSocialCardID(card); got == first {
		t.Fatal("SiteSocialCardID did not change with map content")
	}
	first = SiteSocialCardID(card)
	card.PoweredByLogos = []string{"/title-sponsor.svg"}
	card.PoweredByNames = []string{"Title Partner"}
	if got := SiteSocialCardID(card); got == first {
		t.Fatal("SiteSocialCardID did not change with powered-by sponsor content")
	}
}

func TestSiteSocialCardIDTracksEveryCardField(t *testing.T) {
	cardType := reflect.TypeOf(SiteSocialCard{})
	base := SiteSocialCardID(SiteSocialCard{})
	for fieldIndex := 0; fieldIndex < cardType.NumField(); fieldIndex++ {
		field := cardType.Field(fieldIndex)
		t.Run(field.Name, func(t *testing.T) {
			value := reflect.New(cardType).Elem()
			setSocialCardFingerprintTestValue(t, value.Field(fieldIndex))
			card := value.Interface().(SiteSocialCard)
			if got := SiteSocialCardID(card); got == base {
				t.Fatalf("SiteSocialCardID did not change when %s changed", field.Name)
			}
		})
	}
}

func setSocialCardFingerprintTestValue(t *testing.T, value reflect.Value) {
	t.Helper()
	switch value.Kind() {
	case reflect.String:
		value.SetString("changed")
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(1)
	case reflect.Slice:
		element := reflect.New(value.Type().Elem()).Elem()
		setSocialCardFingerprintTestValue(t, element)
		value.Set(reflect.Append(value, element))
	case reflect.Struct:
		if value.NumField() == 0 {
			t.Fatalf("cannot create a non-zero test value for %s", value.Type())
		}
		setSocialCardFingerprintTestValue(t, value.Field(0))
	default:
		t.Fatalf("add a fingerprint test value for new field type %s", value.Type())
	}
}

func TestRenderConferenceSiteSocialCardHTMLUsesHomeCardLayout(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{
		Kind: "conference", Eyebrow: "technical bitcoin event · October 2026", Title: "Local Dev", TitleSuffix: "signet edition",
		Subtitle: "Build the future together.", Location: "Austin · Assembly Hall",
		Images: []string{"/static/img/dev26/leading.png"}, MapImage: "/static/img/home/worldmap.svg",
		MapPoints:    []SiteSocialCardMapPoint{{X: 24, Y: 42, Upcoming: true}},
		SponsorLogos: []string{"/headline.svg"}, SponsorNames: []string{"Headline Partner"},
		PoweredByLogos: []string{"/title.svg"}, PoweredByNames: []string{"Title Partner"},
	})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	for _, want := range []string{"kind-conference has-title-sponsor", "conference-map-backdrop", "conference-title-block", "conference-date", `class="conference-title__lead">Local Dev</span>`, `class="conference-title__suffix">signet edition</span>`, "Build the future together.", "conference-map-backdrop__point", "left:24.00%;top:42.00%;", ".kind-conference .visual { inset: 0 auto 0 0;", "social-card-sponsor-ticker", "Headline Partner", "conference-powered-by", "powered by", "Title Partner"} {
		if !bytes.Contains(html, []byte(want)) {
			t.Fatalf("rendered conference card missing %q", want)
		}
	}
}

func TestRenderSiteSocialCardHTML(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{
		Kind: "events", Eyebrow: "community profile", Title: "Mara Chen",
		Subtitle: "Talk: Building Bitcoin", Images: []string{"/static/img/atx25/leading.jpg"},
		ImageLabels: []string{"mempool / edition 9"}, Stats: []SiteSocialCardStat{{Value: "3", Label: "talks"}},
	})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	for _, want := range []string{"Building Bitcoin", "community profile", "mempool / edition 9", `data-card-ready="false"`, "imageLoadTimeoutMs", "naturalWidth"} {
		if !bytes.Contains(html, []byte(want)) {
			t.Fatalf("rendered site card missing %q", want)
		}
	}
	if bytes.Contains(html, []byte("Mara Chen")) {
		t.Fatal("events card rendered its removed bottom title")
	}
}

func TestRenderHomeSiteSocialCardHTMLIncludesSharedFooter(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{
		Kind: "home", Title: "Developing the frontier of bitcoin.", MapImage: "/static/img/home/worldmap.svg",
		MapPoints:    []SiteSocialCardMapPoint{{X: 24, Y: 42, Upcoming: true}, {X: 52, Y: 36}},
		SponsorLabel: "2026 headline partners", SponsorLogos: []string{"/headline.svg"}, SponsorNames: []string{"Headline Partner"},
	})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	for _, want := range []string{"kind-home", "Ubuntu Brand", "wordmark__pluses", "Developing the", `<span class="home-serif">frontier</span>`, "of bitcoin.", "btcpp.dev", "card-footer", "card-sparkles", "✨", "home-card-map", "home-card-map__point is-upcoming", "left:24.00%;top:42.00%;", "2026 headline partners", "Headline Partner"} {
		if !bytes.Contains(html, []byte(want)) {
			t.Fatalf("rendered home card missing %q", want)
		}
	}
	// The declaration plus the home hero and footer literal bitcoin++ wordmarks
	// are the only permitted uses of this face.
	if got := bytes.Count(html, []byte(`font-family: "Ubuntu Brand"`)); got != 3 {
		t.Fatalf("Ubuntu Brand font-family usage count = %d, want 3 brand-only uses", got)
	}
}

func TestRenderAwardSiteSocialCardHTML(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{
		Kind: "award", Eyebrow: "Example Sponsor", Title: "Best Signet Infrastructure",
		Subtitle: "Build reliable tools.", AccentColor: "#2563eb", TextColor: "#ffffff",
		ValueLabel: "Prize value", Value: "1.75M", ValueSuffix: "sats",
		Callout: "Local Dev · Austin, TX · Oct 1–3, 2026",
		Details: []string{"Hardware signing device"},
	})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	for _, want := range []string{"kind-award", "award-event", "award-logo", "Local Dev", "Best Signet Infrastructure", `award-value__whole">1`, `award-value__decimal">.75`, `award-value__unit">M`, "Presented by", "Example Sponsor", "award-prize-details", "Hardware signing device", "--card-accent:#2563eb"} {
		if !bytes.Contains(html, []byte(want)) {
			t.Fatalf("rendered award card missing %q", want)
		}
	}
	for _, unwanted := range []string{"award-trophy", "award-trophy__word"} {
		if bytes.Contains(html, []byte(unwanted)) {
			t.Fatalf("rendered award card still contains %q", unwanted)
		}
	}
}

func TestSplitCompactSocialCardValue(t *testing.T) {
	for _, test := range []struct {
		value                string
		whole, decimal, unit string
	}{
		{value: "1.0M", whole: "1", unit: "M"},
		{value: "750k", whole: "750", unit: "k"},
		{value: "3", whole: "3"},
	} {
		whole, decimal, unit := splitCompactSocialCardValue(test.value)
		if whole != test.whole || decimal != test.decimal || unit != test.unit {
			t.Fatalf("splitCompactSocialCardValue(%q) = (%q, %q, %q), want (%q, %q, %q)", test.value, whole, decimal, unit, test.whole, test.decimal, test.unit)
		}
	}
}

func TestRenderHackathonSiteSocialCardIncludesSponsorTicker(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{
		Kind: "hackathon", Title: "Build something real.",
		SponsorLogos: []string{"/headline.svg", "/title.svg", "/hackathon.svg"},
		SponsorNames: []string{"Headline Partner", "Title Partner", "Hackathon Partner"},
	})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	for _, want := range []string{"social-card-sponsor-ticker", "social-card-sponsor-march", "grayscale(100%)", "Headline Partner", "Title Partner", "Hackathon Partner"} {
		if !bytes.Contains(html, []byte(want)) {
			t.Fatalf("rendered hackathon card missing %q", want)
		}
	}
}

func TestRenderSiteSocialCardHTMLRejectsUnsafePaletteValue(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{Kind: "conference", Title: "Event", AccentColor: "red; display:none"})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	if bytes.Contains(html, []byte("display:none")) || !bytes.Contains(html, []byte("--card-accent:#0a0a0a")) {
		t.Fatalf("unsafe palette value reached rendered CSS: %s", html)
	}
}

func TestPersonSiteSocialCardUsesAustereImageRail(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{Kind: "person", Title: "Mara Chen", ProfileHandle: "mara", XHandle: "mara_x", GitHubHandle: "mara-gh", Badges: []string{"First Place"}})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	for _, want := range []string{".kind-person .images", "grid-template-columns: repeat(5", "object-fit: contain", ".kind-person .copy { inset: 395px", "fitPersonName", "name.scrollWidth <= name.clientWidth", "fitCurrentLayout() >= 125", "person-name-line", "X @mara_x", "person-social-handle", "@mara-gh"} {
		if !bytes.Contains(html, []byte(want)) {
			t.Fatalf("rendered person card missing %q", want)
		}
	}
	for _, unwanted := range []string{`<div class="person-achievement-stickers"`, `<div class="card-sparkles"`, `<span class="person-profile-handle">`} {
		if bytes.Contains(html, []byte(unwanted)) {
			t.Fatalf("rendered person card contains %q", unwanted)
		}
	}
}

func TestWhoIsSiteSocialCardIncludesSparkleField(t *testing.T) {
	html, err := RenderSiteSocialCardHTML(SiteSocialCard{Kind: "whois", Title: "The people building bitcoin."})
	if err != nil {
		t.Fatalf("RenderSiteSocialCardHTML: %v", err)
	}
	if !bytes.Contains(html, []byte(`class="whois-sparkles"`)) {
		t.Fatalf("rendered whois card is missing its sparkle field: %s", html)
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
	rendered, err := RenderSocialCardHTML("/static/img/merch/core-hat.avif", "Bitcoin Core Hat", "$35")
	if err != nil {
		t.Fatalf("RenderSocialCardHTML: %v", err)
	}
	if !bytes.Contains(rendered, []byte(`src="/static/img/merch/core-hat.avif"`)) {
		t.Fatalf("rendered social card does not contain seeded image URL: %s", rendered)
	}
	if !bytes.Contains(rendered, []byte(`url("/static/fonts/Ubuntu-BoldItalic.ttf")`)) {
		t.Fatalf("rendered social card does not contain the brand font: %s", rendered)
	}
	for _, want := range []string{`>merch shop</div>`, `Wear the <em>frontier.</em>`, `<h1 class="social-card__title">Bitcoin Core Hat</h1>`, `social-card__price-star`, `Price $35`, `>$35</div>`, `Developing the frontier of bitcoin.`, `btcpp.dev`, `brand-lockup`, `card-sparkles`, `✨`} {
		if !bytes.Contains(rendered, []byte(want)) {
			t.Fatalf("rendered social card does not contain %q: %s", want, rendered)
		}
	}
	// The declaration and the literal bitcoin++ footer wordmark are the only
	// permitted uses of the brand face in individual product cards.
	if got := bytes.Count(rendered, []byte(`font-family: "Ubuntu Brand"`)); got != 2 {
		t.Fatalf("Ubuntu Brand font-family usage count = %d, want 2 brand-only uses", got)
	}
	if _, err := RenderSocialCardHTML("javascript:alert(1)", "Unsafe Hat", "$35"); err == nil {
		t.Fatal("RenderSocialCardHTML accepted unsafe URL scheme")
	}
}
