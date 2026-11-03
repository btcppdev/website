package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExactScheduleTimes(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	// UTC date differs from the event date: edits must keep the event date.
	current := time.Date(2026, 11, 3, 16, 0, 0, 0, time.UTC)
	for _, minute := range []int{12*60 + 15, 12*60 + 20} {
		start, end, err := exactScheduleTimes(current, loc, &minute, 5)
		if err != nil {
			t.Fatal(err)
		}
		if start.Day() != 4 || start.Hour()*60+start.Minute() != minute || end.Sub(start) != 5*time.Minute {
			t.Fatalf("unexpected slot: %s–%s", start, end)
		}
	}
	start, end, err := exactScheduleTimes(current, loc, nil, 5)
	if err != nil || !start.Equal(current) || end.Sub(start) != 5*time.Minute {
		t.Fatal("resize changed start", start, end, err)
	}
	for _, tc := range []struct{ start, duration int }{{-1, 5}, {1440, 5}, {1439, 5}, {735, 0}, {735, -5}, {735, 1441}} {
		if _, _, err := exactScheduleTimes(current, loc, &tc.start, tc.duration); err == nil {
			t.Fatalf("accepted invalid slot: %+v", tc)
		}
	}
}

func TestExactScheduleTimeEditorTemplate(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{Env: &types.EnvConfig{}, Err: log.New(os.Stderr, "", 0)}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/schedule.tmpl", &AdminSchedulePage{Conf: &types.Conf{Tag: "seoul"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `id="schedule-edit-time"`) {
		t.Fatal("unexpected top-level time editor")
	}
	for _, want := range []string{`id="schedule-time-talk"`, `id="schedule-time-start"`, `step="60"`, `id="schedule-time-duration"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing editor field %s", want)
		}
	}
}
