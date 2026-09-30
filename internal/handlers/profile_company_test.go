package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestProfileCompanyEditor(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{false, true} {
		for _, mode := range []string{"create", "edit"} {
			page := &EditSpeakerPage{Mode: mode, IsAdmin: admin, Speaker: &types.Speaker{Name: "Clara", Company: "alloc init", AvailToHire: true, LookingToHire: true}}
			var out bytes.Buffer
			if err := app.TemplateCache.ExecuteTemplate(&out, "dashboard_edit_speaker.tmpl", page); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{`name="Name" type="text" required`, `name="OrgLogoFile"`, `name="AvailToHire" value="on" checked`, `name="LookingToHire" value="on" checked`} {
				if !strings.Contains(out.String(), field) {
					t.Fatalf("missing %s for admin=%t mode=%s", field, admin, mode)
				}
			}
			if !strings.Contains(out.String(), `name="Company" type="text" value="alloc init"`) {
				t.Fatalf("company input missing for admin=%t mode=%s", admin, mode)
			}
		}
	}
}
