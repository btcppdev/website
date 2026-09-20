package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

func AdminAttachRecording(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	admin := requireConfAdmin(w, r, ctx)
	if admin == nil {
		return
	}
	serveAdminAttachRecording(w, r, ctx, admin)
}

func serveAdminAttachRecording(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, admin *auth.Identity) {
	tag, proposalID := mux.Vars(r)["conf"], mux.Vars(r)["proposalID"]
	if tag == "" || admin == nil || !admin.Satisfies(auth.Spec{Conf: tag, Role: auth.RoleAdmin}) {
		http.Error(w, "Event administrator required", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to secure recording form", http.StatusInternalServerError)
		return
	}
	if !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
		http.Error(w, "Invalid form token", http.StatusForbidden)
		return
	}
	talk, err := getters.GetConfTalkByProposal(ctx, proposalID)
	if err != nil {
		http.Error(w, "Unable to load talk", http.StatusInternalServerError)
		return
	}
	if !adminTalkResourcesInConference(talk, tag) {
		http.Error(w, "Accepted talk not found in this event", http.StatusNotFound)
		return
	}
	redirect := func(field, message string) {
		q := url.Values{field: {message}}
		if back := r.PostForm.Get("ReturnURL"); safeAdminReturn(back, tag) {
			q.Set("return", back)
		}
		target := fmt.Sprintf("/%s/admin/proposal/%s/edit?%s#recording", url.PathEscape(tag), url.PathEscape(proposalID), q.Encode())
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
	existing, err := getters.GetRecordingByConfTalk(ctx, talk.ID)
	if err != nil {
		http.Error(w, "Unable to load recording", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		yt, x := getJob(existing.ID), getXJob(existing.ID)
		if (yt != nil && yt.Status == "running") || (x != nil && x.Status == "running") {
			redirect("error", "Wait for the current publishing job to finish before changing the recording source.")
			return
		}
	}
	_, err = attachAdminTalkRecording(talk, tag, r.PostForm.Get("RecordingSource"), spaces.BaseURL(), spaces.Exists,
		func(id string, update getters.RecordingUpsert) (*types.Recording, error) {
			if existing == nil && talk.Proposal != nil {
				name := talk.Proposal.Title
				update.TalkName = &name
			}
			return getters.UpsertRecordingForConfTalk(ctx, id, update)
		})
	if err != nil {
		ctx.Err.Printf("attach recording for talk %s: %s", talk.ID, err)
		redirect("error", err.Error())
		return
	}
	invalidateWhoIsDirectoryCache()
	invalidateConferenceCache(ctx)
	redirect("flash", "Recording attached. Open Manage recording and publishing to continue.")
}

func attachAdminTalkRecording(talk *types.ConfTalk, tag, source, baseURL string, exists func(string) bool, save func(string, getters.RecordingUpsert) (*types.Recording, error)) (*types.Recording, error) {
	if !adminTalkResourcesInConference(talk, tag) {
		return nil, fmt.Errorf("Talk does not belong to this event.")
	}
	key, err := parseAdminRecordingSource(source, baseURL)
	if err != nil {
		return nil, err
	}
	if !exists(key) {
		return nil, fmt.Errorf("Unable to find or access this file in Spaces. Check the path and wait for the upload to finish.")
	}
	// Only the source is changed on existing rows: published URLs, copy, and
	// scheduling are deliberately left intact. The getter enforces one row per talk.
	rec, err := save(talk.ID, getters.RecordingUpsert{FileURI: &key})
	if err != nil {
		return nil, fmt.Errorf("Unable to save recording. Please try again.")
	}
	return rec, nil
}

// A URL must point at the configured bucket (origin or DigitalOcean CDN).
// Never fetch the submitted URL; existence is checked through the Spaces client.
func parseAdminRecordingSource(source, baseURL string) (string, error) {
	key := strings.TrimSpace(source)
	if strings.Contains(key, "://") {
		u, err := url.Parse(key)
		base, baseErr := url.Parse(baseURL)
		if err != nil || baseErr != nil || base == nil || base.Host == "" {
			return "", fmt.Errorf("Use an object key, or a URL from this site's configured Spaces bucket.")
		}
		allowedHost := strings.EqualFold(u.Host, base.Host)
		if strings.HasSuffix(base.Host, ".digitaloceanspaces.com") && !strings.Contains(base.Host, ".cdn.") {
			cdn := strings.TrimSuffix(base.Host, ".digitaloceanspaces.com") + ".cdn.digitaloceanspaces.com"
			allowedHost = allowedHost || strings.EqualFold(u.Host, cdn)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !allowedHost || u.Fragment != "" {
			return "", fmt.Errorf("Use a URL from this site's Spaces bucket, or paste the object key directly.")
		}
		// URL.Path decodes escaped spaces once; raw object keys are kept verbatim.
		key = strings.TrimPrefix(u.Path, "/")
	}
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsAny(key, "\\:") || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("Enter a Spaces object key without a leading slash or a URL from this site's bucket.")
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("The recording path cannot contain empty, '.' or '..' segments.")
		}
	}
	return key, nil
}
