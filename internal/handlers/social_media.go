package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"btcpp-web/external/buffer"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"

	"github.com/google/uuid"
)

const maxSocialMediaBytes = 300 * 1000 * 1000

// socialMediaType detects supported formats from bytes, not the supplied filename.
func socialMediaType(header []byte) (kind, contentType, ext string, err error) {
	contentType = http.DetectContentType(header)
	switch contentType {
	case "image/jpeg":
		return "image", contentType, ".jpg", nil
	case "image/png":
		return "image", contentType, ".png", nil
	case "video/mp4":
		return "video", contentType, ".mp4", nil
	}
	// QuickTime uses an ISO base media file header with the qt brand.
	if len(header) >= 12 && string(header[4:8]) == "ftyp" && string(header[8:12]) == "qt  " {
		return "video", "video/quicktime", ".mov", nil
	}
	return "", "", "", fmt.Errorf("choose an MP4 or MOV video, or a JPEG or PNG image")
}

func SocialMediaUpload(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireConfAdmin(w, r, ctx) == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if !spaces.IsConfigured() {
		http.Error(w, "Media uploads are not configured", http.StatusServiceUnavailable)
		return
	}
	limitRequestBody(w, r, maxSocialMediaBytes+maxFormBodyBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "Unable to read upload; maximum file size is 300 MB", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, info, err := r.FormFile("media")
	if err != nil {
		http.Error(w, "Choose a file to upload", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if info.Size <= 0 || info.Size > maxSocialMediaBytes {
		http.Error(w, "File must be between 1 byte and 300 MB", http.StatusBadRequest)
		return
	}
	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		http.Error(w, "Unable to read media", http.StatusBadRequest)
		return
	}
	kind, contentType, ext, err := socialMediaType(header[:n])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Unable to read media", http.StatusInternalServerError)
		return
	}
	// Unique URLs keep already queued media stable when another file is uploaded.
	key := "social-media/" + conf.Tag + "/" + uuid.NewString() + ext
	mediaURL, err := spaces.UploadStream(key, file, contentType, info.Size)
	if err != nil {
		ctx.Err.Printf("/%s/admin/social/media upload: %s", conf.Tag, err)
		http.Error(w, "Unable to upload media; please try again", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buffer.Asset{URL: mediaURL, Kind: kind})
}

// Only accept media hosted in this event's upload directory.
func validateSocialUpload(confTag, field, u, kind string) (*buffer.Asset, error) {
	prefix := spaces.PublicURL("social-media/" + confTag + "/")
	if !strings.HasPrefix(u, prefix) || len(u) == len(prefix) || strings.ContainsAny(strings.TrimPrefix(u, prefix), "/?#") {
		return nil, fmt.Errorf("invalid uploaded media for %s; upload the file again", field)
	}
	filename := strings.TrimPrefix(u, prefix)
	ext := path.Ext(filename)
	if _, err := uuid.Parse(strings.TrimSuffix(filename, ext)); err != nil {
		return nil, fmt.Errorf("invalid uploaded media for %s", field)
	}
	expectedKind := ""
	switch ext {
	case ".mp4", ".mov":
		expectedKind = "video"
	case ".jpg", ".png":
		expectedKind = "image"
	}
	if kind != expectedKind || expectedKind == "" {
		return nil, fmt.Errorf("media type does not match uploaded file for %s", field)
	}
	if kind != "image" && kind != "video" {
		return nil, fmt.Errorf("invalid media type for %s", field)
	}
	return &buffer.Asset{URL: u, Kind: kind}, nil
}

// SocialMediaItem identifies a generated attachment or an uploaded file.
// Generated cards keep their channel-specific variants while moving as one item.
type SocialMediaItem struct {
	Source string `json:"source"`
	URL    string `json:"url,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

func socialMediaSelection(r *http.Request, confTag, field string) ([]SocialMediaItem, error) {
	raw := r.FormValue("media_items_" + field)
	if raw == "" {
		var defaults []SocialMediaItem
		if strings.HasPrefix(field, "speaker_") {
			if r.FormValue("photo_"+field) != "" || r.FormValue("instaphoto_"+field) != "" {
				defaults = append(defaults, SocialMediaItem{Source: "card"})
			}
			if r.FormValue("speakerphoto_"+field) != "" {
				defaults = append(defaults, SocialMediaItem{Source: "photo"})
			}
		} else if r.FormValue("photo_"+field) != "" || r.FormValue("card_"+field) != "" {
			defaults = append(defaults, SocialMediaItem{Source: "card"})
		}
		return defaults, nil
	}
	var items []SocialMediaItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil || len(items) == 0 || len(items) > 20 {
		return nil, fmt.Errorf("invalid media list for %s", field)
	}
	videos := 0
	for _, item := range items {
		switch item.Source {
		case "card":
		case "photo":
			if !strings.HasPrefix(field, "speaker_") {
				return nil, fmt.Errorf("invalid speaker photo for %s", field)
			}
		case "upload":
			if _, err := validateSocialUpload(confTag, field, item.URL, item.Kind); err != nil {
				return nil, err
			}
			if item.Kind == "video" {
				videos++
			}
		default:
			return nil, fmt.Errorf("invalid media source for %s", field)
		}
	}
	if videos > 0 && len(items) != 1 {
		return nil, fmt.Errorf("%s: a video must be the only media; remove the other attachments", field)
	}
	return items, nil
}

func selectedSocialAssets(r *http.Request, field, service string, items []SocialMediaItem) []buffer.Asset {
	var assets []buffer.Asset
	for _, item := range items {
		asset := buffer.Asset{Kind: "image"}
		switch item.Source {
		case "upload":
			asset = buffer.Asset{URL: item.URL, Kind: item.Kind}
		case "photo":
			asset.URL = r.FormValue("speakerphoto_" + field)
		case "card":
			asset.URL = r.FormValue("photo_" + field)
			if strings.HasPrefix(field, "sponsor_") {
				asset.URL = r.FormValue("card_" + field)
			}
			if strings.HasPrefix(field, "speaker_") && (service == "twitter" || service == "instagram") && r.FormValue("instaphoto_"+field) != "" {
				asset.URL = r.FormValue("instaphoto_" + field)
			}
		}
		if asset.URL != "" {
			assets = append(assets, asset)
		}
	}
	return assets
}

func socialSelectionHasVideo(items []SocialMediaItem) bool {
	for _, item := range items {
		if item.Source == "upload" && item.Kind == "video" {
			return true
		}
	}
	return false
}
