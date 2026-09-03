package types

import (
	"testing"
	"time"
)

func TestNormalizeConferenceAccentColor(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
		valid bool
	}{
		{input: "", want: DefaultConferenceAccentColor, valid: true},
		{input: " #A1B2C3 ", want: "#a1b2c3", valid: true},
		{input: "orange", want: DefaultConferenceAccentColor, valid: false},
		{input: "#abcd", want: DefaultConferenceAccentColor, valid: false},
	} {
		got, valid := NormalizeConferenceAccentColor(test.input)
		if got != test.want || valid != test.valid {
			t.Errorf("NormalizeConferenceAccentColor(%q) = (%q, %t), want (%q, %t)", test.input, got, valid, test.want, test.valid)
		}
	}
}

func TestConfLocUsesKnownConferenceTimezoneFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		conf     *Conf
		wantZone string
	}{
		{
			name: "blank timezone",
			conf: &Conf{
				Tag:       "nairobi",
				StartDate: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC),
			},
			wantZone: "Africa/Nairobi",
		},
		{
			name: "utc timezone",
			conf: &Conf{
				Tag:       "nairobi",
				Timezone:  "UTC",
				TZ:        time.UTC,
				StartDate: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC),
			},
			wantZone: "Africa/Nairobi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conf.Loc().String(); got != tc.wantZone {
				t.Fatalf("Loc() = %q, want %q", got, tc.wantZone)
			}
		})
	}
}

func TestTalkAnchorTagUsesClipartBasename(t *testing.T) {
	for _, tc := range []struct {
		name    string
		clipart string
		want    string
	}{
		{
			name:    "plain filename",
			clipart: "package-relay.png",
			want:    "package-relay",
		},
		{
			name:    "local fixture path",
			clipart: "../static/img/floripa26/leading.png",
			want:    "leading",
		},
		{
			name:    "empty",
			clipart: "",
			want:    "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (&Talk{Clipart: tc.clipart}).AnchorTag(); got != tc.want {
				t.Fatalf("Talk.AnchorTag() = %q, want %q", got, tc.want)
			}
			if got := (&ConfTalk{Clipart: tc.clipart}).AnchorTag(); got != tc.want {
				t.Fatalf("ConfTalk.AnchorTag() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfEventDayCount(t *testing.T) {
	loc := time.FixedZone("event", -5*60*60)
	for _, tc := range []struct {
		name string
		conf *Conf
		want int
	}{
		{
			name: "inclusive date span",
			conf: &Conf{
				StartDate: time.Date(2026, 6, 17, 9, 0, 0, 0, loc),
				EndDate:   time.Date(2026, 6, 20, 18, 0, 0, 0, loc),
			},
			want: 4,
		},
		{
			name: "same day",
			conf: &Conf{
				StartDate: time.Date(2026, 6, 17, 9, 0, 0, 0, loc),
				EndDate:   time.Date(2026, 6, 17, 18, 0, 0, 0, loc),
			},
			want: 1,
		},
		{
			name: "missing end date",
			conf: &Conf{
				StartDate: time.Date(2026, 6, 17, 9, 0, 0, 0, loc),
			},
			want: 1,
		},
		{
			name: "end before start",
			conf: &Conf{
				StartDate: time.Date(2026, 6, 17, 9, 0, 0, 0, loc),
				EndDate:   time.Date(2026, 6, 16, 18, 0, 0, 0, loc),
			},
			want: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conf.EventDayCount(); got != tc.want {
				t.Fatalf("EventDayCount() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestConfDisplayGracePeriodsStartAfterFinalLocalDay(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("load Toronto timezone: %s", err)
	}
	conf := &Conf{
		PublicationStatus: "published",
		Timezone:          "America/Toronto",
		TZ:                loc,
		StartDate:         time.Date(2026, time.July, 22, 0, 0, 0, 0, loc),
		// Conference date fields may be stored at the beginning of the day.
		EndDate: time.Date(2026, time.July, 24, 0, 0, 0, 0, loc),
	}

	finalDayEnd := time.Date(2026, time.July, 25, 0, 0, 0, 0, loc)
	if got := conf.FinalDayEndsAt(); !got.Equal(finalDayEnd) {
		t.Fatalf("FinalDayEndsAt() = %s, want %s", got, finalDayEnd)
	}
	if !conf.IsInActiveEventListAt(time.Date(2026, time.July, 26, 11, 59, 0, 0, loc)) {
		t.Fatal("conference left public active list before 36-hour grace elapsed")
	}
	if conf.IsInActiveEventListAt(time.Date(2026, time.July, 26, 12, 0, 0, 0, loc)) {
		t.Fatal("conference remained in public active list after 36-hour grace elapsed")
	}
	if conf.DashboardHasEndedAt(time.Date(2026, time.July, 31, 23, 59, 0, 0, loc)) {
		t.Fatal("conference left dashboard current events before seven-day grace elapsed")
	}
	if !conf.DashboardHasEndedAt(time.Date(2026, time.August, 1, 0, 0, 0, 0, loc)) {
		t.Fatal("conference remained in dashboard current events after seven-day grace elapsed")
	}
}

func TestConfArchiveTitleParts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		conf        *Conf
		wantTitle   string
		wantEdition string
	}{
		{
			name: "standard bitcoin plus plus description",
			conf: &Conf{
				Desc: "bitcoin++ Austin 2024, bitcoin script edition",
			},
			wantTitle:   "Austin",
			wantEdition: "bitcoin script edition",
		},
		{
			name: "future description without year",
			conf: &Conf{
				Desc: "bitcoin++ Berlin, payments edition",
			},
			wantTitle:   "Berlin",
			wantEdition: "payments edition",
		},
		{
			name: "local edition description",
			conf: &Conf{
				Desc: "bitcoin++ local edition, Durham NC",
			},
			wantTitle:   "Durham NC",
			wantEdition: "local edition",
		},
		{
			name: "fallback to location",
			conf: &Conf{
				Location: "Madeira, Portugal",
			},
			wantTitle: "Madeira, Portugal",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conf.ArchiveTitle(); got != tc.wantTitle {
				t.Fatalf("ArchiveTitle() = %q, want %q", got, tc.wantTitle)
			}
			if got := tc.conf.ArchiveEdition(); got != tc.wantEdition {
				t.Fatalf("ArchiveEdition() = %q, want %q", got, tc.wantEdition)
			}
		})
	}
}
