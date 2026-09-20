package handlers

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/mux"
)

func TestRecordingSourceObjectKey(t *testing.T) {
	base := "https://btcpp.nyc3.digitaloceanspaces.com"
	for _, tc := range []struct{ in, want string }{
		{" nairobi/recordings/rita.mp4 ", "nairobi/recordings/rita.mp4"},
		{base + "/nairobi/Day%201/rita.mp4", "nairobi/Day 1/rita.mp4"},
		{base + "/nairobi/rita.mp4?download=1", "nairobi/rita.mp4"},
		{"https://btcpp.nyc3.cdn.digitaloceanspaces.com/nairobi/rita.mp4", "nairobi/rita.mp4"},
		{"nairobi/a+b%20.mp4", "nairobi/a+b%20.mp4"},
		{base + "/nairobi/a%2520b.mp4", "nairobi/a%20b.mp4"},
	} {
		got, err := parseAdminRecordingSource(tc.in, base)
		if err != nil || got != tc.want {
			t.Errorf("%q: %q %v", tc.in, got, err)
		}
	}
	for _, in := range []string{"", "/nairobi/rita.mp4", "//evil.test/file", "nairobi/../rita.mp4", "nairobi//rita.mp4", "nairobi/./rita.mp4", "nairobi\\rita.mp4", "nairobi/rita\n.mp4", "https://evil.test/file", "https://other.nyc3.digitaloceanspaces.com/file", "https://btcpp.nyc3.digitaloceanspaces.com.evil.test/file", "https://user:pass@btcpp.nyc3.digitaloceanspaces.com/file", base + "/nairobi/%2e%2e/rita.mp4", base + "/nairobi/%2Frita.mp4", base + "/nairobi/rita.mp4#fragment", "file:///tmp/rita.mp4", "https:/example.com/file", base + "/"} {
		if got, err := parseAdminRecordingSource(in, base); err == nil {
			t.Errorf("accepted invalid %q as %q", in, got)
		}
	}
	if _, err := parseAdminRecordingSource(base+"/rita.mp4", ""); err == nil {
		t.Fatal("URL accepted without configured bucket")
	}
	if got, err := parseAdminRecordingSource("nairobi/rita.mp4", ""); err != nil || got != "nairobi/rita.mp4" {
		t.Fatal(got, err)
	}
}

func TestAttachAdminTalkRecording(t *testing.T) {
	for _, tc := range []struct {
		name, tag, source                             string
		found, saveErr, wantLookup, wantSave, wantErr bool
	}{
		{"attach", "nairobi", "nairobi/rita.mp4", true, false, true, true, false},
		{"missing file", "nairobi", "nairobi/rita.mp4", false, false, true, false, true},
		{"database failure", "nairobi", "nairobi/rita.mp4", true, true, true, true, true},
		{"wrong event", "toronto", "nairobi/rita.mp4", true, false, false, false, true},
		{"invalid source", "nairobi", "https://evil.test/rita.mp4", true, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup, saved := false, false
			talk := &types.ConfTalk{ID: "rita-talk", Conf: &types.Conf{Tag: "nairobi"}}
			rec, err := attachAdminTalkRecording(talk, tc.tag, tc.source, "https://btcpp.nyc3.digitaloceanspaces.com", func(key string) bool {
				lookup = true
				if key != "nairobi/rita.mp4" {
					t.Fatal(key)
				}
				return tc.found
			}, func(id string, u getters.RecordingUpsert) (*types.Recording, error) {
				saved = true
				if id != "rita-talk" || u.FileURI == nil || *u.FileURI != "nairobi/rita.mp4" {
					t.Fatalf("wrong save: %s %+v", id, u)
				}
				if u.YTLink != nil || u.XLink != nil || u.XReplyLink != nil || u.PublishAt != nil || u.SetPublishAt || u.TalkName != nil {
					t.Fatal("changed publishing fields")
				}
				if tc.saveErr {
					return nil, errors.New("offline")
				}
				return &types.Recording{ID: "recording-id"}, nil
			})
			if (err != nil) != tc.wantErr || lookup != tc.wantLookup || saved != tc.wantSave {
				t.Fatalf("err=%v lookup=%v saved=%v", err, lookup, saved)
			}
			if !tc.wantErr && (rec == nil || rec.ID != "recording-id") {
				t.Fatal("missing recording result")
			}
		})
	}
}

func TestAdminAttachRecordingAuthorizationAndCSRF(t *testing.T) {
	for _, role := range []string{"", "nairobi-staff", "nairobi-volcoord", "toronto-admin"} {
		req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/nairobi/admin/proposal/rita/recording", nil), map[string]string{"conf": "nairobi", "proposalID": "rita"})
		w := httptest.NewRecorder()
		serveAdminAttachRecording(w, req, &config.AppContext{}, &auth.Identity{Roles: auth.ParseRoles([]string{role})})
		if w.Code != http.StatusForbidden {
			t.Fatalf("role %q received %d", role, w.Code)
		}
	}
	for _, role := range []string{"nairobi-admin", "global-admin"} {
		session := scs.New()
		req := httptest.NewRequest(http.MethodPost, "/nairobi/admin/proposal/rita/recording", strings.NewReader(url.Values{"csrf": {"bad"}, "RecordingSource": {"nairobi/rita.mp4"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx, err := session.Load(req.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		session.Put(ctx, authMethodsCSRFKey, "expected")
		req = mux.SetURLVars(req.WithContext(ctx), map[string]string{"conf": "nairobi", "proposalID": "rita"})
		w := httptest.NewRecorder()
		serveAdminAttachRecording(w, req, &config.AppContext{Session: session}, &auth.Identity{Roles: auth.ParseRoles([]string{role})})
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Invalid form token") {
			t.Fatalf("bad token reached data access: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAttachRecordingTemplate(t *testing.T) {
	raw, err := os.ReadFile("../../templates/admin/edit_proposal.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tm, err := template.New("edit").Funcs(template.FuncMap{"safeURL": func(s string) string { return s }, "speakerPhoto": func(s string) string { return s }, "dict": func(...any) map[string]any { return nil }}).Parse(string(raw) + `{{define "header"}}{{end}}{{define "mainnav"}}{{end}}{{define "admin_favicon"}}{{end}}{{define "admin_speakerconf_details"}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	page := &AdminEditProposalPage{Conf: &types.Conf{Tag: "nairobi"}, Proposal: &types.Proposal{ID: "rita", ConfTalk: &types.ConfTalk{}}, ResourcesCSRF: "test-token"}
	var b bytes.Buffer
	for _, existing := range []bool{false, true} {
		if existing {
			page.Proposal.Recording = &types.Recording{ID: "rec", FileURI: "nairobi/rita.mp4"}
		}
		b.Reset()
		if err := tm.Execute(&b, page); err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{`action="/nairobi/admin/proposal/rita/recording"`, `name="RecordingSource"`, `name="csrf" value="test-token"`, "Attach recording"} {
			if !strings.Contains(b.String(), s) {
				t.Fatalf("missing %q", s)
			}
		}
		if existing {
			for _, s := range []string{`href="/nairobi/admin/recordings/rec"`, `value="nairobi/rita.mp4"`, "Save recording source"} {
				if !strings.Contains(b.String(), s) {
					t.Fatalf("missing existing recording %q", s)
				}
			}
		}
	}
	page.Proposal.ConfTalk = nil
	b.Reset()
	if err := tm.Execute(&b, page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `name="RecordingSource"`) {
		t.Fatal("recording form shown for unaccepted proposal")
	}
}
