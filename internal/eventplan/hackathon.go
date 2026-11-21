package eventplan

// Hackathon edits the competition attached to an event, rather than keeping a
// second copy of its public settings in the planning document.
type Hackathon struct {
	Included             bool   `json:"included"`
	ID                   string `json:"id"`
	UpdatedAt            string `json:"updated_at"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	DescriptionFormat    string `json:"description_format"`
	Visibility           string `json:"visibility"`
	MaxTeamSize          *int   `json:"max_team_size"`
	PublicGalleryEnabled bool   `json:"public_gallery_enabled"`
	AllowLateSubmissions bool   `json:"allow_late_submissions"`
	PublicTablesEnabled  bool   `json:"public_tables_enabled"`
}
