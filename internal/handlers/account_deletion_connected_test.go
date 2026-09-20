package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Opt-in rehearsal with a real Alerts executable and synthetic local databases.
// The central worker is invoked directly so retry clocks need not take minutes.
func TestAccountDeletionConnectedAlertsPostgres(t *testing.T) {
	binary := os.Getenv("ALERTS_TEST_BINARY")
	if binary == "" || os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("ALERTS_TEST_BINARY and BTCPP_POSTGRES_SMOKE=1 required")
	}
	database := os.Getenv("DATABASE_URL")
	u, err := url.Parse(database)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || !strings.HasPrefix(u.Path, "/commerce_account_deletion_") {
		t.Fatal("use a dedicated loopback commerce_account_deletion_ database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool}
	person, admin := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO people(id,name) VALUES($1,'Deletion rehearsal'),($2,'Deletion rehearsal admin')`, person, admin); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM people WHERE id=$1 OR id=$2`, person, admin)
	if _, err := pool.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, admin); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	sqlite := filepath.Join(tmp, "alerts.db")
	token := strings.Repeat("local-deletion-test-", 3)
	// Never inherit real OAuth, billing, Telegram, GitHub or mail credentials.
	env := []string{"PATH=" + os.Getenv("PATH"), "BTCPP_ACCOUNT_EVENTS_TOKEN=" + token, "HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=localhost,127.0.0.1,::1"}
	seed := exec.Command(binary, "-database", sqlite, "-seed-only")
	seed.Env = env
	if output, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed Alerts: %v %s", err, output)
	}
	sql := func(statement string) string {
		t.Helper()
		cmd := exec.Command("sqlite3", sqlite, statement)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("SQLite: %v %s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	sql(fmt.Sprintf(`UPDATE sources SET next_check_at=4102444800;
 INSERT INTO users(id,name,created_at) VALUES('%s','Deletion rehearsal',1);
 INSERT INTO destinations(id,user_id,channel,address,verified_at,unsubscribe_hash) VALUES(900001,'%s','email','deletion-rehearsal@example.test',1,'delete-test');
 INSERT INTO sessions VALUES('deletion-session','%s','csrf',4102444800);
 INSERT INTO mailer_test_messages(message_id,destination_id,recipient,subject,body,unsubscribe_url,created_at,next_attempt_at) VALUES('deletion-rehearsal-message',900001,'deletion-rehearsal@example.test','Test','Test','https://example.test',4102444800,4102444800);`, person, person, person))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	origin := "http://" + address
	start := func() func() {
		t.Helper()
		logFile, err := os.CreateTemp(tmp, "alerts-process-*.log")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "-database", sqlite, "-listen", address, "-public-url", origin, "-frontend", tmp)
		cmd.Env = env
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				_ = cmd.Process.Kill()
				<-done
				logFile.Close()
			}
		}
		t.Cleanup(stop)
		client := &http.Client{Timeout: time.Second}
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			response, err := client.Get(origin + "/api/health")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					return stop
				}
			}
			select {
			case err := <-done:
				stopped = true
				logFile.Close()
				t.Fatalf("Alerts exited before health: %v", err)
			default:
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("Alerts health timed out")
		return stop
	}
	stop := start()
	var bodies []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if len(bodies) == 1 {
			w.WriteHeader(503)
			return
		}
		forward, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, origin+"/api/internal/accounts/deleted", bytes.NewReader(body))
		forward.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(forward)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 204 {
			t.Errorf("Alerts rejected deletion: %d", response.StatusCode)
		}
		if len(bodies) == 2 {
			w.WriteHeader(503)
			return
		} // lost acknowledgment after commit
		w.WriteHeader(response.StatusCode)
	}))
	defer proxy.Close()
	if err := getters.DeletePerson(app, person, admin); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Second)
	for index, offset := range []time.Duration{0, 2 * time.Minute, 5 * time.Minute} {
		worked, err := processAccountDeletionDelivery(ctx, app, person, "alerts", proxy.URL, token, now.Add(offset))
		if !worked || (index < 2 && err == nil) || (index == 2 && err != nil) {
			t.Fatal("central delivery", index, worked, err)
		}
		if index == 0 && sql(`SELECT count(*) FROM users`) != "1" {
			t.Fatal("outage erased Alerts account")
		}
		if index == 1 {
			if sql(`SELECT count(*) FROM users`) != "0" || sql(`SELECT count(*) FROM sessions`) != "0" {
				t.Fatal("receiver did not erase account/session")
			}
			stop()
			stop = start()
		}
	}
	if len(bodies) != 3 || bodies[0] != bodies[1] || bodies[1] != bodies[2] {
		t.Fatal("event changed across outage/restart")
	}
	if sql(`SELECT count(*) FROM mailer_suppressions WHERE suppression_key='alerts:deletion-rehearsal-message'`) != "1" {
		t.Fatal("remote cancellation intent lost")
	}
	if sql(`SELECT count(*) FROM deleted_accounts`) != "1" {
		t.Fatal("deletion marker lost on restart")
	}
	if worked, err := processAccountDeletionDelivery(ctx, app, person, "alerts", proxy.URL, token, now.Add(time.Hour)); err != nil || worked {
		t.Fatal("accepted deletion delivered again", worked, err)
	}
}
