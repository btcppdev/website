package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseATX23(t *testing.T) {
	talks, err := parseATX23("../../static/atx23/talks.html")
	if err != nil {
		t.Fatalf("parseATX23: %v", err)
	}
	if got, want := len(talks), 39; got != want {
		t.Fatalf("talk count: got %d want %d", got, want)
	}
	recordings := 0
	for _, talk := range talks {
		if talk.Title == "" {
			t.Fatalf("talk %d has empty title", talk.Index)
		}
		if talk.Speaker.Name == "" {
			t.Fatalf("talk %s has empty speaker name", talk.Title)
		}
		if talk.YouTubeURL != "" {
			recordings++
		}
	}
	if got, want := recordings, 30; got != want {
		t.Fatalf("recording count: got %d want %d", got, want)
	}
}

func TestInferATX22Speaker(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		desc      string
		wantTitle string
		wantName  string
		wantTw    string
	}{
		{
			name:      "title speaker",
			title:     "Packaging Your Favorite Open Source Project for Umbrel and Embassy w/ Dread",
			wantTitle: "Packaging Your Favorite Open Source Project for Umbrel and Embassy",
			wantName:  "Dread",
		},
		{
			name:      "paren twitter",
			title:     "Becoming a Lightning Network Sensei",
			desc:      "John Cantrell (https://twitter.com/JohnCantrell97) presents on his lightning node project, Sensei",
			wantTitle: "Becoming a Lightning Network Sensei",
			wantName:  "John Cantrell",
			wantTw:    "JohnCantrell97",
		},
		{
			name:      "avoid short duplicate",
			title:     "Lightning Liquidity Deep Dive w/ Lisa Neigut aka niftynei",
			desc:      "Lisa: https://twitter.com/niftynei",
			wantTitle: "Lightning Liquidity Deep Dive",
			wantName:  "Lisa Neigut aka niftynei",
			wantTw:    "niftynei",
		},
		{
			name:      "skip twitter label",
			title:     "Enterprise Lightning Engineering at CashApp w/ Ryan Loomba",
			desc:      "Twitter: https://twitter.com/RLoombs",
			wantTitle: "Enterprise Lightning Engineering at CashApp",
			wantName:  "Ryan Loomba",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotSpeaker := inferATX22Speaker(tt.title, tt.desc)
			if gotTitle != tt.wantTitle {
				t.Fatalf("title: got %q want %q", gotTitle, tt.wantTitle)
			}
			if gotSpeaker.Name != tt.wantName {
				t.Fatalf("speaker: got %q want %q", gotSpeaker.Name, tt.wantName)
			}
			if gotSpeaker.Twitter != tt.wantTw {
				t.Fatalf("twitter: got %q want %q", gotSpeaker.Twitter, tt.wantTw)
			}
		})
	}
}

func TestFirstSchedEventURL(t *testing.T) {
	desc := `Lisa: https://twitter.com/niftynei
https://btcpp2022.sched.com/event/12P7O/lightning-liquidity-deep-dive`
	got := firstSchedEventURL(desc)
	want := "https://btcpp2022.sched.com/event/12P7O/lightning-liquidity-deep-dive"
	if got != want {
		t.Fatalf("sched URL: got %q want %q", got, want)
	}
}

func TestParseSchedDate(t *testing.T) {
	got, err := parseSchedDate("2022-06-08T10:45:00-0500")
	if err != nil {
		t.Fatalf("parseSchedDate: %v", err)
	}
	if got.Format("2006-01-02T15:04:05-0700") != "2022-06-08T10:45:00-0500" {
		t.Fatalf("parsed wrong date: %s", got.Format("2006-01-02T15:04:05-0700"))
	}
}

func TestEnrichFromSchedEventHTML(t *testing.T) {
	html := `<script type="application/ld+json">{"@context":"https://schema.org","@type":"Event","name":"lightning liquidity deep dive","startDate":"2022-06-08T10:45:00-0500","endDate":"2022-06-08T11:20:00-0500","location":{"@type":"Place","address":{"@type":"PostalAddress","streetAddress":"Austin, TX, USA"}}}</script>`
	ev, err := parseSchemaEvent(html)
	if err != nil {
		t.Fatalf("parseSchemaEvent: %v", err)
	}
	start, err := parseSchedDate(ev.StartDate)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	end, err := parseSchedDate(ev.EndDate)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if got, want := int(end.Sub(start).Minutes()), 35; got != want {
		t.Fatalf("duration: got %d want %d", got, want)
	}
	if got, want := schedLocationName(ev.Location), "Austin, TX, USA"; got != want {
		t.Fatalf("venue: got %q want %q", got, want)
	}
}

func TestParseReviewedCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.csv")
	csv := `event,talk_index,talk_anchor,proposal_id,conf_talk_id,speaker_person_id,speaker_name,speaker_email,speaker_company,speaker_twitter,speaker_github_url,speaker_website_url,speaker_nostr,speaker_photo_source,talk_title,talk_description,talk_category,talk_type,venue,scheduled_start,scheduled_end,duration_min,clipart_source,youtube_url
atx22,4,voEbAeS_B5w,proposal-id,conf-talk-id,person-id,Lisa Neigut,nifty@example.com,Base58,niftynei,,,,/tmp/photo.png,Lightning Liquidity Deep Dive,desc,YouTube Playlist,talk,"Austin, TX, USA",2022-06-08T10:45:00-05:00,2022-06-08T11:20:00-05:00,35,/tmp/clip.png,https://www.youtube.com/watch?v=voEbAeS_B5w
`
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	talks, err := parseReviewedCSV(path)
	if err != nil {
		t.Fatalf("parseReviewedCSV: %v", err)
	}
	if got, want := len(talks), 1; got != want {
		t.Fatalf("talk count: got %d want %d", got, want)
	}
	talk := talks[0]
	if talk.ProposalID != "proposal-id" || talk.ConfTalkID != "conf-talk-id" || talk.Speaker.PersonID != "person-id" {
		t.Fatalf("ids not preserved: %#v", talk)
	}
	if talk.Speaker.Email != "nifty@example.com" {
		t.Fatalf("email: got %q", talk.Speaker.Email)
	}
	if got, want := talk.DurationMin, 35; got != want {
		t.Fatalf("duration: got %d want %d", got, want)
	}
	if talk.Start.IsZero() || talk.End.IsZero() {
		t.Fatalf("expected parsed schedule: %#v", talk)
	}
}

func TestSplitSpeakerEmails(t *testing.T) {
	got := splitSpeakerEmails(" Alice@example.com, bob@example.com, alice@example.com, ")
	want := []string{"alice@example.com", "bob@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitSpeakerEmails = %#v, want %#v", got, want)
	}
}

func TestATX23Sponsors(t *testing.T) {
	if got, want := len(atx23Sponsors), 14; got != want {
		t.Fatalf("atx23Sponsors len = %d, want %d", got, want)
	}
	for _, sponsor := range atx23Sponsors {
		if sponsor.Name == "" || sponsor.Website == "" || sponsor.Logo == "" {
			t.Fatalf("incomplete sponsor: %#v", sponsor)
		}
	}
}
