package handlers

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"btcpp-web/internal/types"
)

func TestRecordingBroadcastTitleCredits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		speakers []*types.Speaker
		want     string
	}{
		{"handle", []*types.Speaker{{Name: "Nick Farrow", Twitter: types.Twitter{Handle: "utxoclub"}}}, "FROSTY Workshop — @utxoclub | bitcoin++ Seoul"},
		{"name fallback", []*types.Speaker{{Name: "Nick Farrow"}}, "FROSTY Workshop — Nick Farrow | bitcoin++ Seoul"},
		{"panel", []*types.Speaker{{Name: "Nick", Twitter: types.Twitter{Handle: "@utxoclub"}}, {Name: "Ada"}, {Name: "Nick duplicate", Twitter: types.Twitter{Handle: "UTXOCLUB"}}, nil}, "FROSTY Workshop — @utxoclub, Ada | bitcoin++ Seoul"},
		{"profile URL", []*types.Speaker{{Twitter: types.Twitter{Handle: "https://x.com/utxoclub"}}}, "FROSTY Workshop — @utxoclub | bitcoin++ Seoul"},
		{"invalid handle", []*types.Speaker{{Name: "Nick", Twitter: types.Twitter{Handle: "bad handle"}}}, "FROSTY Workshop — Nick | bitcoin++ Seoul"},
		{"deleted profile", []*types.Speaker{{Name: "Deleted", Twitter: types.Twitter{Handle: "deleted"}, IsDeletedAccount: true}}, "FROSTY Workshop | bitcoin++ Seoul"},
		{"no speakers", nil, "FROSTY Workshop | bitcoin++ Seoul"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := &RecordingRow{
				Recording: &types.Recording{TalkName: "Old title"},
				ConfTalk:  &types.ConfTalk{Proposal: &types.Proposal{Title: " FROSTY\nWorkshop "}, Conf: &types.Conf{Desc: "bitcoin++ Seoul, privacy edition"}},
				Speakers:  tc.speakers,
			}
			if got := recordingBroadcastTitle(row); got != tc.want {
				t.Fatalf("title=%q; want %q", got, tc.want)
			}
		})
	}
}

func TestRecordingBroadcastTitleMissingMetadata(t *testing.T) {
	for _, tc := range []struct {
		row  *RecordingRow
		want string
	}{
		{nil, "Bitcoin++ broadcast"},
		{&RecordingRow{}, "Bitcoin++ broadcast"},
		{&RecordingRow{Recording: &types.Recording{TalkName: "Recorded session"}}, "Recorded session"},
		{&RecordingRow{Recording: &types.Recording{TalkName: "Recorded session"}, ConfTalk: &types.ConfTalk{Conf: &types.Conf{Location: "Berlin, Germany"}}}, "Recorded session | bitcoin++ Berlin"},
		{&RecordingRow{ConfTalk: &types.ConfTalk{Proposal: &types.Proposal{Title: "Panel"}, Conf: &types.Conf{Desc: "Seoul, privacy edition"}}}, "Panel | bitcoin++ Seoul"},
	} {
		if got := recordingBroadcastTitle(tc.row); got != tc.want {
			t.Fatalf("title=%q; want %q", got, tc.want)
		}
	}
}

func TestRecordingBroadcastTitleKeepsWholeHandlesAndUnicode(t *testing.T) {
	row := &RecordingRow{
		Recording: &types.Recording{TalkName: strings.Repeat("비트코인 🔐 ", 40)},
		ConfTalk:  &types.ConfTalk{Conf: &types.Conf{Desc: "bitcoin++ Seoul, privacy edition"}},
		Speakers:  []*types.Speaker{{Twitter: types.Twitter{Handle: "utxoclub"}}},
	}
	got := recordingBroadcastTitle(row)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) > recordingBroadcastTitleLimit || !strings.HasSuffix(got, "… — @utxoclub") {
		t.Fatalf("unsafe long title: %q", got)
	}
	row.Speakers = nil
	for i := 0; i < 15; i++ {
		row.Speakers = append(row.Speakers, &types.Speaker{Twitter: types.Twitter{Handle: fmt.Sprintf("speaker%08d", i)}})
	}
	got = recordingBroadcastTitle(row)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) > recordingBroadcastTitleLimit || !strings.Contains(got, " more") {
		t.Fatalf("unsafe panel title: %q", got)
	}
	credits := strings.SplitN(got, " — ", 2)
	if len(credits) != 2 {
		t.Fatalf("missing panel credits: %q", got)
	}
	for _, part := range strings.Split(strings.SplitN(credits[1], " +", 2)[0], ", ") {
		if len(part) != 16 || !strings.HasPrefix(part, "@speaker") {
			t.Fatalf("partial handle: %q", part)
		}
	}
}
