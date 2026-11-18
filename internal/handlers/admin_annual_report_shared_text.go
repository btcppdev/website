package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"btcpp-web/internal/config"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func annualSharedTextScope(year int) string {
	return fmt.Sprintf("annual-text-v1/%d", year)
}

// Only editorial fields can be shared. Scope, analytics, exclusions, recipient
// identity and frozen snapshots never enter the shared-text record.
func annualSharedTextKey(key string) bool {
	for _, section := range annualSections(2026, "") {
		if key == section.Key {
			return true
		}
	}
	id := ""
	if strings.HasPrefix(key, "event_highlight_") {
		id = strings.TrimPrefix(key, "event_highlight_")
	} else if projectID, ok := annualProjectStoryKey(key); ok {
		id = projectID
	}
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil
}

func annualSharedTextSelection(form url.Values) ([]string, error) {
	if form.Get("action") != "share_text" {
		return nil, nil
	}
	var selected []string
	seen := map[string]bool{}
	for _, key := range form["share_field"] {
		if !annualSharedTextKey(key) || len(form[key]) != 1 {
			return nil, fmt.Errorf("select text fields from this report to share")
		}
		if !seen[key] {
			selected = append(selected, key)
			seen[key] = true
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("select at least one text field to share across sponsors")
	}
	return selected, nil
}

func (p annualReportPage) sharedTextPatch() (map[string]string, error) {
	available := map[string]bool{}
	for _, section := range p.Sections {
		available[section.Key] = true
	}
	for _, highlight := range p.EventHighlightEditors() {
		available[highlight.Copy.Key] = true
	}
	for _, project := range p.BriefProjects() {
		for _, field := range project.StoryFields() {
			available[field.Key] = true
		}
	}
	patch := map[string]string{}
	for _, key := range p.ShareTextFields {
		value, present := p.Saved[key]
		if !available[key] || !present || !annualSharedTextKey(key) {
			return nil, fmt.Errorf("select text fields from this report to share")
		}
		patch[key] = value // An intentional blank is shared too.
	}
	return patch, nil
}

func loadAnnualSharedText(ctx context.Context, app *config.AppContext, p *annualReportPage) error {
	var content []byte
	err := app.DB.QueryRow(ctx, `SELECT content,revision FROM sponsor_report_drafts WHERE scope=$1`, annualSharedTextScope(p.Year)).Scan(&content, &p.SharedTextRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var shared map[string]string
	if err := json.Unmarshal(content, &shared); err != nil {
		return err
	}
	if p.Saved == nil {
		p.Saved = map[string]string{}
	}
	for key, text := range shared {
		if !annualSharedTextKey(key) {
			return fmt.Errorf("invalid shared report text field")
		}
		if _, exists := p.Saved[key]; !exists {
			p.Saved[key] = text
		}
	}
	return nil
}

// Serialize annual saves for the same year so sharing cannot race a recipient
// save or a first draft. The revision also protects forms opened before a share.
func lockAnnualSharedText(ctx context.Context, tx pgx.Tx, p annualReportPage) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, annualSharedTextScope(p.Year)); err != nil {
		return err
	}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM sponsor_report_drafts WHERE scope=$1`, annualSharedTextScope(p.Year)).Scan(&revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if revision != p.SharedTextRevision {
		return errReportConflict
	}
	return nil
}

func saveAnnualSharedText(ctx context.Context, tx pgx.Tx, p annualReportPage, patch map[string]string) error {
	if len(patch) == 0 {
		return nil
	}
	content, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO sponsor_report_drafts(scope,content) VALUES($1,$2::jsonb)
 ON CONFLICT(scope) DO UPDATE SET content=sponsor_report_drafts.content || EXCLUDED.content,
 revision=sponsor_report_drafts.revision+1,updated_at=now()`, annualSharedTextScope(p.Year), string(content))
	if err != nil {
		return err
	}
	// Match live recipient drafts exactly, never /frozen/ editions or other years.
	pattern := fmt.Sprintf(`^annual-v1/%d/(nydig|hrf|org/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`, p.Year)
	_, err = tx.Exec(ctx, `UPDATE sponsor_report_drafts SET content=content || $2::jsonb,revision=revision+1
 WHERE scope ~ $1 AND scope<>$3`, pattern, string(content), p.Scope())
	// Keep other drafts' updated_at: it marks their last event-scope review and
	// determines whether newly completed events should enter legacy selections.
	return err
}
