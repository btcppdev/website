package helpers

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestChromeMediaRendering(t *testing.T) {
	found := false
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
		if _, err := exec.LookPath(name); err == nil {
			found = true
			break
		}
	}
	if !found {
		if os.Getenv("BTCPP_REQUIRE_CHROME_TEST") != "" {
			t.Fatal("Chrome required but not installed")
		}
		t.Skip("Chrome not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><style>@page{size:4in 3in;margin:0}html,body{margin:0;background:#0a3954;height:288px}h1{margin:0}</style><body><h1>bitcoin++ report</h1></body>`))
	}))
	defer server.Close()
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 30*time.Second)
	defer timeout()
	page := &PDFPage{URL: server.URL, Width: 4, Height: 3}
	pngData, err := pngGrabber(ctx, page)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("invalid PNG: %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != 384 || bounds.Dy() != 288 {
		t.Fatalf("unexpected viewport: %v", bounds)
	}
	pdfData, err := pdfGrabber(ctx, page)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdfData, []byte("%PDF-")) || !bytes.Contains(pdfData, []byte("%%EOF")) {
		t.Fatal("invalid PDF output")
	}
}
