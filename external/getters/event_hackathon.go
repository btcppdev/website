package getters

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

func LoadPlanningHackathon(app *config.AppContext, conf *types.Conf) (eventplan.Hackathon, error) {
	h := eventplan.Hackathon{Title: conf.Desc + " hackathon", DescriptionFormat: CompetitionDescriptionFormatMarkdown, Visibility: CompetitionVisibilityHidden}
	competition, err := GetCompetitionByConferenceID(app, conf.Ref)
	if err != nil || competition == nil {
		return h, err
	}
	return eventplan.Hackathon{
		Included: true, ID: competition.ID, UpdatedAt: competition.UpdatedAt.Format(time.RFC3339Nano),
		Title: competition.Title, Description: competition.Description, DescriptionFormat: competition.DescriptionFormat,
		Visibility: competition.Visibility, MaxTeamSize: competition.MaxTeamSize,
		PublicGalleryEnabled: competition.PublicGalleryEnabled, AllowLateSubmissions: competition.AllowLateSubmissions, PublicTablesEnabled: competition.PublicTablesEnabled,
	}, nil
}

// The caller holds the event/plan locks. Also check the competition's timestamp
// so a wizard tab cannot overwrite a newer edit made in hackathon administration.
func savePlanningHackathon(c context.Context, tx pgx.Tx, conf *types.Conf, h eventplan.Hackathon) error {
	var id string
	var updated time.Time
	err := tx.QueryRow(c, `SELECT id::text,updated_at FROM competitions WHERE conference_id=$1 FOR UPDATE`, conf.Ref).Scan(&id, &updated)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id != h.ID {
		return ErrEventPlanConflict
	}
	if id != "" {
		seen, e := time.Parse(time.RFC3339Nano, h.UpdatedAt)
		if e != nil || !seen.Equal(updated) {
			return ErrEventPlanConflict
		}
	}
	if !h.Included {
		if id != "" {
			return fmt.Errorf("this event already has a hackathon; use Hidden visibility to take it off the public site")
		}
		return nil
	}
	h.Title = strings.TrimSpace(h.Title)
	if h.Title == "" || utf8.RuneCountInString(h.Title) > 160 {
		return fmt.Errorf("enter a hackathon title of 1–160 characters")
	}
	if len(h.Description) > 100000 {
		return fmt.Errorf("hackathon description must be under 100 KB")
	}
	if h.MaxTeamSize != nil && (*h.MaxTeamSize < 1 || int64(*h.MaxTeamSize) > 2147483647) {
		return fmt.Errorf("maximum team size must be a positive whole number, or left blank")
	}
	if h.DescriptionFormat == "" {
		h.DescriptionFormat = CompetitionDescriptionFormatMarkdown
	}
	switch h.DescriptionFormat {
	case CompetitionDescriptionFormatPlain, CompetitionDescriptionFormatMarkdown, CompetitionDescriptionFormatHTML:
	default:
		return fmt.Errorf("choose a valid description format")
	}
	if h.Visibility == "" {
		h.Visibility = CompetitionVisibilityHidden
	}
	if h.Visibility != CompetitionVisibilityHidden && h.Visibility != CompetitionVisibilityPublic {
		return fmt.Errorf("choose Hidden or Public visibility")
	}
	if id == "" {
		err = tx.QueryRow(c, `INSERT INTO competitions
 (conference_id,title,description,description_format,visibility,max_team_size,public_gallery_enabled,allow_late_submissions,public_tables_enabled)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
 ON CONFLICT(conference_id) DO NOTHING RETURNING id::text`,
			conf.Ref, h.Title, h.Description, h.DescriptionFormat, h.Visibility, h.MaxTeamSize, h.PublicGalleryEnabled, h.AllowLateSubmissions, h.PublicTablesEnabled).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventPlanConflict
		}
		return err
	}
	// Only update the fields in this step. Schedule, judges, results, awards and
	// lifecycle overrides remain owned by the existing hackathon tools.
	_, err = tx.Exec(c, `UPDATE competitions SET title=$2,description=$3,description_format=$4,visibility=$5,
 max_team_size=$6,public_gallery_enabled=$7,allow_late_submissions=$8,public_tables_enabled=$9
 WHERE id=$1`, id, h.Title, h.Description, h.DescriptionFormat, h.Visibility, h.MaxTeamSize, h.PublicGalleryEnabled, h.AllowLateSubmissions, h.PublicTablesEnabled)
	return err
}
