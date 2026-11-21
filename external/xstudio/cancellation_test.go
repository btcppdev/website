package xstudio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCancelScheduled(t *testing.T) {
	for _, tc := range []struct {
		name, before, after, deletion string
		wantError                     bool
		writes                        int
	}{
		{"confirmed", `{"rows":[{"broadcastId":"target","kind":"scheduled","stateName":"NotStarted"}],"scheduledComplete":true}`, `{"rows":[],"scheduledComplete":true}`, `{"value":null}`, false, 1},
		{"already absent", `{"rows":[],"scheduledComplete":true}`, "", "", false, 0},
		{"live", `{"rows":[{"broadcastId":"target","kind":"live","stateName":"Running"}],"scheduledComplete":true}`, "", "", true, 0},
		{"ended", `{"rows":[{"broadcastId":"target","kind":"ended","stateName":"Ended"}],"scheduledComplete":true}`, "", "", true, 0},
		{"incomplete", `{"rows":[],"scheduledComplete":false}`, "", "", true, 0},
		{"malformed", `{"scheduledComplete":true}`, "", "", true, 0},
		{"rejected", `{"rows":[{"broadcastId":"target","kind":"scheduled","stateName":"NotStarted"}],"scheduledComplete":true}`, "", `{"error":"unauthorized"}`, true, 1},
		{"not confirmed", `{"rows":[{"broadcastId":"target","kind":"scheduled","stateName":"NotStarted"}],"scheduledComplete":true}`, `{"rows":[],"scheduledComplete":false}`, `{"value":null}`, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes, reads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "test-cookie" || r.Header.Get("X-Session-ID") != "session" || r.URL.Query().Get("rwebShell") != "1" {
					t.Error("missing request context")
				}
				switch r.Method + " " + r.URL.Path {
				case "GET /api/live/get-broadcasts":
					value := tc.before
					if reads > 0 {
						value = tc.after
					}
					reads++
					fmt.Fprintf(w, `{"value":%s}`, value)
				case "POST /api/live/delete-scheduled-broadcast":
					writes++
					var payload map[string]string
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload) != 1 || payload["broadcastId"] != "target" {
						t.Errorf("wrong deletion payload: %v %v", payload, err)
					}
					fmt.Fprint(w, tc.deletion)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c, err := New(Config{Cookie: "test-cookie", IngestID: "ingest", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			err = c.CancelScheduled(context.Background(), "session", "target")
			if (err != nil) != tc.wantError || writes != tc.writes {
				t.Fatalf("error=%v writes=%d", err, writes)
			}
		})
	}
}
