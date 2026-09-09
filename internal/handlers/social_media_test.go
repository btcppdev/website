package handlers

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"btcpp-web/external/spaces"
	"encoding/json"
)

func TestSocialMediaType(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      []byte
		kind, ext string
	}{
		{"png", []byte("\x89PNG\r\n\x1a\n"), "image", ".png"},
		{"jpeg", []byte("\xff\xd8\xff\xe0"), "image", ".jpg"},
		{"mp4", []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), "video", ".mp4"},
		{"mov", []byte("\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00qt  "), "video", ".mov"},
		{"html", []byte("<html><script>alert(1)</script></html>"), "", ""},
		{"empty", nil, "", ""},
		{"svg", []byte("<svg onload='alert(1)'></svg>"), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, _, ext, err := socialMediaType(tc.data)
			if (err != nil) != (tc.kind == "") || kind != tc.kind || ext != tc.ext {
				t.Fatalf("got %q %q %v", kind, ext, err)
			}
		})
	}
}

func TestValidateSocialUpload(t *testing.T) {
	const filename = "60e1228f-2be2-4a48-bba7-8dd92b977d32.mp4"
	valid := spaces.PublicURL("social-media/berlin26/" + filename)
	for _, tc := range []struct {
		name, mediaURL, kind string
		valid                bool
	}{

		{"video", valid, "video", true},
		{"wrong type", valid, "image", false},
		{"wrong event", spaces.PublicURL("social-media/other/" + filename), "video", false},
		{"external", "https://example.test/" + filename, "video", false},
		{"query", valid + "?other=1", "video", false},
		{"traversal", spaces.PublicURL("social-media/berlin26/../" + filename), "video", false},
		{"encoded traversal", spaces.PublicURL("social-media/berlin26/%2e%2e%2f" + filename), "video", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asset, err := validateSocialUpload("berlin26", "speaker_a", tc.mediaURL, tc.kind)
			if (err == nil) != tc.valid {
				t.Fatalf("asset=%#v error=%v", asset, err)
			}
			if tc.valid && tc.mediaURL != "" && (asset == nil || asset.URL != tc.mediaURL || asset.Kind != tc.kind) {
				t.Fatalf("asset=%#v", asset)
			}
		})
	}
}

func TestSocialMediaSelectionOrderAndRemoval(t *testing.T) {
	upload := spaces.PublicURL("social-media/berlin26/60e1228f-2be2-4a48-bba7-8dd92b977d32.png")
	for _, tc := range []struct {
		name, raw string
		want      []string
	}{
		{"defaults", "", []string{"square.png", "portrait.jpg"}},
		{"reordered", `[{"source":"photo"},{"source":"card"}]`, []string{"portrait.jpg", "square.png"}},
		{"removed card", `[{"source":"photo"}]`, []string{"portrait.jpg"}},
		{"removed photo", `[{"source":"card"}]`, []string{"square.png"}},
		{"upload last", `[{"source":"photo"},{"source":"upload","kind":"image","url":"` + upload + `"}]`, []string{"portrait.jpg", upload}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"media_items_speaker_a": {tc.raw}, "photo_speaker_a": {"wide.png"}, "instaphoto_speaker_a": {"square.png"}, "speakerphoto_speaker_a": {"portrait.jpg"}}
			r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			items, err := socialMediaSelection(r, "berlin26", "speaker_a")
			if err != nil {
				t.Fatal(err)
			}
			for _, service := range []string{"twitter", "instagram", "linkedin"} {
				got := selectedSocialAssets(r, "speaker_a", service, items)
				var urls []string
				for _, asset := range got {
					urls = append(urls, asset.URL)
				}
				want := append([]string(nil), tc.want...)
				if service == "linkedin" {
					for i, u := range want {
						if u == "square.png" {
							want[i] = "wide.png"
						}
					}
				}
				if !reflect.DeepEqual(urls, want) {
					t.Fatalf("%s got %v want %v", service, urls, want)
				}
			}
		})
	}
}

func TestSocialMediaSelectionValidation(t *testing.T) {
	video := SocialMediaItem{Source: "upload", URL: spaces.PublicURL("social-media/berlin26/60e1228f-2be2-4a48-bba7-8dd92b977d32.mp4"), Kind: "video"}
	for _, tc := range []struct {
		name  string
		items []SocialMediaItem
		valid bool
	}{
		{"video alone", []SocialMediaItem{video}, true},
		{"mixed video", []SocialMediaItem{video, {Source: "card"}}, false},
		{"multiple videos", []SocialMediaItem{video, video}, false},
		{"empty", []SocialMediaItem{}, false},
		{"unknown source", []SocialMediaItem{{Source: "unknown"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.items)
			r := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{"media_items_speaker_a": {string(raw)}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			items, err := socialMediaSelection(r, "berlin26", "speaker_a")
			if (err == nil) != tc.valid {
				t.Fatalf("items %v error %v", items, err)
			}
		})
	}
}
