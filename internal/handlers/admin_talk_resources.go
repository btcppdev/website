package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

func adminTalkResourcesInConference(talk *types.ConfTalk, tag string) bool {
	return talk != nil && talk.Conf != nil && tag != "" && talk.Conf.Tag == tag
}

func AdminTalkResources(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireConfAdmin(w, r, ctx) == nil {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tag, proposalID := mux.Vars(r)["conf"], mux.Vars(r)["proposalID"]
	talk, err := getters.GetConfTalkByProposal(ctx, proposalID)
	if err != nil {
		http.Error(w, "Unable to load talk resources", 500)
		return
	}
	if !adminTalkResourcesInConference(talk, tag) {
		http.Error(w, "Accepted talk not found in this event", 404)
		return
	}
	limitRequestBody(w, r, maxPresentationBodyBytes)
	if err := r.ParseMultipartForm(maxPresentationBytes); err != nil {
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		http.Error(w, "Unable to read upload. Slides must be no larger than 40 MiB.", 400)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to secure resources form", 500)
		return
	}
	if !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
		http.Error(w, "Invalid form token", 403)
		return
	}
	editURL := fmt.Sprintf("/%s/admin/proposal/%s/edit", url.PathEscape(tag), url.PathEscape(proposalID))
	redirect := func(key, message string) {
		q := url.Values{key: {message}}
		if back := r.PostForm.Get("ReturnURL"); safeAdminReturn(back, tag) {
			q.Set("return", back)
		}
		http.Redirect(w, r, editURL+"?"+q.Encode()+"#resources", http.StatusSeeOther)
	}
	deps := adminTalkResourceStore{
		upload: func(key string, raw []byte, contentType string) (string, error) {
			if !spaces.IsConfigured() {
				return "", fmt.Errorf("slides upload is not configured")
			}
			return spaces.Upload(key, raw, contentType, "")
		},
		save: func(github, slides, key string) error {
			return getters.UpdateConfTalkResources(ctx, talk.ID, github, slides, key)
		},
		discard: func(key string) {
			if err := spaces.Delete(key); err != nil {
				ctx.Err.Printf("clean up failed talk resources upload %s: %s", key, err)
			}
		},
	}
	if err := updateAdminTalkResources(r, talk, deps); err != nil {
		ctx.Err.Printf("admin talk resources %s: %s", talk.ID, err)
		redirect("error", err.Error())
		return
	}
	invalidateWhoIsDirectoryCache()
	invalidateConferenceCache(ctx)
	redirect("flash", "Talk resources updated.")
}

type adminTalkResourceStore struct {
	upload  func(string, []byte, string) (string, error)
	save    func(string, string, string) error
	discard func(string)
}

// Validate everything before uploading; save the association before discarding
// any failed new upload. Previously published files are retained so existing
// public links keep working when an admin replaces or detaches slides.
func updateAdminTalkResources(r *http.Request, talk *types.ConfTalk, store adminTalkResourceStore) error {
	github, slides, key := talk.GithubRepoURL, talk.SlidesURL, talk.SlidesObjectKey
	if _, ok := r.PostForm["GithubRepoURL"]; ok {
		github = strings.TrimSpace(r.PostForm.Get("GithubRepoURL"))
	}
	if _, ok := r.PostForm["SlidesURL"]; ok {
		slides = strings.TrimSpace(r.PostForm.Get("SlidesURL"))
	}
	if github != "" && !isGithubRepoURL(github) {
		return fmt.Errorf("GitHub link must be an http(s) github.com URL.")
	}
	if slides != "" {
		u, err := url.ParseRequestURI(slides)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return fmt.Errorf("Slides link must be an absolute http(s) URL.")
		}
	}
	raw, contentType, ext, err := readMultipartPresentationFile(r, "SlidesFile")
	if err != nil && err != http.ErrMissingFile {
		return fmt.Errorf("Slides must be a PDF, PPT, PPTX, Keynote, or ODP file up to 40 MiB.")
	}
	remove := r.PostForm.Get("RemoveSlides") == "1"
	if remove && len(raw) > 0 {
		return fmt.Errorf("Choose either removing slides or uploading a replacement.")
	}
	if len(raw) > 0 && slides != "" && slides != talk.SlidesURL {
		return fmt.Errorf("Choose either a slides link or an uploaded file.")
	}
	if slides != talk.SlidesURL {
		key = ""
	}
	if remove {
		slides, key = "", ""
	}
	uploadedKey := ""
	if len(raw) > 0 {
		uploadedKey = fmt.Sprintf("%s/presentations/talk-%d-%s%s", talk.Conf.Tag, time.Now().UTC().UnixNano(), imgproc.ShortID(raw), ext)
		slides, err = store.upload(uploadedKey, raw, contentType)
		if err != nil {
			return fmt.Errorf("Unable to upload slides. Please try again or use a slides link.")
		}
		key = uploadedKey
	}
	if err := store.save(github, slides, key); err != nil {
		if uploadedKey != "" {
			store.discard(uploadedKey)
		}
		return fmt.Errorf("Unable to save talk resources. Please try again.")
	}
	return nil
}
