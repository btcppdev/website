package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/eventplan"
	"github.com/gorilla/mux"
)

func TestEventDetailsInlineSaves(t *testing.T) {
	app := wizardTestApp(t)
	c := context.Background()
	tag := fmt.Sprintf("details-%d", time.Now().UnixNano())
	confID, err := getters.CreatePlannedEvent(app, eventplan.Basics{Tag: tag, Name: "Details test", Edition: "global", Timezone: "UTC"}, "")
	if err != nil {
		t.Fatal(err)
	}
	person, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Details admin", Email: tag + "@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM conferences WHERE id=$1`, confID)
		app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, person)
	})
	if _, err = app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(c, `UPDATE conferences SET about_body='Saved introduction', venue_body='Saved venue' WHERE id=$1`, confID); err != nil {
		t.Fatal(err)
	}
	post := func(suffix string, form url.Values, jsonResponse, login bool) *httptest.ResponseRecorder {
		t.Helper()
		r := mux.SetURLVars(httptest.NewRequest("POST", "/"+tag+"/admin/details"+suffix, strings.NewReader(form.Encode())), map[string]string{"conf": tag})
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if jsonResponse {
			r.Header.Set("Accept", "application/json")
		}
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if login {
				if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
					t.Fatal(e)
				}
			}
			if suffix == "/confinfo" {
				GlobalAdminUpdateConfInfo(w, r, app)
			} else {
				GlobalAdminUpdateConfDetails(w, r, app)
			}
		})).ServeHTTP(w, r)
		return w
	}
	checkJSON := func(w *httptest.ResponseRecorder, status int, ok bool) {
		t.Helper()
		var result struct {
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		}
		if w.Code != status || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.OK != ok || result.Message == "" || w.Header().Get("Location") != "" {
			t.Fatalf("unexpected inline response: %d %s", w.Code, w.Body.String())
		}
	}
	day := url.Values{"day": {"1"}, "doors_start": {"09:00"}, "doors_end": {"18:00"}, "venues": {"Main stage"}}
	if w := post("/confinfo", day, true, false); w.Code != http.StatusSeeOther {
		t.Fatalf("anonymous save: %d", w.Code)
	}
	checkJSON(post("/confinfo", day, true, true), http.StatusOK, true)
	infos, err := getters.ListConfInfos(app, tag)
	if err != nil || len(infos) != 1 {
		t.Fatalf("days: %v %v", infos, err)
	}
	day.Set("id", infos[0].ID)
	day.Set("doors_start", "10:00")
	checkJSON(post("/confinfo", day, true, true), http.StatusOK, true)
	day.Set("doors_start", "invalid")
	checkJSON(post("/confinfo", day, true, true), http.StatusBadRequest, false)
	infos, err = getters.ListConfInfos(app, tag)
	if err != nil || len(infos) != 1 || timeInputStart(infos[0].Doors) != "10:00" {
		t.Fatalf("invalid save changed the day: %v %v", infos, err)
	}
	conf, err := getters.GetConfByRef(app, confID)
	if err != nil {
		t.Fatal(err)
	}
	if conf.AboutBody != "Saved introduction" || conf.VenueBody != "Saved venue" {
		t.Fatal("day save erased conference details")
	}
	checkJSON(post("", url.Values{"timezone": {"Not/AZone"}}, true, true), http.StatusBadRequest, false)
	details := url.Values{"description": {"Updated details"}, "timezone": {"UTC"}, "about_body": {"Updated introduction"}, "venue_body": {"Updated venue"}}
	checkJSON(post("", details, true, true), http.StatusOK, true)
	conf, err = getters.GetConfByRef(app, confID)
	if err != nil || conf.AboutBody != "Updated introduction" || conf.VenueBody != "Updated venue" {
		t.Fatalf("details save failed: %v", err)
	}
	defaultQuote, err := conferenceCatalog.Message("en", "about.quote")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, value       string
		omit, wantDefault bool
	}{
		{name: "untouched default", value: defaultQuote, wantDefault: true},
		{name: "custom copy", value: "A room for cryptography builders."},
		{name: "older form preserves custom copy", value: "A room for cryptography builders.", omit: true},
		{name: "clear hides the line", value: ""},
		{name: "older form preserves hidden line", value: "", omit: true},
	} {
		if test.omit {
			details.Del("about_quote")
		} else {
			details.Set("about_quote", test.value)
		}
		checkJSON(post("", details, true, true), http.StatusOK, true)
		conf, err = getters.GetConfByRef(app, confID)
		if err != nil {
			t.Fatal(err)
		}
		if test.wantDefault {
			if conf.AboutQuote != nil {
				t.Fatalf("%s: default was replaced", test.name)
			}
		} else if conf.AboutQuote == nil || *conf.AboutQuote != test.value {
			t.Fatalf("%s: got %v", test.name, conf.AboutQuote)
		}
	}
	day.Set("doors_start", "11:00")
	if w := post("/confinfo", day, false, true); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/"+tag+"/admin/details?") {
		t.Fatalf("HTML fallback: %d %s", w.Code, w.Body.String())
	}
}
