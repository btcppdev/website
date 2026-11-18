package handlers

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestSpeakerCardFormats(t *testing.T) {
	app := antarcticaTestTemplates(t)
	for _, format := range types.MediaCards {
		t.Run(format, func(t *testing.T) {
			r := mux.SetURLVars(httptest.NewRequest("GET", "/", nil), map[string]string{"conf": "berlin26", "card": format})
			w := httptest.NewRecorder()
			PreviewSpeakerCard(w, r, app)
			if w.Code != 200 {
				t.Fatalf("preview returned %d: %s", w.Code, w.Body.String())
			}
			for _, required := range []string{"Speaker&#39;s Name", "niftynei.jpg", "Acme Bitcoin Co.", "@niftynei"} {
				if !strings.Contains(w.Body.String(), required) {
					t.Errorf("preview missing %q", required)
				}
			}
			var out bytes.Buffer
			if err := app.TemplateCache.ExecuteTemplate(&out, "media/speaker_"+format+".tmpl", &SpeakerCard{ConfTag: "berlin26", Name: "Alice & Bob"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Alice &amp; Bob") {
				t.Error("speaker name missing or not escaped")
			}
			for _, omitted := range []string{`<img class="speaker-photo"`, `<div class="speaker-details">`} {
				if strings.Contains(out.String(), omitted) {
					t.Errorf("missing optional data still rendered: %s", omitted)
				}
			}
		})
	}
}
