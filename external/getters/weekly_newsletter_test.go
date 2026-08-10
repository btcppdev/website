package getters

import (
	"testing"
	"time"
)

func TestWeeklyNewsletterTalkCandidateRanking(t *testing.T) {
	base := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	ptr := func(value time.Time) *time.Time { return &value }
	baseline := weeklyNewsletterTalkCandidate{
		Talk:           WeeklyNewsletterTalk{TalkID: "baseline", PublishAt: base},
		TalkType:       "talk",
		Venue:          "side stage",
		ScheduledStart: ptr(base),
		ConfEnd:        ptr(base),
		Recent:         true,
	}

	clone := func(id string) weeklyNewsletterTalkCandidate {
		candidate := baseline
		candidate.Talk.TalkID = id
		return candidate
	}
	tests := []struct {
		name string
		a    weeklyNewsletterTalkCandidate
		b    weeklyNewsletterTalkCandidate
	}{
		{
			name: "recent candidate beats fallback panel",
			a:    baseline,
			b: weeklyNewsletterTalkCandidate{
				Talk: WeeklyNewsletterTalk{TalkID: "old-panel", PublishAt: base.Add(-30 * 24 * time.Hour)}, TalkType: "panel", Venue: "main stage",
			},
		},
		{
			name: "new conference avoids consecutive repetition within the same programming tier",
			a:    baseline,
			b: func() weeklyNewsletterTalkCandidate {
				candidate := clone("same-conf-talk")
				candidate.SamePreviousConf = true
				return candidate
			}(),
		},
		{
			name: "panel beats main-stage talk",
			a: func() weeklyNewsletterTalkCandidate {
				candidate := clone("panel")
				candidate.TalkType = "panel"
				return candidate
			}(),
			b: func() weeklyNewsletterTalkCandidate {
				candidate := clone("main-talk")
				candidate.Venue = "main stage"
				return candidate
			}(),
		},
		{
			name: "main stage beats side stage within type",
			a: func() weeklyNewsletterTalkCandidate {
				candidate := clone("main")
				candidate.Venue = "one"
				return candidate
			}(),
			b: baseline,
		},
		{
			name: "final day beats earlier day",
			a:    clone("final-day"),
			b: func() weeklyNewsletterTalkCandidate {
				candidate := clone("earlier-day")
				candidate.ScheduledStart = ptr(base.Add(-24 * time.Hour))
				return candidate
			}(),
		},
		{
			name: "later final-day slot wins",
			a: func() weeklyNewsletterTalkCandidate {
				candidate := clone("closing")
				candidate.ScheduledStart = ptr(base.Add(4 * time.Hour))
				return candidate
			}(),
			b: baseline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !weeklyNewsletterTalkCandidateLess(tt.a, tt.b) {
				t.Fatalf("candidate %q did not rank ahead of %q", tt.a.Talk.TalkID, tt.b.Talk.TalkID)
			}
		})
	}
}

func TestWeeklyNewsletterFinalDayUsesConferenceTimezone(t *testing.T) {
	start := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC) // Aug 13 in Chicago
	end := time.Date(2026, time.August, 14, 4, 0, 0, 0, time.UTC)   // Aug 13 in Chicago
	candidate := weeklyNewsletterTalkCandidate{
		ConfTimezone:   "America/Chicago",
		ScheduledStart: &start,
		ConfEnd:        &end,
	}
	if !weeklyNewsletterFinalDay(candidate) {
		t.Fatal("same local conference day was not recognized as the final day")
	}
}

func TestOrganizeWeeklyNewsletterTalksCreatesBroadcastsForMoreThanThree(t *testing.T) {
	published := []WeeklyNewsletterTalk{{TalkID: "one"}, {TalkID: "two"}, {TalkID: "three"}, {TalkID: "four"}}
	featured := &WeeklyNewsletterTalk{TalkID: "one"}
	talks, broadcasts := organizeWeeklyNewsletterTalks(published, featured)
	if len(talks) != 0 || len(broadcasts) != 3 {
		t.Fatalf("talks=%d broadcasts=%d, want 0 and 3", len(talks), len(broadcasts))
	}
	if broadcasts[0].TalkID != "two" || broadcasts[1].TalkID != "three" || broadcasts[2].TalkID != "four" {
		t.Fatalf("broadcasts=%#v, want the three most recently published talks", broadcasts)
	}
}

func TestOrganizeWeeklyNewsletterTalksOmitsFeaturedFromSmallUpdateList(t *testing.T) {
	published := []WeeklyNewsletterTalk{{TalkID: "one"}, {TalkID: "two"}, {TalkID: "three"}}
	talks, broadcasts := organizeWeeklyNewsletterTalks(published, &WeeklyNewsletterTalk{TalkID: "one"})
	if len(talks) != 2 || talks[0].TalkID != "two" || len(broadcasts) != 0 {
		t.Fatalf("talks=%#v broadcasts=%#v", talks, broadcasts)
	}
}

func TestWeeklyNewsletterSpeakerProfileURLPreference(t *testing.T) {
	if got := weeklyNewsletterSpeakerProfileURL("@alice", "npub1alice", "https://alice.example"); got != "https://x.com/alice" {
		t.Fatalf("X profile = %q", got)
	}
	if got := weeklyNewsletterSpeakerProfileURL("", "npub1alice", "https://alice.example"); got != "https://njump.me/npub1alice" {
		t.Fatalf("Nostr profile = %q", got)
	}
	if got := weeklyNewsletterSpeakerProfileURL("", "", "https://alice.example"); got != "https://alice.example" {
		t.Fatalf("website profile = %q", got)
	}
}
