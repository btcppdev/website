package handlers

import (
	"context"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

func TestInsiderContributorDirectoryRequiresServiceToken(t *testing.T) {
	for _, header := range []string{"", "Bearer wrong", "Basic secret"} {
		request := httptest.NewRequest("GET", "/api/internal/insider/contributors", nil)
		request.Header.Set("Authorization", header)
		response := httptest.NewRecorder()
		ListInsiderContributors(response, request, &config.AppContext{Env: &types.EnvConfig{InsiderServiceToken: "secret"}})
		if response.Code != 401 || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("unauthorized directory response: %d", response.Code)
		}
	}
}

func TestInsiderReporterRoleMigration(t *testing.T) {
	database := os.Getenv("ROLE_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("ROLE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	_, err = connection.Exec(ctx, `CREATE TEMP TABLE people_roles (person_id text, scope text, position text, PRIMARY KEY(person_id,scope,position));
INSERT INTO people_roles VALUES ('one','insider','writer'),('one','global','staff'),('two','insider','writer'),('two','insider','reporter'),('three','insider','contributor');`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../db/migrations/153_insider_reporter_role.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := connection.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	var roles []string
	if err := connection.QueryRow(ctx, `SELECT array_agg(person_id||':'||scope||'-'||position ORDER BY person_id,scope,position) FROM people_roles`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	want := []string{"one:global-staff", "one:insider-reporter", "three:insider-contributor", "two:insider-reporter"}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles after rename: %#v", roles)
	}
}
