package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/types"
)

func TestProfileAttendeePreferences(t *testing.T) {
	app := wizardTestApp(t)
	person, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Preferences test", Email: fmt.Sprintf("prefs-%d@example.test", time.Now().UnixNano()), AttendeePreferences: &types.AttendeePreferences{TShirt: "MM", Diet: "vegan", Allergies: " Peanuts "}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB.Exec(context.Background(), `DELETE FROM people WHERE id=$1`, person) })
	request := func(method string, values url.Values, loggedIn bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for key, entries := range values {
			for _, value := range entries {
				if err := form.WriteField(key, value); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "/dashboard/profile", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if loggedIn {
				if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
					t.Fatal(e)
				}
			}
			DashboardEditSpeaker(w, r, app)
		})).ServeHTTP(w, req)
		return w
	}
	page := request("GET", nil, true)
	if page.Code != 200 || page.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(page.Body.String(), `value="vegan" selected`) || !strings.Contains(page.Body.String(), `type="hidden" name="FoodAllergies" value="Peanuts"`) {
		t.Fatal("private profile form", page.Code, page.Body.String())
	}
	values := url.Values{"Name": {"Preferences test"}, "AttendeePreferencesSet": {"1"}, "TShirt": {"LL"}, "DietaryPreference": {"other"}, "DietaryOther": {" Kosher "}, "FoodAllergies": {" Shellfish "}}
	if w := request("POST", values, false); w.Code != 303 {
		t.Fatal("anonymous profile save", w.Code)
	}
	got, _ := getters.GetPersonAttendeePreferences(app, person)
	if got.Diet != "vegan" {
		t.Fatal("anonymous request changed preferences", got)
	}
	if w := request("POST", values, true); w.Code != 303 || strings.Contains(w.Header().Get("Location"), "failed") {
		t.Fatal("profile save", w.Code, w.Header().Get("Location"))
	}
	got, err = getters.GetPersonAttendeePreferences(app, person)
	if err != nil || got != (types.AttendeePreferences{TShirt: "LL", Diet: "other", DietOther: "Kosher", Allergies: "Shellfish"}) {
		t.Fatal("profile round trip", got, err)
	}
	if err = getters.UpdateSpeaker(app, person, getters.SpeakerUpdate{Bio: "Unrelated edit", BioSet: true}); err != nil {
		t.Fatal(err)
	}
	preserved, _ := getters.GetPersonAttendeePreferences(app, person)
	if preserved != got {
		t.Fatal("sparse edit erased preferences", preserved)
	}
	values.Set("Name", "Must not save")
	values.Set("DietaryOther", "")
	request("POST", values, true)
	sp, _ := getters.FetchSpeakerByID(app, person)
	if sp.Name != "Preferences test" {
		t.Fatal("invalid preferences partially saved profile")
	}
	publicJSON, err := json.Marshal(sp)
	if err != nil || strings.Contains(string(publicJSON), "Kosher") || strings.Contains(string(publicJSON), "Shellfish") {
		t.Fatal("private diet leaked through public person type", string(publicJSON), err)
	}
	values.Set("Name", "Preferences test")
	values.Set("TShirt", "")
	values.Set("DietaryPreference", "")
	values.Set("FoodAllergies", "")
	request("POST", values, true)
	got, err = getters.GetPersonAttendeePreferences(app, person)
	if err != nil || got != (types.AttendeePreferences{}) {
		t.Fatal("explicit clearing failed", got, err)
	}
}

func TestAttendeePrefillPreservesSavedAndOtherOwners(t *testing.T) {
	for _, tc := range []struct {
		name, person string
		version      int64
		want         bool
	}{
		{"new own ticket", "buyer", 0, true}, {"saved own ticket", "buyer", 1, false}, {"other account", "other", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := &types.TicketAttendeeDetails{OwnerPersonID: "buyer", Version: tc.version, ProfileShirt: "MM", ProfileDiet: "none", Diet: "vegan"}
			guest := &types.TicketAttendeeDetails{OwnerPersonID: "buyer", ProfileShirt: "MM", ProfileDiet: "none"}
			prefillTicketAttendeeDefaults([]*types.TicketAttendeeDetails{row, guest}, tc.person)
			if row.IsBuyer != tc.want || guest.IsBuyer || guest.Diet != "" || guest.TShirt != "" {
				t.Fatal("wrong ticket prefilled", row, guest)
			}
			if !tc.want && row.Diet != "vegan" {
				t.Fatal("saved selection overwritten", row)
			}
			if tc.want && (row.Diet != "none" || row.TShirt != "MM") {
				t.Fatal("missing defaults", row)
			}
		})
	}
}
