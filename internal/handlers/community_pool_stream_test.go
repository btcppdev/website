package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/prizepool"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommunitySnapshotSharedAndInvalidated(t *testing.T) {
	h := &communityStreamHub{viewers: make(map[chan struct{}]struct{}), snapshots: make(map[string]string)}
	changed := make(chan struct{}, 1)
	h.viewers[changed] = struct{}{}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.snapshot("event", func() (string, error) { calls.Add(1); return "funding", nil })
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("25 viewers caused %d loads", calls.Load())
	}
	h.invalidate()
	h.invalidate()
	if len(changed) != 1 {
		t.Fatal("notifications not coalesced")
	}
	_, _ = h.snapshot("event", func() (string, error) { calls.Add(1); h.invalidate(); return "old", nil })
	v, _ := h.snapshot("event", func() (string, error) { calls.Add(1); return "new", nil })
	if v != "new" || calls.Load() != 3 {
		t.Fatal("stale snapshot cached across invalidation")
	}
}
func TestCommunityStreamPushAndReconnect(t *testing.T) {
	changes := make(chan struct{}, 1)
	var total atomic.Int64
	total.Store(100)
	var failed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveCommunityStream(w, r, changes, func() (string, error) {
			if failed.Load() {
				return "", errors.New("listener disconnected")
			}
			b, _ := json.Marshal(communityStatusData(&prizepool.Pool{TotalMSat: total.Load() * 1000, Status: "open"}))
			return string(b), nil
		})
	}))
	defer server.Close()
	open := func() (*http.Response, *bufio.Scanner) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		r, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatal("not SSE")
		}
		return response, bufio.NewScanner(response.Body)
	}
	read := func(scanner *bufio.Scanner) (string, string) {
		event, data := "", ""
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				return event, data
			}
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		t.Fatalf("stream ended: %v", scanner.Err())
		return "", ""
	}
	response, scanner := open()
	event, data := read(scanner)
	if event != "funding" || !strings.Contains(data, `"sats":"100"`) {
		t.Fatal(event, data)
	}
	total.Store(200)
	changes <- struct{}{}
	event, data = read(scanner)
	if event != "funding" || !strings.Contains(data, `"sats":"200"`) {
		t.Fatal(event, data)
	}
	response.Body.Close()
	total.Store(300)
	response, scanner = open()
	event, data = read(scanner)
	if !strings.Contains(data, `"sats":"300"`) {
		t.Fatal("reconnect did not catch up", data)
	}
	failed.Store(true)
	changes <- struct{}{}
	event, _ = read(scanner)
	if event != "unavailable" {
		t.Fatal("did not enable fallback", event)
	}
	response.Body.Close()
}
func TestCommunityStreamRejectsPrivateAndOmitsReceipts(t *testing.T) {
	w := httptest.NewRecorder()
	serveCommunityStream(w, httptest.NewRequest("GET", "/", nil), make(chan struct{}), func() (string, error) { return "", errCommunityUnavailable })
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	p := &prizepool.Pool{Description: "private", NodeID: "secret-node", Offer: "offer", Status: "open"}
	b, _ := json.Marshal(communityStatusData(p))
	for _, private := range []string{"private", "secret-node", "offer", "payment_hash", "payer_note"} {
		if strings.Contains(string(b), private) {
			t.Fatal("private data in stream", string(b))
		}
	}
}

func TestCommunityListenerReconnectAndCleanup(t *testing.T) {
	connectionURL := os.Getenv("PRIZE_TEST_DATABASE_URL")
	if connectionURL == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	cfg, err := pgxpool.ParseConfig(connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	name := "community-sse-" + uuid.NewString()
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	db, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := &config.AppContext{DB: db}
	hub, changes, stop := subscribeCommunityResults(app)
	defer stop()
	other, _, stopOther := subscribeCommunityResults(app)
	if other != hub {
		t.Fatal("viewers do not share listener")
	}
	stopOther()
	select {
	case <-hub.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not start")
	}
	// Identify only this test's dedicated listener; never terminate unrelated connections.
	var pid int32
	if err = db.QueryRow(context.Background(), `SELECT pid FROM pg_stat_activity WHERE application_name=$1 AND query='LISTEN btcpp_community_pools'`, name).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	default:
	}
	if _, err = db.Exec(context.Background(), `SELECT pg_notify('btcpp_community_pools','')`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("notification not delivered")
	}
	if _, err = db.Exec(context.Background(), `SELECT pg_terminate_backend($1)`, pid); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		hub.mu.Lock()
		connected := hub.connected
		hub.mu.Unlock()
		if !connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener failure not detected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		hub.mu.Lock()
		connected := hub.connected
		hub.mu.Unlock()
		if connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener did not reconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	communityStreams.Lock()
	_, exists := communityStreams.hubs[db]
	communityStreams.Unlock()
	if exists {
		t.Fatal("idle listener retained")
	}
}
