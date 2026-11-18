package eventplan

import (
	"testing"
	"time"
)

func TestStaffingUsesLocalAccessAndDistinctStages(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	p := Plan{ShiftHours: 4, MerchDesk: true, Venues: []Venue{{ID: "venue-a", Name: "Hall", Stages: "Main, Workshop", Days: []Day{{Date: "2027-05-12", StaffOpen: "07:00", DoorsOpen: "09:00", DoorsClose: "18:00", StaffClose: "20:00"}}}}}
	shifts, e := Staffing(p, loc)
	if e != nil {
		t.Fatal(e)
	}
	keys := map[string]bool{}
	jobs := map[string]int{}
	for _, s := range shifts {
		if keys[s.Key] {
			t.Fatal("duplicate shift", s.Key)
		}
		keys[s.Key] = true
		jobs[s.Job]++
		if s.Start.Hour() < 7 || s.End.Hour() > 20 || !s.End.After(s.Start) || s.End.Sub(s.Start) > 4*time.Hour {
			t.Fatalf("outside access: %+v", s)
		}
		if _, offset := s.Start.Zone(); offset != 9*3600 {
			t.Fatal("lost timezone")
		}
	}
	if jobs["showrunner"] != 6 || jobs["avdesk"] != 6 || jobs["merch"] != 3 || jobs["setup"] != 1 || jobs["teardown"] != 1 {
		t.Fatalf("jobs: %v", jobs)
	}
	p.Venues[0].Stages = "Main,Main"
	if _, e = Staffing(p, loc); e == nil {
		t.Fatal("accepted duplicate stages")
	}
	p.Venues[0].Stages = "Main"
	p.Venues[0].Days[0].StaffClose = "17:00"
	if _, e = Staffing(p, loc); e == nil {
		t.Fatal("accepted doors beyond staff access")
	}
}
func TestDateValidation(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	if _, e := LocalTime("2027-03-14T02:30", loc); e == nil {
		t.Fatal("accepted nonexistent DST time")
	}
	b := Basics{Tag: "tokyo27", Name: "Tokyo", Edition: "global", Timezone: "Asia/Tokyo"}
	if _, _, _, e := ValidateBasics(b); e != nil {
		t.Fatal(e)
	}
	b.Start = "2027-05-12T09:00"
	if _, _, _, e := ValidateBasics(b); e == nil {
		t.Fatal("accepted incomplete dates")
	}
	b.End = "2027-05-13T18:00"
	if _, _, _, e := ValidateBasics(b); e != nil {
		t.Fatal(e)
	}
	b.Tag = "admin"
	if _, _, _, e := ValidateBasics(b); e == nil {
		t.Fatal("accepted reserved URL")
	}
}
