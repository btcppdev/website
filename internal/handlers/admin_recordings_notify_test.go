package handlers

import (
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestRecordingSpeakerNotificationsIncludesEverySpeakerAndUsesCampaignOrder(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	later := now.Add(48 * time.Hour)
	sooner := now.Add(24 * time.Hour)
	rows := []*RecordingRow{
		recordingNotificationTestRow("later", "Later talk", later, []*types.Speaker{
			{ID: "speaker-a", Name: "Alice", Email: "Alice@example.com"},
			{ID: "speaker-b", Name: "Bob", Email: "bob@example.com"},
		}),
		recordingNotificationTestRow("sooner", "Sooner talk", sooner, []*types.Speaker{
			{ID: "speaker-c", Name: "Carol", Email: "carol@example.com"},
		}),
	}

	items, skipped := recordingSpeakerNotifications(rows, &types.Conf{Timezone: "UTC"}, now)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %d, want 0", len(skipped))
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if got := items[0].TalkTitle; got != "Sooner talk" {
		t.Fatalf("first talk = %q, want Sooner talk", got)
	}
	if got := items[1].Email; got != "alice@example.com" {
		t.Fatalf("normalized email = %q, want alice@example.com", got)
	}
}

func TestRecordingSpeakerNotificationsSkipsIncompleteRows(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	rows := []*RecordingRow{
		recordingNotificationTestRow("past", "Past", past, []*types.Speaker{{Name: "Alice", Email: "alice@example.com"}}),
		recordingNotificationTestRow("email", "Bad email", future, []*types.Speaker{{Name: "Bob", Email: "not-an-email"}}),
		recordingNotificationTestRow("speakers", "No speakers", future, nil),
	}
	rows = append(rows, &RecordingRow{Recording: &types.Recording{ID: "url", TalkName: "No URL", PublishAt: &future}})

	items, skipped := recordingSpeakerNotifications(rows, &types.Conf{Timezone: "UTC"}, now)
	if len(items) != 0 {
		t.Fatalf("items = %d, want 0", len(items))
	}
	if len(skipped) != 4 {
		t.Fatalf("skipped = %d, want 4", len(skipped))
	}
	reasons := make([]string, 0, len(skipped))
	for _, item := range skipped {
		reasons = append(reasons, item.Reason)
	}
	joined := strings.Join(reasons, "|")
	for _, want := range []string{"publication time has already passed", "speaker is missing a valid email", "no speakers are attached", "missing a valid YouTube URL"} {
		if !strings.Contains(joined, want) {
			t.Errorf("skip reasons %q do not contain %q", joined, want)
		}
	}
}

func TestRecordingSpeakerNotificationJobKeyStaysStableWhenScheduleChanges(t *testing.T) {
	publishAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	item := &RecordingSpeakerNotification{
		Row:       &RecordingRow{Recording: &types.Recording{ID: "recording-1"}},
		Email:     "speaker@example.com",
		PublishAt: publishAt,
	}
	first := recordingSpeakerNotificationJobKey(item)
	if first != recordingSpeakerNotificationJobKey(item) {
		t.Fatal("job key is not deterministic")
	}
	item.PublishAt = publishAt.Add(time.Hour)
	if first != recordingSpeakerNotificationJobKey(item) {
		t.Fatal("reminder job key changed after rescheduling; the existing mailer job should be updated")
	}
	item.JobKeySuffix = "-devtest-1"
	if got := recordingSpeakerNotificationJobKey(item); got == first || !strings.HasSuffix(got, item.JobKeySuffix) {
		t.Fatalf("test reminder job key = %q, want a distinct key ending in %q", got, item.JobKeySuffix)
	}
}

func TestRecordingNotificationYouTubeURLNormalizesVideoID(t *testing.T) {
	if got := recordingNotificationYouTubeURL("abc123"); got != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("URL = %q", got)
	}
	if got := recordingNotificationYouTubeURL("https://youtu.be/abc123"); got != "https://youtu.be/abc123" {
		t.Fatalf("URL = %q", got)
	}
}

func TestBuildRecordingSpeakerCampaignOrdersDigestByRoomAndAgenda(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	conf := &types.Conf{Tag: "test", Desc: "Test Event", Timezone: "UTC"}
	speaker := &types.Speaker{ID: "speaker", Name: "Speaker", Email: "speaker@example.com"}
	workshop := recordingNotificationTestRow("workshop", "Workshop", now.Add(72*time.Hour), []*types.Speaker{speaker})
	workshop.ConfTalk.Venue = "three"
	workshop.ConfTalk.Sched.Start = now
	main := recordingNotificationTestRow("main", "Main", now.Add(48*time.Hour), []*types.Speaker{speaker})
	main.ConfTalk.Venue = "one"
	main.ConfTalk.Sched.Start = now.Add(2 * time.Hour)
	talks := recordingNotificationTestRow("talks", "Talks", now.Add(60*time.Hour), []*types.Speaker{speaker})
	talks.ConfTalk.Venue = "two"
	talks.ConfTalk.Sched.Start = now.Add(time.Hour)

	campaign := buildRecordingSpeakerCampaign([]*RecordingRow{workshop, talks, main}, conf, now, "https://btcpp.dev")
	if len(campaign.Talks) != 3 || len(campaign.Recipients) != 1 || len(campaign.Reminders) != 3 {
		t.Fatalf("campaign counts talks=%d recipients=%d reminders=%d", len(campaign.Talks), len(campaign.Recipients), len(campaign.Reminders))
	}
	want := []string{"Main", "Talks", "Workshop"}
	for i, title := range want {
		if got := campaign.Talks[i].TalkTitle; got != title {
			t.Fatalf("talk %d = %q, want %q", i, got, title)
		}
	}
	if got, wantAt := campaign.Reminders[0].ReminderAt, campaign.Reminders[0].PublishAt.Add(-24*time.Hour); !got.Equal(wantAt) {
		t.Fatalf("reminder = %s, want %s", got, wantAt)
	}
	if got, wantAt := campaign.Talks[0].BufferAt, campaign.Talks[0].PublishAt.Add(5*time.Minute); !got.Equal(wantAt) {
		t.Fatalf("Buffer time = %s, want %s", got, wantAt)
	}
	if got := campaign.Talks[0].PublishUTCLabel; !strings.HasSuffix(got, " UTC") {
		t.Fatalf("UTC publication label = %q", got)
	}
	if got := campaign.Talks[0].SpeakerNames; got != "Speaker" {
		t.Fatalf("speaker names = %q, want Speaker", got)
	}
}

func TestMergeRecordingDigestRecipientsAddsStaffAndDeduplicatesSpeakers(t *testing.T) {
	campaign := &recordingSpeakerCampaign{Recipients: []*RecordingSpeakerDigestRecipient{
		{Speaker: &types.Speaker{Name: "Speaker"}, Email: "speaker@example.com"},
	}}
	mergeRecordingDigestRecipients(campaign, []*types.Speaker{
		{Name: "Duplicate", Email: "SPEAKER@example.com"},
		{Name: "Admin", Email: "admin@example.com"},
		{Name: "No email"},
	})
	if len(campaign.Recipients) != 2 {
		t.Fatalf("recipients = %d, want 2", len(campaign.Recipients))
	}
	var emails []string
	for _, recipient := range campaign.Recipients {
		emails = append(emails, recipient.Email)
	}
	if strings.Join(emails, ",") != "admin@example.com,speaker@example.com" {
		t.Fatalf("recipient emails = %v", emails)
	}
}

func TestRecordingBufferXTextIncludesYouTubeAndFitsXLimit(t *testing.T) {
	talk := &RecordingSpeakerCampaignTalk{
		TalkTitle:     strings.Repeat("Long title ", 40),
		SpeakerCredit: "Ada (@ada)",
		YouTubeURL:    "https://youtu.be/example",
	}
	got := recordingBufferXText(talk)
	if !strings.Contains(got, talk.YouTubeURL) {
		t.Fatalf("post does not include YouTube URL: %q", got)
	}
	if len([]rune(got)) > 280 {
		t.Fatalf("post has %d runes, want <= 280", len([]rune(got)))
	}
}

func TestSocialPostBlocksBufferSchedule(t *testing.T) {
	if socialPostBlocksBufferSchedule(nil) {
		t.Fatal("nil post blocked Buffer scheduling")
	}
	if !socialPostBlocksBufferSchedule(&types.SocialPost{Status: recordingStatusScheduled}) {
		t.Fatal("scheduled browser-X post did not block Buffer scheduling")
	}
	if !socialPostBlocksBufferSchedule(&types.SocialPost{URL: "https://x.com/btcplusplus/status/1"}) {
		t.Fatal("published X URL did not block Buffer scheduling")
	}
	if socialPostBlocksBufferSchedule(&types.SocialPost{Status: recordingStatusFailed}) {
		t.Fatal("failed X post should allow Buffer scheduling")
	}
}

func recordingNotificationTestRow(id, title string, publishAt time.Time, speakers []*types.Speaker) *RecordingRow {
	return &RecordingRow{
		Recording: &types.Recording{
			ID:        id,
			TalkName:  title,
			YTLink:    "https://www.youtube.com/watch?v=" + id,
			PublishAt: &publishAt,
		},
		YTURL: "https://www.youtube.com/watch?v=" + id,
		ConfTalk: &types.ConfTalk{
			ID:    "talk-" + id,
			Venue: "one",
			Sched: &types.Times{Start: publishAt},
		},
		Speakers: speakers,
	}
}
