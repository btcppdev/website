package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"btcpp-web/internal/config"
)

func TestAnnualReportQuoteAttribution(t *testing.T) {
	p := annualFixture()
	quote := p.Section("community_quote").Text
	p.QuoteAuthors = map[string]annualQuoteAuthor{"community_quote": {Name: "A participant", Quote: quote}}
	if p.ParticipantQuote("community_quote").Author == nil || p.ParticipantQuote("reach_quote").Author != nil {
		t.Fatal("attribution did not stay attached to its confirmed quote")
	}
	for _, text := range []string{"A replacement quote by someone else.", ""} {
		p.Saved["community_quote"] = text
		p.loadNarrativeSections()
		if p.ParticipantQuote("community_quote").Author != nil {
			t.Fatal("replacement or removed quote inherited an attribution")
		}
	}
}

func TestAnnualReportQuoteProfilesPostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, reportFixtureSQL+`
INSERT INTO people(id,name,norm_photo_path,company,twitter_handle) VALUES
 ('0dc4ca45-a545-43ce-a485-ff39e9ab6470','Confirmed builder','https://example.test/builder.jpg','','https://x.com/test_builder'),
 ('7ed6d0a4-da67-48e7-a542-3d6f152312ec','Confirmed reader','','Profile organization','@test_reader');
INSERT INTO speaker_confs(id,speaker_id,company) VALUES
 ('quote-speaker','0dc4ca45-a545-43ce-a485-ff39e9ab6470','Event organization');`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	p := annualFixture()
	app := &config.AppContext{DB: db}
	if err := loadAnnualQuoteAuthors(ctx, app, tx, &p); err != nil {
		t.Fatal(err)
	}
	a := p.ParticipantQuote("community_quote").Author
	b := p.ParticipantQuote("closing_quote").Author
	if a == nil || a.Company != "Event organization" || a.Twitter.Handle != "test_builder" || a.Photo != "https://example.test/builder.jpg" || b == nil || b.Company != "Profile organization" || b.Photo != "" {
		t.Fatalf("profile fields or event affiliation fallback lost: %+v / %+v", a, b)
	}
	p.Frozen = p.Generated
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var frozen annualReportPage
	if err := json.Unmarshal(raw, &frozen); err != nil || frozen.ParticipantQuote("community_quote").Author == nil {
		t.Fatal("frozen quote attribution was lost", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE people SET is_deleted_account=true WHERE id='0dc4ca45-a545-43ce-a485-ff39e9ab6470'`); err != nil {
		t.Fatal(err)
	}
	if err := loadAnnualQuoteAuthors(ctx, app, tx, &p); err != nil || p.ParticipantQuote("community_quote").Author != nil {
		t.Fatal("deleted profile included in a fresh report", err)
	}
	p.Year = 2027
	if err := loadAnnualQuoteAuthors(ctx, app, tx, &p); err != nil || len(p.QuoteAuthors) != 0 {
		t.Fatal("2026 attributions leaked into another year", err)
	}
}
