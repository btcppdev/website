package devpostimport

import "time"

const ManifestVersion = 1

type BatchEvent struct {
	ConferenceTag string `json:"conference_tag"`
	URL           string `json:"url"`
	Output        string `json:"output,omitempty"`
}

type Manifest struct {
	Version       int         `json:"version"`
	ScrapedAt     time.Time   `json:"scraped_at"`
	SourceURL     string      `json:"source_url"`
	ConferenceTag string      `json:"conference_tag"`
	AssetsDir     string      `json:"assets_dir,omitempty"`
	Competition   Competition `json:"competition"`
	Projects      []Project   `json:"projects"`
	Awards        []Award     `json:"awards"`
	Judges        []Person    `json:"judges"`
}

type Competition struct {
	Title       string     `json:"title"`
	Tagline     string     `json:"tagline,omitempty"`
	Description string     `json:"description_html,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	End         *time.Time `json:"end,omitempty"`
	Location    string     `json:"location,omitempty"`
	HeroImage   Asset      `json:"hero_image,omitempty"`
}

type Project struct {
	DevpostID        string   `json:"devpost_id,omitempty"`
	SourceURL        string   `json:"source_url"`
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	ShortDescription string   `json:"short_description,omitempty"`
	DescriptionHTML  string   `json:"description_html,omitempty"`
	Images           []Asset  `json:"images,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	Members          []Person `json:"members,omitempty"`
	AwardTitles      []string `json:"award_titles,omitempty"`
	GitHubURL        string   `json:"github_url,omitempty"`
	DemoURL          string   `json:"demo_url,omitempty"`
	VideoURL         string   `json:"video_url,omitempty"`
	SlidesURL        string   `json:"slides_url,omitempty"`
	DocsURL          string   `json:"docs_url,omitempty"`
}

type Asset struct {
	SourceURL   string `json:"source_url,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Caption     string `json:"caption,omitempty"`
	OriginalURL string `json:"original_url,omitempty"`
	AVIFURL     string `json:"avif_url,omitempty"`
}

type Person struct {
	Name       string `json:"name"`
	Company    string `json:"company,omitempty"`
	DevpostURL string `json:"devpost_url,omitempty"`
	Photo      Asset  `json:"photo,omitempty"`
}

type Award struct {
	DevpostID    string   `json:"devpost_id,omitempty"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	ValueText    string   `json:"value_text,omitempty"`
	SatoshiValue int64    `json:"satoshi_value,omitempty"`
	WinnerCount  int      `json:"winner_count,omitempty"`
	Winners      []string `json:"winner_project_slugs,omitempty"`
}
