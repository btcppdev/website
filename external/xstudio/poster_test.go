package xstudio

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOversizedPosterFitsProxyBudget(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	rng := rand.New(rand.NewSource(1))
	rng.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	if source.Len() <= 1<<20 {
		t.Fatal("fixture must exceed a common proxy limit")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength >= 1<<20 {
			t.Error("multipart request too large")
			http.Error(w, "too large", 413)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		raw, _ := io.ReadAll(f)
		if len(raw) > posterUploadBudget || h.Filename != "poster.jpg" || h.Header.Get("Content-Type") != "image/jpeg" {
			t.Error("invalid converted poster metadata or size")
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || format != "jpeg" || cfg.Width != 1024 || cfg.Height != 1024 {
			t.Error("invalid image after compression")
		}
		io.WriteString(w, `{"value":"poster-1","error":null}`)
	}))
	defer server.Close()
	client, err := New(Config{Cookie: "auth=fake", IngestID: "fake", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.UploadPoster(context.Background(), "session", "poster.png", "image/png", bytes.NewReader(source.Bytes())); err != nil {
		t.Fatal(err)
	}
}

func TestPoster413HasUsefulContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "<html>nginx padding</html>", 413) }))
	defer server.Close()
	client, err := New(Config{Cookie: "auth=fake", IngestID: "fake", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.UploadPoster(context.Background(), "session", "poster.png", "image/png", strings.NewReader("small-poster"))
	var status *HTTPError
	if !errors.As(err, &status) || status.StatusCode != 413 || !strings.Contains(err.Error(), "upload poster (") || !strings.Contains(err.Error(), "/api/live/upload-poster-image") || strings.Contains(err.Error(), "<html>") {
		t.Fatalf("unexpected error: %v", err)
	}
}
