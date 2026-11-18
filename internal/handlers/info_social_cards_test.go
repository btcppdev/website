package handlers

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

// Exercise each page's real metadata and its corresponding public image endpoint
// without requiring a database, object storage, or Chrome for the HTTP checks.
func TestInfoSocialCardPageMetadataAndEndpoint(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0)}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	storage, renderer := siteSocialCardStorage, renderSiteSocialCardJPEG
	siteSocialCardCache.Lock()
	cached := siteSocialCardCache.images
	siteSocialCardCache.images = make(map[string][]byte)
	siteSocialCardCache.Unlock()
	t.Cleanup(func() {
		siteSocialCardStorage, renderSiteSocialCardJPEG = storage, renderer
		siteSocialCardCache.Lock()
		siteSocialCardCache.images = cached
		siteSocialCardCache.Unlock()
	})
	siteSocialCardStorage.isConfigured = func() bool { return false }
	renderSiteSocialCardJPEG = func(card imgproc.SiteSocialCard) ([]byte, error) { return []byte("rendered " + card.Kind), nil }
	for _, test := range []struct {
		path, kind string
		handler    func(http.ResponseWriter, *http.Request, *config.AppContext)
	}{
		{"/contact", "contact", ContactPage},
		{"/developers/api", "api", DevelopersAPI},
	} {
		t.Run(test.kind, func(t *testing.T) {
			card, err := loadSiteSocialCard(ctx, test.kind, "")
			if err != nil {
				t.Fatal(err)
			}
			imagePath := siteSocialCardPath(test.kind, "", card)
			page := httptest.NewRecorder()
			test.handler(page, httptest.NewRequest(http.MethodGet, test.path, nil), ctx)
			if page.Code != http.StatusOK {
				t.Fatalf("page status = %d: %s", page.Code, page.Body.String())
			}
			for _, want := range []string{
				`property="og:image" content="` + SEOHost + imagePath + `"`,
				`name="twitter:image" content="` + SEOHost + imagePath + `"`,
				`property="og:image:type" content="image/jpeg"`,
				`property="og:image:width" content="1200"`,
				`property="og:image:height" content="630"`,
			} {
				if !strings.Contains(page.Body.String(), want) {
					t.Fatalf("page missing %s", want)
				}
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				request := mux.SetURLVars(httptest.NewRequest(method, imagePath, nil), map[string]string{"kind": test.kind})
				image := httptest.NewRecorder()
				ServeSiteSocialCard(image, request, ctx)
				if image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/jpeg" {
					t.Fatalf("%s image response = %d %s", method, image.Code, image.Header().Get("Content-Type"))
				}
				if method == http.MethodGet && image.Body.String() != "rendered "+test.kind {
					t.Fatalf("wrong card served: %s", image.Body.String())
				}
				if method == http.MethodHead && image.Body.Len() != 0 {
					t.Fatal("HEAD returned an image body")
				}
				request.Header.Set("If-None-Match", image.Header().Get("ETag"))
				cachedImage := httptest.NewRecorder()
				ServeSiteSocialCard(cachedImage, request, ctx)
				if cachedImage.Code != http.StatusNotModified {
					t.Fatalf("conditional image status = %d", cachedImage.Code)
				}
			}
		})
	}
}
