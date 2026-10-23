package xstudio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBroadcastAnalytics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "test-cookie" {
			t.Error("missing authentication")
		}
		switch r.URL.Path {
		case "/api/live/get-broadcasts":
			w.Write([]byte(`{"value":{"rows":[{"broadcastId":"public","title":"A talk","startTime":1234,"totalWatched":9999},{"broadcastId":"private","isLocked":true,"startTime":1234},{"broadcastId":"scheduled","startTime":null}],"cursor":null,"canReadAnalytics":true},"error":null}`))
		case "/api/live/get-broadcast-view-counts":
			w.Write([]byte(`{"value":{"public":0},"error":null}`))
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := New(Config{Cookie: "test-cookie", IngestID: "unused", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.BroadcastMetrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal("locked media leaked")
	}
	count, err := client.BroadcastViews(context.Background(), "public")
	if err != nil || count == nil || *count != 0 {
		t.Fatal("zero must be a valid count", err)
	}
	if _, err = client.BroadcastViews(context.Background(), "missing"); err == nil {
		t.Fatal("missing count treated as zero")
	}
}
