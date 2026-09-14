package handlers

import (
	"btcpp-web/internal/types"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAdminRoleOptions(t *testing.T) {
	options := adminRoleOptions([]*types.Conf{{Tag: "berlin26"}})
	seen := map[string]bool{}
	for _, option := range options {
		if seen[option.Value] || option.Label == "" || option.Description == "" {
			t.Fatal("incomplete or duplicate role option", option)
		}
		seen[option.Value] = true
		if option.Value == "accts-admin" && !option.Restricted {
			t.Fatal("accounts grant restriction missing")
		}
	}
	for _, tag := range []string{"merch-admin", "global-admin", "accts-admin", "berlin26-admin", "berlin26-staff", "berlin26-volcoord", "berlin26-hackathon"} {
		if !seen[tag] {
			t.Fatal("missing option", tag)
		}
	}
}

func TestRoleManagerPreview(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/admin/role_manager.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	page := &GlobalAdminDashboardPage{RoleOptions: adminRoleOptions([]*types.Conf{{Tag: "berlin26"}, {Tag: "vienna"}}), RoleCSRF: "preview-only"}
	handler := http.NewServeMux()
	handler.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../../static"))))
	handler.HandleFunc("/preview/roles", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/static/css/admin-roles.css"><script src="/static/js/admin-roles.js" defer></script><style>body{margin:0;background:#f6f3ee;color:#171717;font-family:Arial,sans-serif}main{max-width:900px;margin:24px auto;padding:16px}*{box-sizing:border-box}</style></head><body><main><p>bitcoin++ / Admin preview · synthetic users, changes are not saved</p>`))
		if err := tmpl.ExecuteTemplate(w, "admin_role_manager", page); err != nil {
			t.Error(err)
		}
		w.Write([]byte(`</main></body></html>`))
	})
	handler.HandleFunc("/api/speakers/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"mara","name":"Mara Example","email":"mara@example.test"},{"id":"empty","name":"New Person","email":"new@example.test"},{"id":"error","name":"Unavailable Person","email":"unavailable@example.test"}]`))
	})
	handler.HandleFunc("/api/speakers/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "error") {
			http.Error(w, "Temporary failure", 503)
			return
		}
		if strings.Contains(r.URL.Path, "empty") {
			w.Write([]byte(`{"roles":null}`))
			return
		}
		w.Write([]byte(`{"roles":["berlin26-staff","merch-admin","legacy-custom"]}`))
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/preview/roles", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "Merch administrator") || !strings.Contains(recorder.Body.String(), `data-description="Access accounts.btcpp.dev. Only nifty@btcpp.dev can grant this role." disabled`) {
		t.Fatal("role picker rendering or restricted role failed", recorder.Body.String())
	}
	if os.Getenv("ROLE_MANAGER_PREVIEW") == "1" {
		t.Log("Role manager preview: http://127.0.0.1:8101/preview/roles")
		t.Fatal(http.ListenAndServe("127.0.0.1:8101", handler))
	}
}
