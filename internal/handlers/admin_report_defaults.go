package handlers

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// Only reviewed narrative and dated video statistics are bundled. Attendance,
// project membership, delivery confirmation and payouts always use live records.
//
//go:embed reportdata/nairobi-defaults.json
var nairobiReportDefaults []byte

func applyNairobiReportDefaults(page *sponsorReportPage) error {
	var defaults struct {
		Saved  map[string]string
		Videos []struct {
			ID    string
			Views int64
		}
		Panels []struct{ Title, Note string }
	}
	if err := json.Unmarshal(nairobiReportDefaults, &defaults); err != nil {
		return err
	}
	for key, value := range defaults.Saved {
		page.Saved[key] = value
	}
	views := map[string]int64{}
	for _, v := range defaults.Videos {
		views[v.ID] = v.Views
	}
	for _, s := range page.Sessions {
		if count, ok := views[reportYouTubeVideoID(s.YouTube)]; ok {
			page.Saved["views_"+s.ID] = strconv.FormatInt(count, 10)
		}
		for _, panel := range defaults.Panels {
			if s.Kind == "panel" && s.Title == panel.Title {
				page.Saved["session_note_"+s.ID] = panel.Note
			}
		}
	}
	return nil
}

func reportYouTubeVideoID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "youtu.be" {
		return strings.Trim(u.Path, "/")
	}
	if host != "youtube.com" && host != "www.youtube.com" && host != "youtube-nocookie.com" && host != "www.youtube-nocookie.com" {
		return ""
	}
	if id := u.Query().Get("v"); id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 2 && (parts[0] == "embed" || parts[0] == "live" || parts[0] == "shorts") {
		return parts[1]
	}
	return ""
}
