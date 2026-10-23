package emaildirectory

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperatorsRestrictedToAlertsAndCurrentGlobalAdmins(t *testing.T) {
	s := serviceStore(t)
	ctx := context.Background()
	if _, err := s.DB.Exec(ctx, `CREATE TABLE people_roles(person_id uuid,scope text,position text)`); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, name := range []string{"admin", "no-email", "regular", "alerts-admin", "deleted"} {
		var id string
		if err := s.DB.QueryRow(ctx, `INSERT INTO people(name,is_deleted_account) VALUES($1,$2) RETURNING id::text`, name, name == "deleted").Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
		scope := "global"
		if name == "regular" {
			scope = "conference"
		}
		if name == "alerts-admin" {
			scope = "alerts"
		}
		if _, err := s.DB.Exec(ctx, `INSERT INTO people_roles VALUES($1,$2,'admin')`, id, scope); err != nil {
			t.Fatal(err)
		}
	}
	for _, address := range []string{"primary@example.test", "secondary@example.test", "unverified@example.test"} {
		if _, err := s.Resolve(ctx, address, "alerts"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,$3,CASE WHEN $4 THEN now() ELSE NULL END)`, ids["admin"], address, address == "primary@example.test", address != "unverified@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{Store: s, Tokens: map[string]string{"alerts": strings.Repeat("a", 32), "insider": strings.Repeat("i", 32)}}
	request := func(service, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/internal/email-directory/"+service+"/operators", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.Routes().ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		service, token string
		status         int
	}{{"alerts", "", 401}, {"alerts", h.Tokens["insider"], 401}, {"insider", h.Tokens["insider"], 403}} {
		w := request(tc.service, tc.token)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "@example.test") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request("alerts", h.Tokens["alerts"])
	var out struct {
		Recipients []struct {
			AccountID string `json:"account_id"`
			Email     string `json:"email"`
		} `json:"recipients"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Recipients) != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, v := range out.Recipients {
		if v.AccountID == ids["admin"] && v.Email != "primary@example.test" {
			t.Fatal(v)
		}
		if v.AccountID != ids["admin"] && v.AccountID != ids["no-email"] {
			t.Fatal(v)
		}
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("cacheable private directory")
	}
	if _, err := s.DB.Exec(ctx, `DELETE FROM people_roles WHERE person_id=$1`, ids["admin"]); err != nil {
		t.Fatal(err)
	}
	if w = request("alerts", h.Tokens["alerts"]); strings.Contains(w.Body.String(), "primary@example.test") {
		t.Fatal("revoked role still returned")
	}
}
