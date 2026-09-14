package handlers

import (
	"bufio"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestJudgingSnapshotSharedAndInvalidated(t *testing.T) {
	h := &judgingStreamHub{viewers: make(map[chan struct{}]struct{}), snapshots: make(map[string]string)}
	changes := make(chan struct{}, 1)
	h.viewers[changes] = struct{}{}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := h.snapshot("round", func() (string, error) { calls.Add(1); return "standings", nil })
			if err != nil || value != "standings" {
				t.Errorf("snapshot: %q %v", value, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("25 viewers caused %d result loads", calls.Load())
	}
	h.invalidate()
	h.invalidate() // Slow consumers receive one wakeup and always fetch latest state.
	if len(changes) != 1 {
		t.Fatal("notifications did not coalesce")
	}
	_, _ = h.snapshot("round", func() (string, error) { calls.Add(1); return "new standings", nil })
	if calls.Load() != 2 {
		t.Fatal("new results were not loaded after invalidation")
	}
}

func TestJudgingSnapshotDoesNotCacheAcrossInvalidation(t *testing.T) {
	h := &judgingStreamHub{viewers: make(map[chan struct{}]struct{}), snapshots: make(map[string]string)}
	_, err := h.snapshot("round", func() (string, error) { h.invalidate(); return "old", nil })
	if err != nil {
		t.Fatal(err)
	}
	value, err := h.snapshot("round", func() (string, error) { return "new", nil })
	if err != nil || value != "new" {
		t.Fatalf("cached stale result: %q %v", value, err)
	}
}

// Requires an explicitly enabled disposable migrated PostgreSQL database.
// JUDGING_BROWSER_PREVIEW exposes only this synthetic fixture on localhost.
func TestJudgingSSEFlow(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires disposable migrated PostgreSQL")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wd, _ := os.Getwd()
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	if err = loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	c := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) { t.Helper(); _, err := pool.Exec(c, q, args...); must(err) }
	id := func(q string, args ...any) string {
		t.Helper()
		var value string
		must(pool.QueryRow(c, q, args...).Scan(&value))
		return value
	}
	tag := "judging-" + uuid.NewString()
	confID := id(`INSERT INTO conferences(tag,active,description,date_desc,start_date,end_date,timezone,location,venue) VALUES($1,true,'Judging preview','September 2026',now(),now()+interval '2 days','Europe/Berlin','Berlin','Test venue') RETURNING id::text`, tag)
	judgeID := id(`INSERT INTO people(name) VALUES('Preview judge') RETURNING id::text`)
	exec(`INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, judgeID, tag+"@example.test")
	competitionID, err := getters.CreateCompetition(app, getters.CompetitionInput{ConferenceID: confID, Title: "Bitcoin builders", Visibility: getters.CompetitionVisibilityPublic, JudgingMode: getters.CompetitionJudgingModeManual})
	must(err)
	must(getters.ReplaceCompetitionScheduleSegments(app, competitionID, []getters.CompetitionScheduleSegmentInput{{SegmentType: getters.JudgeTypeExpo, Title: "Expo", DefaultDurationMinutes: 60}}))
	events, err := getters.ListJudgeEvents(app, competitionID)
	must(err)
	if len(events) != 1 {
		t.Fatalf("events: %v", events)
	}
	eventID := events[0].ID
	exec(`UPDATE judge_events SET state='open' WHERE id=$1`, eventID)
	exec(`INSERT INTO competition_judges(competition_id,person_id,judge_type) VALUES($1,$2,'expo')`, competitionID, judgeID)
	projectID, err := getters.CreateProject(app, getters.ProjectInput{CompetitionID: competitionID, CreatedByPersonID: judgeID, Slug: "lightning-garden", Title: "Lightning garden", ShortDescription: "A payment tool for community gardens"})
	must(err)
	exec(`UPDATE projects SET status='submitted' WHERE id=$1`, projectID)
	secondOwner := id(`INSERT INTO people(name) VALUES('Second builder') RETURNING id::text`)
	project2, err := getters.CreateProject(app, getters.ProjectInput{CompetitionID: competitionID, CreatedByPersonID: secondOwner, Slug: "open-source-wallets", Title: "Open source wallets", ShortDescription: "A small wallet for local communities"})
	must(err)
	exec(`UPDATE projects SET status='submitted' WHERE id=$1`, project2)
	router := mux.NewRouter()
	router.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, judgeID, auth.MethodPassword); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/"+tag+"/hackathon/judging", 303)
	})
	router.HandleFunc("/preview/close", func(w http.ResponseWriter, r *http.Request) {
		exec(`UPDATE judge_events SET state='closed' WHERE id=$1`, eventID)
		http.Redirect(w, r, "/"+tag+"/hackathon/judging?view=results", 303)
	})
	router.HandleFunc("/{conf}/hackathon/judging", func(w http.ResponseWriter, r *http.Request) { HackathonJudging(w, r, app) })
	router.HandleFunc("/{conf}/hackathon/judging/submitted", func(w http.ResponseWriter, r *http.Request) { HackathonBallotSubmitted(w, r, app) })
	router.HandleFunc("/{conf}/hackathon/judging/scorecards", func(w http.ResponseWriter, r *http.Request) { HackathonScorecardSubmit(w, r, app) }).Methods("POST")
	router.HandleFunc("/{conf}/hackathon/judging/results/live", func(w http.ResponseWriter, r *http.Request) { HackathonJudgingLiveResults(w, r, app) })
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	if os.Getenv("JUDGING_BROWSER_PREVIEW") == "1" {
		t.Log("Preview: http://127.0.0.1:8094/preview")
		t.Fatal(http.ListenAndServe("127.0.0.1:8094", SessionMiddleware(app, router)))
	}
	server := httptest.NewServer(SessionMiddleware(app, router))
	defer server.Close()
	streamURL := server.URL + "/" + tag + "/hackathon/judging/results/live?judge_event=" + eventID
	response, err := http.Get(streamURL)
	must(err)
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("anonymous: %d", response.StatusCode)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	response, err = client.Get(server.URL + "/preview")
	must(err)
	response.Body.Close()
	response, err = client.Get(streamURL)
	must(err)
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("open results: %d", response.StatusCode)
	}
	exec(`UPDATE judge_events SET state='closed' WHERE id=$1`, eventID)
	response, err = client.Get(streamURL)
	must(err)
	defer response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %v", response.StatusCode, response.Header)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	waitFor := func(want string) {
		t.Helper()
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), want) {
				return
			}
		}
		t.Fatalf("missing %q: %v", want, scanner.Err())
	}
	waitFor("Lightning garden")
	// Writes use another pool/connection, like a second application instance.
	writer, err := pgxpool.New(c, os.Getenv("DATABASE_URL"))
	must(err)
	defer writer.Close()
	_, err = writer.Exec(c, `UPDATE projects SET title='Updated garden' WHERE id=$1`, projectID)
	must(err)
	waitFor("Updated garden")
	_, err = writer.Exec(c, `DELETE FROM competition_judges WHERE person_id=$1 AND competition_id=$2`, judgeID, competitionID)
	must(err)
	waitFor("event: revoked")
}

func TestJudgingSessionMiddlewarePreservesNormalRequests(t *testing.T) {
	session := scs.New()
	app := &config.AppContext{Session: session}
	handler := SessionMiddleware(app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/normal" {
			session.Put(r.Context(), "test", "saved")
			io.WriteString(w, "normal")
			return
		}
		if _, ok := w.(http.Flusher); !ok {
			t.Error("stream writer lost Flush")
		}
		io.WriteString(w, session.GetString(r.Context(), "test"))
	}))
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	response, err := client.Get(server.URL + "/normal")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	response, err = client.Get(server.URL + "/test/hackathon/judging/results/live")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "saved" {
		t.Fatalf("session missing: %q %v", body, err)
	}
}
