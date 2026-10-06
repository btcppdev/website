package handlers

import (
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestLocalizedConferenceRendering(t *testing.T) {
	t.Chdir("../..")
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul", Desc: "bitcoin++ Seoul, privacy edition", Location: "Seoul, South Korea", Tagline: "goes dark", StartDate: time.Date(2026, 11, 5, 10, 0, 0, 0, loc), DateDesc: "Nov 5 - 6, 2026", Venue: "Community House -- Masil", ShowHackathon: true, TZ: loc}
	page := &ConfPage{Conf: conf, Tix: &types.ConfTicket{ID: "seoul-pass", Currency: "USD", Symbol: "$", BasePrice: 389, Local: 121}, TixLeft: 30, Year: 2026}
	for _, language := range []string{"ko", "en", "ko"} {
		req := mux.SetURLVars(httptest.NewRequest("GET", "/"+language+"/seoul", nil), map[string]string{"locale": language})
		w := httptest.NewRecorder()
		if err := renderLocalizedConference(w, req, ctx, "conf/generic.tmpl", page); err != nil {
			t.Fatal(err)
		}
		body := w.Body.String()
		for _, expected := range []string{`lang="` + language + `"`, `hreflang="ko"`, `/tix/seoul-pass/checkout`, `/seoul/speakers`} {
			if !strings.Contains(body, expected) {
				t.Errorf("%s page missing %s", language, expected)
			}
		}
		if w.Header().Get("Content-Language") != language {
			t.Error("wrong language header")
		}
		if language == "ko" {
			for _, expected := range []string{"대한민국 서울", `href="/ko/seoul#about"`, `https://btcpp.dev/ko/seoul`} {
				if !strings.Contains(body, expected) {
					t.Errorf("Korean page missing %s", expected)
				}
			}
		} else if !strings.Contains(body, "Seoul, South Korea") {
			t.Error("English cache contaminated")
		}
		if os.Getenv("BTCPP_TRANSLATION_PREVIEW") == "1" {
			if err := os.WriteFile("/tmp/btcpp-i18n-"+language+".html", w.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Exercise both caches simultaneously; language selection must be request-safe.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			language := "en"
			if i%2 == 0 {
				language = "ko"
			}
			req := mux.SetURLVars(httptest.NewRequest("GET", "/seoul", nil), map[string]string{"locale": language})
			w := httptest.NewRecorder()
			if err := renderLocalizedConference(w, req, ctx, "conf/generic.tmpl", page); err != nil {
				t.Error(err)
				return
			}
			if !strings.Contains(w.Body.String(), `<html lang="`+language+`">`) {
				t.Error("concurrent render leaked locale")
			}
		}(i)
	}
	wg.Wait()
	if conf.Location != "Seoul, South Korea" || conf.Desc != "bitcoin++ Seoul, privacy edition" {
		t.Fatal("render mutated original data")
	}
	req := mux.SetURLVars(httptest.NewRequest("GET", "/fr/seoul", nil), map[string]string{"locale": "fr"})
	w := httptest.NewRecorder()
	if err := renderLocalizedConference(w, req, ctx, "conf/generic.tmpl", page); err != nil {
		t.Fatal(err)
	}
	if w.Code != 404 {
		t.Fatalf("unsupported locale status %d", w.Code)
	}
}
