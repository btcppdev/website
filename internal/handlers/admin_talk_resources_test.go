package handlers

import (
	"bytes"
	"errors"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestAdminTalkResourcesConferenceScope(t *testing.T) {
	for _, tc := range []struct {
		talk *types.ConfTalk
		tag  string
		want bool
	}{
		{nil, "nairobi", false}, {&types.ConfTalk{}, "nairobi", false},
		{&types.ConfTalk{Conf: &types.Conf{Tag: "nairobi"}}, "toronto", false},
		{&types.ConfTalk{Conf: &types.Conf{Tag: "nairobi"}}, "nairobi", true},
	} {
		if got := adminTalkResourcesInConference(tc.talk, tc.tag); got != tc.want {
			t.Fatalf("scope=%v, want %v", got, tc.want)
		}
	}
}

func TestAdminTalkResourcesSave(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		fields                            map[string]string
		file                              string
		uploadErr, saveErr                bool
		wantErr                           bool
		wantGithub, wantSlides, wantKey   string
		wantUpload, wantSave, wantDiscard bool
	}{
		{name: "links", fields: map[string]string{"GithubRepoURL": " https://github.com/example/node ", "SlidesURL": "https://example.com/deck.pdf"}, wantGithub: "https://github.com/example/node", wantSlides: "https://example.com/deck.pdf", wantSave: true},
		{name: "preserve uploaded slides", fields: map[string]string{"GithubRepoURL": "https://github.com/example/new", "SlidesURL": "https://cdn.example/old.pdf"}, wantGithub: "https://github.com/example/new", wantSlides: "https://cdn.example/old.pdf", wantKey: "old-key", wantSave: true},
		{name: "clear github", fields: map[string]string{"GithubRepoURL": ""}, wantSlides: "https://cdn.example/old.pdf", wantKey: "old-key", wantSave: true},
		{name: "remove slides", fields: map[string]string{"RemoveSlides": "1"}, wantGithub: "https://github.com/example/old", wantSave: true},
		{name: "replace upload", fields: map[string]string{"SlidesURL": "https://cdn.example/old.pdf"}, file: "new.pdf", wantGithub: "https://github.com/example/old", wantSlides: "https://cdn.example/new.pdf", wantKey: "uploaded", wantUpload: true, wantSave: true},
		{name: "invalid github", fields: map[string]string{"GithubRepoURL": "https://github.com.evil.test/repo"}, file: "new.pdf", wantErr: true},
		{name: "invalid slides", fields: map[string]string{"SlidesURL": "javascript:alert(1)"}, wantErr: true},
		{name: "relative slides", fields: map[string]string{"SlidesURL": "https:/file.pdf"}, wantErr: true},
		{name: "unsupported file", file: "new.exe", wantErr: true},
		{name: "remove and upload", fields: map[string]string{"RemoveSlides": "1"}, file: "new.pdf", wantErr: true},
		{name: "new link and upload", fields: map[string]string{"SlidesURL": "https://other.example/deck.pdf"}, file: "new.pdf", wantErr: true},
		{name: "upload failure", file: "new.pdf", uploadErr: true, wantErr: true, wantUpload: true},
		{name: "database failure cleans new upload", file: "new.pdf", saveErr: true, wantErr: true, wantUpload: true, wantSave: true, wantDiscard: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			for k, v := range tc.fields {
				if err := mw.WriteField(k, v); err != nil {
					t.Fatal(err)
				}
			}
			if tc.file != "" {
				f, err := mw.CreateFormFile("SlidesFile", tc.file)
				if err != nil {
					t.Fatal(err)
				}
				f.Write([]byte("%PDF-1.7\nexample"))
			}
			mw.Close()
			r := httptest.NewRequest(http.MethodPost, "/nairobi/admin/proposal/p/resources", &body)
			r.Header.Set("Content-Type", mw.FormDataContentType())
			if err := r.ParseMultipartForm(maxPresentationBytes); err != nil {
				t.Fatal(err)
			}
			defer r.MultipartForm.RemoveAll()
			talk := &types.ConfTalk{Conf: &types.Conf{Tag: "nairobi"}, GithubRepoURL: "https://github.com/example/old", SlidesURL: "https://cdn.example/old.pdf", SlidesObjectKey: "old-key"}
			uploaded, saved, discarded := false, false, false
			newKey := ""
			deps := adminTalkResourceStore{
				upload: func(key string, raw []byte, ct string) (string, error) {
					uploaded = true
					newKey = key
					if !strings.HasPrefix(key, "nairobi/presentations/") || !strings.HasSuffix(key, ".pdf") || ct != "application/pdf" {
						t.Fatalf("bad upload %q %q", key, ct)
					}
					if tc.uploadErr {
						return "", errors.New("offline")
					}
					return "https://cdn.example/new.pdf", nil
				},
				save: func(github, slides, key string) error {
					saved = true
					if tc.saveErr {
						return errors.New("database offline")
					}
					wantKey := tc.wantKey
					if wantKey == "uploaded" {
						wantKey = newKey
					}
					if github != tc.wantGithub || slides != tc.wantSlides || key != wantKey {
						t.Fatalf("saved %q %q %q", github, slides, key)
					}
					return nil
				},
				discard: func(key string) {
					discarded = true
					if key == "old-key" || key != newKey {
						t.Fatalf("discarded wrong object %q", key)
					}
				},
			}
			err := updateAdminTalkResources(r, talk, deps)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if uploaded != tc.wantUpload || saved != tc.wantSave || discarded != tc.wantDiscard {
				t.Fatalf("upload/save/discard = %v/%v/%v", uploaded, saved, discarded)
			}
		})
	}
}

func TestAdminTalkResourcesTemplate(t *testing.T) {
	raw, err := os.ReadFile("../../templates/admin/edit_proposal.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("edit").Funcs(template.FuncMap{"safeURL": func(s string) string { return s }, "speakerPhoto": func(s string) string { return s }, "dict": func(...any) map[string]any { return nil }}).Parse(string(raw) + `{{define "header"}}{{end}}{{define "mainnav"}}{{end}}{{define "admin_favicon"}}{{end}}{{define "admin_speakerconf_details"}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	page := &AdminEditProposalPage{Conf: &types.Conf{Tag: "nairobi"}, Proposal: &types.Proposal{ID: "rita", ConfTalk: &types.ConfTalk{SlidesURL: "https://example.com/slides.pdf", GithubRepoURL: "https://github.com/example/node"}}, ResourcesCSRF: "token"}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, page); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`action="/nairobi/admin/proposal/rita/resources"`, `enctype="multipart/form-data"`, `name="csrf" value="token"`, `name="SlidesFile"`, `name="GithubRepoURL"`, `name="RemoveSlides"`, "Save resources"} {
		if !strings.Contains(b.String(), s) {
			t.Fatalf("missing %s", s)
		}
	}
	page.Proposal.ConfTalk = nil
	b.Reset()
	if err := tmpl.Execute(&b, page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `name="SlidesFile"`) {
		t.Fatal("upload shown for unaccepted proposal")
	}
}
