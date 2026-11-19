package handlers

import (
	"btcpp-web/internal/types"
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"
)

func TestRecordingPublishColumnShowsPendingAndConfirmedDates(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/admin/recordings.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 11, 19, 16, 5, 0, 0, time.UTC)
	next := old.Add(24 * time.Hour)
	for _, tc := range []struct {
		state     string
		requested *time.Time
		want      string
	}{
		{"pending", &next, "Waiting to sync"}, {"syncing", &next, "Confirming with YouTube"}, {"needs_review", &next, "Needs review"}, {"failed", &next, "Schedule change failed"}, {"pending", nil, "Remove publication schedule requested"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			var out bytes.Buffer
			row := RecordingRow{Recording: &types.Recording{PublishAt: &old}, YTRequestedAt: tc.requested, YTScheduleState: tc.state, YTScheduleError: "<script>bad</script>"}
			if err := tmpl.ExecuteTemplate(&out, "recording_publish_status", row); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			if !strings.Contains(text, tc.want) || !strings.Contains(text, "Previously confirmed:") || strings.Contains(text, "<script>") {
				t.Fatal(text)
			}
			if tc.requested != nil && !strings.Contains(text, "Nov 20, 2026") {
				t.Fatal("requested date missing:", text)
			}
		})
	}
}
