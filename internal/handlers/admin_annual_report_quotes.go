package handlers

import (
	"context"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

type annualQuoteAuthor struct {
	ID, Name, Alias, Photo, Company, Quote string
	Twitter                                types.Twitter
}

type annualParticipantQuote struct {
	reportSection
	Author *annualQuoteAuthor
}

func (p annualReportPage) ParticipantQuote(key string) annualParticipantQuote {
	quote := annualParticipantQuote{reportSection: p.BriefSection(key)}
	// An edited or replaced quote must not inherit someone else's attribution.
	if author, ok := p.QuoteAuthors[key]; ok && quote.Text != "" && strings.TrimSpace(quote.Text) == strings.TrimSpace(author.Quote) {
		quote.Author = &author
	}
	return quote
}

// Organizer-confirmed authors, matched by person ID. Only public-facing profile
// fields are read. These details are serialized with newly frozen editions.
func loadAnnualQuoteAuthors(ctx context.Context, app *config.AppContext, tx pgx.Tx, p *annualReportPage) error {
	p.QuoteAuthors = nil
	if p.Year != 2026 {
		return nil
	}
	keys := map[string]string{
		"0dc4ca45-a545-43ce-a485-ff39e9ab6470": "community_quote",
		"7ed6d0a4-da67-48e7-a542-3d6f152312ec": "closing_quote",
	}
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT p.id::text,p.name,p.norm_photo_path,p.twitter_handle,
 coalesce(nullif(trim(p.company),''),
 (SELECT string_agg(DISTINCT trim(sc.company),' · ' ORDER BY trim(sc.company)) FROM speaker_confs sc WHERE sc.speaker_id=p.id AND trim(sc.company)<>''),'')
 FROM people p WHERE p.id::text=ANY($1::text[]) AND NOT p.is_deleted_account`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	p.QuoteAuthors = make(map[string]annualQuoteAuthor)
	for rows.Next() {
		var author annualQuoteAuthor
		var twitter string
		if err := rows.Scan(&author.ID, &author.Name, &author.Photo, &twitter, &author.Company); err != nil {
			return err
		}
		author.Twitter = types.ParseTwitter(twitter)
		if strings.TrimSpace(author.Photo) != "" {
			author.Photo = SpeakerPhotoURL(app, author.Photo)
		}
		key := keys[author.ID]
		if key == "community_quote" {
			author.Alias = "Gotcha"
		}
		for _, section := range annualSections(p.Year, p.Recipient) {
			if section.Key == key {
				author.Quote = section.Text
				break
			}
		}
		p.QuoteAuthors[key] = author
	}
	return rows.Err()
}
