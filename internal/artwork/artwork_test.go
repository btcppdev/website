package artwork

import (
	"btcpp-web/internal/types"
	"os"
	"path/filepath"
	"testing"
)

func TestArtworkReplacementAndFallbacks(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := "static/img/event"
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"leading.png", "leading.avif", "logo-black.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	conf := &types.Conf{Tag: "event"}
	if got := Resolve(conf, "leading"); got.Original != "/static/img/event/leading.png" || got.AVIF != "/static/img/event/leading.avif" {
		t.Fatalf("bundled artwork: %+v", got)
	}
	conf.LeadingImageURL = "https://images.example/new.png"
	if got := Resolve(conf, "leading"); got.Web() != conf.LeadingImageURL || got.AVIF != "" {
		t.Fatalf("old AVIF overrode upload: %+v", got)
	}
	conf.LeadingImageAVIFURL = "https://images.example/new-web.avif"
	if Resolve(conf, "leading").Web() != conf.LeadingImageAVIFURL {
		t.Fatal("optimized upload not preferred")
	}
	if Email("https://btcpp.dev/", conf) != conf.LeadingImageURL {
		t.Fatal("email did not use original")
	}
	if Resolve(conf, "logo-black").Original != "/static/img/event/logo-black.png" {
		t.Fatal("logo fallback missing")
	}
	if Resolve(&types.Conf{Tag: "unknown"}, "leading").Web() != "" {
		t.Fatal("missing artwork falsely marked ready")
	}
	if Bundled("../event", "leading").Web() != "" {
		t.Fatal("unsafe path")
	}
}
