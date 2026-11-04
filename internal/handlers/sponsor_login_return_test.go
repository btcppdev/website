package handlers

import (
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func TestDatabaseSmokeSponsorPendingEmailRedirect(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated postgres")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, pending := range []bool{false, true} {
		app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0)}
		target := "/dashboard/sponsor/" + uuid.NewString() + "?tab=challenges"
		req := httptest.NewRequest("GET", target, nil)
		q, err := app.Session.Load(req.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		req = req.WithContext(q)
		if pending {
			if err := auth.LoginEmail(app, req, uuid.NewString()+"@example.test"); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		id, _, ok := sponsorDashboardIdentity(w, req, app)
		if ok || id != nil || w.Code != 303 {
			t.Fatalf("unexpected access: %v %v %d", id, ok, w.Code)
		}
		dest, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		want := "/login"
		if pending {
			want = "/dashboard/profile"
		}
		if dest.Path != want || dest.Query().Get("next") != target {
			t.Fatalf("pending=%v redirect=%s", pending, dest)
		}
	}
}
