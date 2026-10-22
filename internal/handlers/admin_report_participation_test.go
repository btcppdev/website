package handlers

import (
	"net/url"
	"testing"
)

func TestReportPublishedProgramAndLocations(t *testing.T) {
	p := reportFixturePage(t)
	c := p.ProgramSummary()
	if c == nil || c.DonutTotal() != 44 || c.Rows[0].Value != 32 || c.Rows[1].Value != 9 || c.Rows[2].Value != 3 || p.SpeakerCount() != 44 || p.RecordingCount() != 44 {
		t.Fatal("public agenda snapshot totals changed; verify source")
	}
	if len(p.MappedSpeakers()) != 0 {
		t.Fatal("locations inferred without confirmation")
	}
	p.Sessions = append(p.Sessions, p.Sessions[0])
	p.Speakers = append(p.Speakers, p.Speakers[0])
	if p.ProgramSummary().DonutTotal() != 44 || p.SpeakerCount() != 44 || p.RecordingCount() != 44 {
		t.Fatal("duplicate counted")
	}
}
func TestReportSpeakerLocationValidation(t *testing.T) {
	f := url.Values{"speaker_location_one_place": {"Nairobi, Kenya"}, "speaker_location_one_lat": {"-1.29"}, "speaker_location_one_lon": {"36.82"}}
	if saveReportSpeakerLocation(f, map[string]string{}, "one") == nil {
		t.Fatal("unsourced location accepted")
	}
	f.Set("speaker_location_one_source", "Speaker confirmed")
	saved := map[string]string{}
	if err := saveReportSpeakerLocation(f, saved, "one"); err != nil {
		t.Fatal(err)
	}
	p := sponsorReportPage{Saved: saved, Speakers: []reportPerson{{ID: "one", Name: "Example"}}}
	m := p.MappedSpeakers()
	if len(m) != 1 || m[0].X < 180 || m[0].X > 210 || m[0].Y < 175 || m[0].Y > 180 {
		t.Fatal("projection incorrect")
	}
	for _, bad := range []string{"NaN", "Inf", "91"} {
		f.Set("speaker_location_one_lat", bad)
		if saveReportSpeakerLocation(f, map[string]string{}, "one") == nil {
			t.Fatalf("accepted latitude %s", bad)
		}
	}
}

func TestReportSpeakerOriginDefaultsFromEventForm(t *testing.T) {
	p := sponsorReportPage{Speakers: []reportPerson{{ID: "one", ComingFrom: "Lagos, Nigeria"}, {ID: "two", ComingFrom: "unknown"}}}
	l := p.SpeakerLocations()[0]
	if l.Place != "Lagos, Nigeria" || l.Source != "Speaker's event form · traveling from · approximate city centre (GeoNames)" || !l.Mapped || len(p.ReportedSpeakerLocations()) != 1 {
		t.Fatal("event form origin not used correctly")
	}
	f := url.Values{"speaker_location_one_place": {l.Place}, "speaker_location_one_source": {l.Source}}
	saved := map[string]string{}
	if err := saveReportSpeakerLocation(f, saved, "one"); err != nil {
		t.Fatal("unmapped origin prevents saving", err)
	}
	p.Saved = saved
	p.Saved["speaker_location_one_place"] = "Abuja, Nigeria"
	if p.SpeakerLocations()[0].Place != "Abuja, Nigeria" {
		t.Fatal("report correction ignored")
	}
}

func TestReportSpeakerAutomaticMapAndOverrides(t *testing.T) {
	p := sponsorReportPage{Speakers: []reportPerson{{ID: "one", ComingFrom: "Nairobi, Kenya"}, {ID: "two", ComingFrom: "Springfield"}, {ID: "three", ComingFrom: "Nairobi / London"}}}
	if len(p.MappedSpeakers()) != 1 || len(p.ReportedSpeakerLocations()) != 3 {
		t.Fatal("automatic mapping or unresolved coverage incorrect")
	}
	p.Saved = map[string]string{"speaker_location_one_place": "Nairobi, Kenya", "speaker_location_one_source": "Admin correction", "speaker_location_one_lat": "1", "speaker_location_one_lon": "2"}
	l := p.SpeakerLocations()[0]
	if l.Latitude != "1" || l.Longitude != "2" || l.Source != "Admin correction" {
		t.Fatal("manual coordinates overwritten")
	}
	p.Saved["speaker_location_one_lat"] = ""
	if p.SpeakerLocations()[0].Mapped {
		t.Fatal("partial coordinates silently replaced")
	}
}
