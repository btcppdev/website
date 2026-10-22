package missives

import (
	"btcpp-web/internal/mtypes"
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRetiredNewsletterCannotScheduleEvenPreview(t *testing.T) {
	for _, preview := range []bool{false, true} {
		for _, names := range [][]string{{"insider"}, {"newsletter", "!insider"}} {
			_, status, err := scheduleMissive(nil, nil, &mtypes.Letter{Newsletters: names}, preview)
			if err == nil || status != mtypes.ErrO {
				t.Fatalf("retired campaign accepted: preview=%v names=%v status=%v err=%v", preview, names, status, err)
			}
		}
	}
}

func TestRetiredNewsletterSignupGone(t *testing.T) {
	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/insider/subscribe", nil), map[string]string{"newsletter": "insider"})
	rec := httptest.NewRecorder()
	SubscribeEmail(rec, req, nil)
	if rec.Code != http.StatusGone {
		t.Fatalf("status=%d", rec.Code)
	}
}
