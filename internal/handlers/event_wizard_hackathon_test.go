package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/eventplan"
	"github.com/gorilla/mux"
)

func TestEventWizardHackathonLifecycle(t *testing.T) {
	app := wizardTestApp(t)
	c := context.Background()
	tag := fmt.Sprintf("hack-plan-%d", time.Now().UnixNano())
	confID, err := getters.CreatePlannedEvent(app, eventplan.Basics{Tag: tag, Name: "Planning test", Edition: "global", Timezone: "Asia/Tokyo"}, "")
	if err != nil {
		t.Fatal(err)
	}
	person, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Event admin", Email: tag + "@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM conferences WHERE id=$1`, confID)
		app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, person)
	})
	if _, err = app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,'admin')`, person, tag); err != nil {
		t.Fatal(err)
	}
	conf, err := getters.GetConfByRef(app, confID)
	if err != nil {
		t.Fatal(err)
	}
	root := mux.NewRouter()
	registerEventWizard(root, app)
	registerConferenceHackathonAdminRoutes(root, app)
	path := "/" + tag + "/admin/setup"
	request := func(target, method string, payload any, csrf string, login bool) *httptest.ResponseRecorder {
		t.Helper()
		raw, e := json.Marshal(payload)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(method, target, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if login {
				if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
					t.Fatal(e)
				}
			}
			app.Session.Put(r.Context(), authMethodsCSRFKey, "test-token")
			root.ServeHTTP(w, r)
		})).ServeHTTP(w, req)
		return w
	}
	load := func() eventplan.Hackathon {
		t.Helper()
		data, e := eventWizardData(app, conf, true)
		if e != nil {
			t.Fatal(e)
		}
		return data["hackathon"].(eventplan.Hackathon)
	}
	h := load()
	if h.Included || h.ID != "" || h.Visibility != "hidden" || h.DescriptionFormat != "markdown" {
		t.Fatal("unexpected defaults", h)
	}
	if w := request(path+"?step=hackathon", "GET", nil, "", false); w.Code != 303 {
		t.Fatal("anonymous wizard access", w.Code)
	}
	if w := request(path+"?step=hackathon", "GET", nil, "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"hackathon":`) {
		t.Fatal("wizard data", w.Code, w.Body.String())
	}
	rev := int64(0)
	save := func(h eventplan.Hackathon, want int) {
		t.Helper()
		w := request(path, "POST", map[string]any{"section": "hackathon", "revision": rev, "data": h}, "test-token", true)
		if w.Code != want {
			t.Fatalf("save got %d want %d: %s", w.Code, want, w.Body.String())
		}
		if want == 200 {
			rev++
		}
		plan, e := getters.LoadEventPlanning(app, confID)
		if e != nil || plan.Revision != rev {
			t.Fatal("revision not atomic", plan.Revision, rev, e)
		}
	}
	save(h, 200)
	if got := load(); got.Included {
		t.Fatal("optional step created a competition")
	}
	h.Included = true
	h.Title = "Build in Tokyo"
	h.Description = "Build useful Bitcoin tools."
	size := 4
	h.MaxTeamSize = &size
	h.PublicGalleryEnabled = true
	body := map[string]any{"section": "hackathon", "revision": rev, "data": h}
	if w := request(path, "POST", body, "wrong", true); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	bad := h
	bad.Title = " "
	save(bad, 400)
	zero := 0
	bad = h
	bad.MaxTeamSize = &zero
	save(bad, 400)
	bad = h
	bad.Visibility = "invalid"
	save(bad, 400)
	bad = h
	bad.DescriptionFormat = "invalid"
	save(bad, 400)
	save(h, 200)
	created := load()
	if !created.Included || created.ID == "" || created.Visibility != "hidden" || created.Title != h.Title || *created.MaxTeamSize != 4 || !created.PublicGalleryEnabled {
		t.Fatal("creation did not persist", created)
	}
	var count int
	app.DB.QueryRow(c, `SELECT count(*) FROM competitions WHERE conference_id=$1`, confID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate competition", count)
	}
	freshConf, _ := getters.GetConfByRef(app, confID)
	if freshConf.IsPublished() || freshConf.Active {
		t.Fatal("hackathon setup published the event")
	}
	// Existing hackathon administration must be available to this event's admin.
	for _, suffix := range []string{"?setup=2", "/awards", "/judging", "/managers", "/projects"} {
		w := request("/"+tag+"/admin/hackathon"+suffix, "GET", nil, "", true)
		if w.Code != 200 {
			t.Fatal("management link unavailable", suffix, w.Code, w.Body.String())
		}
	}
	// A tab opened before creation must not create a duplicate or overwrite it.
	save(h, 409)
	if w := request(path, "POST", body, "test-token", true); w.Code != 409 {
		t.Fatal("stale plan revision", w.Code)
	}
	// Changes from the separate hackathon admin screen invalidate the old snapshot.
	if _, err = app.DB.Exec(c, `UPDATE competitions SET lifecycle_override='closed',judging_mode='manual',hacking_starts_at=now(),title='Edited elsewhere' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	save(created, 409)
	existing := load()
	oldCompetition, _ := getters.GetCompetitionByID(app, existing.ID)
	projectID, err := getters.CreateProject(app, getters.ProjectInput{CompetitionID: existing.ID, CreatedByPersonID: person, Slug: "existing-project", Title: "Existing project"})
	if err != nil {
		t.Fatal(err)
	}
	awardID, err := getters.CreateAward(app, getters.AwardInput{CompetitionID: existing.ID, Title: "Best project", AwardType: "normal", Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	existing.Title = "Updated in event setup"
	existing.MaxTeamSize = nil
	existing.PublicTablesEnabled = true
	save(existing, 200)
	got, _ := getters.GetCompetitionByID(app, existing.ID)
	if got.ID != created.ID || got.Title != existing.Title || got.MaxTeamSize != nil || !got.PublicTablesEnabled || got.LifecycleOverride != "closed" || got.JudgingMode != "manual" || !got.HackingStartsAt.Equal(*oldCompetition.HackingStartsAt) {
		t.Fatal("edit lost existing competition settings", got)
	}
	var preserved bool
	if err = app.DB.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1) AND EXISTS(SELECT 1 FROM awards WHERE id=$2)`, projectID, awardID).Scan(&preserved); err != nil || !preserved {
		t.Fatal("edit removed related records", err)
	}
	bad = load()
	bad.Included = false
	save(bad, 400)
	bad = load()
	bad.ID = "00000000-0000-0000-0000-000000000000"
	save(bad, 409)
	// Scope and read-only checks apply to the new step just like other steps.
	otherID, e := getters.CreatePlannedEvent(app, eventplan.Basics{Tag: tag + "-b", Name: "Other event", Edition: "global", Timezone: "UTC"}, "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { app.DB.Exec(c, `DELETE FROM conferences WHERE id=$1`, otherID) })
	if w := request("/"+tag+"-b/admin/setup", "POST", body, "test-token", true); w.Code != 403 {
		t.Fatal("cross-event write", w.Code)
	}
	if _, err = app.DB.Exec(c, `UPDATE people_roles SET position='staff' WHERE person_id=$1`, person); err != nil {
		t.Fatal(err)
	}
	if w := request(path, "POST", body, "test-token", true); w.Code != 403 {
		t.Fatal("staff wrote hackathon settings", w.Code)
	}
	if w := request(path+"?step=hackathon", "GET", nil, "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"can_edit":false`) {
		t.Fatal("staff read-only view", w.Code, w.Body.String())
	}
}
