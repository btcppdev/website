package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	mailer "github.com/base58btc/mailer/mail"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHackathonJudgeCalendarIntegration(t *testing.T) {
	connection := os.Getenv("JUDGE_CALENDAR_TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	c := context.Background()
	base, err := pgxpool.New(c, connection)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := "judge_calendar_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = base.Exec(c, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(c, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migrations, err := db.LoadMigrations("../../db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if _, err = pool.Exec(c, m.SQL); err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
	}
	jobs := map[string]mailer.MailRequest{}
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mailer.MailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(503)
			io.WriteString(w, `{"success":false,"code":503,"error":"test outage"}`)
			return
		}
		if _, exists := jobs[req.JobKey]; exists {
			w.WriteHeader(400)
			io.WriteString(w, `{"success":false,"code":400,"error":"UNIQUE constraint failed: scheduled.idem_key"}`)
			return
		}
		jobs[req.JobKey] = req
		io.WriteString(w, `{"success":true,"code":200}`)
	}))
	defer server.Close()
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev", MailEndpoint: server.URL, MailerSecret: "test"}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0), TemplateCache: template.Must(template.New("root").Parse(`{{define "emails/rebrand.tmpl"}}<html>{{.Content}}</html>{{end}}`))}
	var conf string
	if err = pool.QueryRow(c, `INSERT INTO conferences(tag,description,timezone) VALUES('calendar-test','Calendar test','UTC') RETURNING id::text`).Scan(&conf); err != nil {
		t.Fatal(err)
	}
	competition, err := getters.CreateCompetition(app, getters.CompetitionInput{ConferenceID: conf, Title: "Build Bitcoin"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "First Judge", Email: "first@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Second Judge", Email: "second@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []getters.CompetitionScheduleSegmentInput{}
	for _, kind := range []string{"judges-meeting", "expo", "finals", "kickoff"} {
		inputs = append(inputs, getters.CompetitionScheduleSegmentInput{SegmentType: kind, Title: kind, DefaultDurationMinutes: 30})
	}
	if err = getters.ReplaceCompetitionScheduleSegments(app, competition, inputs); err != nil {
		t.Fatal(err)
	}
	segments, err := getters.ListCompetitionScheduleSegments(app, competition)
	if err != nil {
		t.Fatal(err)
	}
	if err = getters.SetCompetitionJudgeTypes(app, competition, first, []string{getters.JudgeTypeExpo}); err != nil {
		t.Fatal(err)
	}
	if err = syncHackathonJudgeCalendars(app, competition); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatal("unscheduled sessions sent mail")
	}
	for _, s := range segments {
		p, err := getters.GetProposal(app, s.ProposalID)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if s.SegmentType == "kickoff" || s.SegmentType == "finals" {
			want = 0
		}
		if len(p.SpeakerConfRefs) != want {
			t.Fatalf("%s linked speakers=%d", s.SegmentType, len(p.SpeakerConfRefs))
		}
	}
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for i, s := range segments {
		at := start.Add(time.Duration(i) * time.Hour)
		if err = getters.UpdateConfTalkSchedule(app, s.ConfTalkID, "Main Stage", at, at.Add(30*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err = syncHackathonJudgeCalendars(app, competition); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("first judge jobs=%d", len(jobs))
	}
	if err = getters.SetCompetitionJudgeTypes(app, competition, second, []string{getters.JudgeTypeFinals}); err != nil {
		t.Fatal(err)
	}
	if err = syncHackathonJudgeCalendars(app, competition); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 4 {
		t.Fatalf("new judge did not receive stamped sessions: jobs=%d", len(jobs))
	}
	if err = syncHackathonJudgeCalendars(app, competition); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 4 {
		t.Fatal("retry duplicated invitations")
	}
	for _, job := range jobs {
		if len(job.Attachments) != 1 || !strings.Contains(job.TextBody, "hackathon session") {
			t.Fatalf("invalid invitation: %+v", job)
		}
	}
	s := segments[0]
	for _, segment := range segments {
		if segment.SegmentType == "judges-meeting" {
			s = segment
		}
	}
	if err = getters.UpdateConfTalkSchedule(app, s.ConfTalkID, "Main Stage", start.Add(10*time.Hour), start.Add(11*time.Hour)); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err = syncHackathonJudgeCalendars(app, competition); err == nil {
		t.Fatal("mail failure hidden")
	}
	fail = false
	if err = syncHackathonJudgeCalendars(app, competition); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 6 {
		t.Fatalf("reschedule retry jobs=%d", len(jobs))
	}
	if err = getters.SetCompetitionJudgeTypes(app, competition, first, []string{getters.JudgeTypeFinals}); err != nil {
		t.Fatal(err)
	}
	if err = syncHackathonJudgeCalendars(app, competition); err != nil {
		t.Fatal(err)
	}
	for _, segment := range segments {
		proposal, err := getters.GetProposal(app, segment.ProposalID)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if segment.SegmentType == "judges-meeting" || segment.SegmentType == "finals" {
			want = 2
		}
		if len(proposal.SpeakerConfRefs) != want {
			t.Fatalf("after reassignment %s attendees=%d, want %d", segment.SegmentType, len(proposal.SpeakerConfRefs), want)
		}
	}
	cancellations := 0
	for _, job := range jobs {
		if strings.Contains(job.TextBody, "has been cancelled") {
			cancellations++
		}
	}
	if cancellations != 1 {
		t.Fatalf("role change cancellations=%d", cancellations)
	}
	count := len(jobs)
	if err = syncHackathonJudgeCalendars(app, competition); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != count {
		t.Fatal("role change retry duplicated mail")
	}
	fail = true
	if err = syncHackathonJudgeCalendars(app, competition, first); err == nil {
		t.Fatal("cancellation mail failure hidden")
	}
	for _, segment := range segments {
		if segment.SegmentType != "judges-meeting" && segment.SegmentType != "finals" {
			continue
		}
		proposal, err := getters.GetProposal(app, segment.ProposalID)
		if err != nil {
			t.Fatal(err)
		}
		if len(proposal.SpeakerConfRefs) != 2 {
			t.Fatal("failed cancellation detached judge before retry")
		}
	}
	fail = false
	if err = syncHackathonJudgeCalendars(app, competition, first); err != nil {
		t.Fatal(err)
	}
	for _, segment := range segments {
		proposal, err := getters.GetProposal(app, segment.ProposalID)
		if err != nil {
			t.Fatal(err)
		}
		for _, sc := range resolveProposalSpeakers(proposal, app) {
			if sc.Speaker != nil && sc.Speaker.ID == first {
				t.Fatalf("removed judge still attached to %s", segment.SegmentType)
			}
		}
	}
	cancellations = 0
	for _, job := range jobs {
		if strings.Contains(job.TextBody, "has been cancelled") {
			cancellations++
		}
	}
	if cancellations != 3 {
		t.Fatalf("total cancellations=%d, want 3", cancellations)
	}

}
