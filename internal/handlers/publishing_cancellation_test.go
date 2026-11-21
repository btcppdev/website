package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/publishing"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublishingCancellationAuthorization(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	admin, member, conf := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO people(id,name) VALUES($1,'Publishing admin'),($2,'Publishing member')`, admin, member); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM people WHERE id IN ($1,$2)`, admin, member)
	if _, err = pool.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO conferences(id,tag) VALUES($1,$2)`, conf, "cancel-"+conf); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM conferences WHERE id=$1`, conf)
	var id string
	if err = pool.QueryRow(ctx, `WITH p AS(INSERT INTO proposals(conference_id,title) VALUES($1,'Cancel fixture') RETURNING id),t AS(INSERT INTO conf_talks(conference_id,proposal_id) SELECT $1,id FROM p RETURNING id) INSERT INTO recordings(conf_talk_id,talk_name,youtube_url,publish_at,duration_seconds) SELECT id,'Cancel fixture','https://youtu.be/abcdefghijk',$2,3600 FROM t RETURNING id::text`, conf, time.Now().Add(24*time.Hour)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	items, err := (publishing.Store{DB: pool}).RecordingItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected publishing.RecordingItem
	for _, d := range items {
		if d.ID == "youtube:"+id {
			selected = d
		}
	}
	body, _ := json.Marshal(map[string]string{"action": "recording_cancel", "id": selected.ID, "revision": selected.Revision})
	request := func(person, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/admin/publishing/action", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		session, err := app.Session.Load(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithContext(session)
		if person != "" {
			if err = auth.LoginPerson(app, r, person, auth.MethodPassword); err != nil {
				t.Fatal(err)
			}
		}
		app.Session.Put(session, authMethodsCSRFKey, "expected-csrf")
		w := httptest.NewRecorder()
		AdminPublishing(w, r, app)
		return w
	}
	for _, tc := range []struct {
		person, csrf string
		status       int
	}{{"", "expected-csrf", 303}, {member, "expected-csrf", 303}, {admin, "wrong", 403}, {admin, "expected-csrf", 200}, {admin, "expected-csrf", 400}} {
		w := request(tc.person, tc.csrf)
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
		}
	}
	var state string
	var target *time.Time
	if err = pool.QueryRow(ctx, `SELECT state,target_at FROM recording_schedule_changes WHERE recording_id=$1 AND channel_id='youtube'`, id).Scan(&state, &target); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || target != nil {
		t.Fatalf("incorrect cancellation: %s %v", state, target)
	}
}
