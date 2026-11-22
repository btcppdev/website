package getters

import (
	"context"
	"testing"
)

func TestConferenceDetailsPreservesRetiredCopy(t *testing.T) {
	app := databaseSmokeContext(t)
	id, tag := insertSmokeConference(t, app)
	_, err := app.DB.Exec(context.Background(), `UPDATE conferences SET hero_caption='Historical caption',local_ticket_body='Historical eligibility',hackathon_section_label='Historical label' WHERE id=$1`, id)
	if err != nil {
		t.Fatal(err)
	}
	in := ConfDetailsInput{
		Description: "Updated event", Timezone: "UTC", AccentColor: "#db7900",
		HeroTitle: "Hero", AboutTitle: "About", AboutBody: "First paragraph", AboutBody2: "Second paragraph",
		VenueTitle: "Our venue", VenueSubtitle: "Venue subtitle", VenueBody: "Venue details", HotelsIntro: "Nearby hotels",
		SpeakersTitle: "Who's coming", SpeakersBody: "Meet the speakers",
		HackathonHeadline: "Build together", HackathonProofLabel: "Last edition",
		MapLatitude: 51.438, MapLongitude: 5.479, MapXPercent: 50.54, MapYPercent: 36.90,
		MapLabel: "Eindhoven", MapLabelSide: "right",
	}
	if err = UpdateConfDetails(app, id, in); err != nil {
		t.Fatal(err)
	}
	conf, err := GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	if conf.HeroCaption != "Historical caption" || conf.LocalTicketBody != "Historical eligibility" || conf.HackathonSectionLabel != "Historical label" {
		t.Fatal("saving active event details erased retired copy")
	}
	for _, pair := range [][2]string{
		{conf.Desc, in.Description}, {conf.HeroTitle, in.HeroTitle}, {conf.AboutTitle, in.AboutTitle},
		{conf.AboutBody, in.AboutBody}, {conf.AboutBody2, in.AboutBody2},
		{conf.VenueTitle, in.VenueTitle}, {conf.VenueSubtitle, in.VenueSubtitle}, {conf.VenueBody, in.VenueBody},
		{conf.HotelsIntro, in.HotelsIntro}, {conf.SpeakersTitle, in.SpeakersTitle}, {conf.SpeakersBody, in.SpeakersBody},
		{conf.HackathonHeadline, in.HackathonHeadline}, {conf.HackathonProofLabel, in.HackathonProofLabel},
		{conf.MapLabel, in.MapLabel}, {conf.MapLabelSide, in.MapLabelSide}, {conf.AccentColor, in.AccentColor},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("active field saved as %q; want %q", pair[0], pair[1])
		}
	}
	if conf.MapLatitude != in.MapLatitude || conf.MapLongitude != in.MapLongitude || conf.MapXPercent != in.MapXPercent || conf.MapYPercent != in.MapYPercent {
		t.Fatal("map position was not saved correctly")
	}
}
