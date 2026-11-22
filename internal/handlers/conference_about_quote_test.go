package handlers

import (
	"bytes"
	"html"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestConferenceAboutQuoteRendering(t *testing.T) {
	t.Chdir("../..")
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	custom := `Cryptography <script>alert("test")</script> & open source.`
	blank := ""
	for _, language := range []string{"en", "ko"} {
		fallback, err := conferenceCatalog.Message(language, "about.quote")
		if err != nil {
			t.Fatal(err)
		}
		for _, quote := range []*string{nil, &custom, &blank} {
			conf := &types.Conf{Tag: "seoul", Desc: "Seoul", AboutQuote: quote}
			page := &ConfPage{Conf: conf}
			r := mux.SetURLVars(httptest.NewRequest("GET", "/"+language+"/seoul", nil), map[string]string{"locale": language})
			w := httptest.NewRecorder()
			if err := renderLocalizedConference(w, r, app, "conf/generic.tmpl", page); err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			want := fallback
			if quote != nil {
				want = *quote
			}
			if want == "" {
				if strings.Contains(body, `class="rebrand-pullquote"`) {
					t.Fatal("cleared copy still rendered an empty pullquote")
				}
			} else if !strings.Contains(body, `<p class="rebrand-pullquote">`+html.EscapeString(want)+`</p>`) {
				t.Fatalf("%s did not render the expected escaped closing line", language)
			}
			if quote != nil && strings.Contains(body, `<p class="rebrand-pullquote">`+html.EscapeString(fallback)+`</p>`) {
				t.Fatal("default remained after overriding the line")
			}
			if conf.AboutQuote != quote || (quote != nil && *quote != want) {
				t.Fatal("localized rendering mutated the shared event")
			}
		}
	}
	for _, quote := range []*string{nil, &custom, &blank} {
		conf := &types.Conf{Tag: "seoul", AboutQuote: quote}
		var output bytes.Buffer
		if err := app.TemplateCache.ExecuteTemplate(&output, "admin/event_details.tmpl", &EventDetailsPage{Conf: conf, MerchUpsellSlots: make([]*types.MerchProduct, 3)}); err != nil {
			t.Fatal(err)
		}
		body := output.String()
		start := strings.Index(body, `<textarea name="about_quote"`)
		if start < 0 {
			t.Fatal("missing editable closing line")
		}
		field := body[start:]
		field = field[strings.Index(field, ">")+1 : strings.Index(field, "</textarea>")]
		fallback, _ := conferenceCatalog.Message("en", "about.quote")
		if html.UnescapeString(field) != conf.AboutQuoteText(fallback) {
			t.Fatal("admin form did not show the current closing line")
		}
	}
}
