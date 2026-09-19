package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountDeletionTransport(t *testing.T) {
	const person = "11111111-1111-4111-8111-111111111111"
	token := strings.Repeat("s", 32)
	var bodies []string
	status := 503
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		var event map[string]any
		if json.Unmarshal(body, &event) != nil || len(event) != 3 || event["schema_version"] != float64(1) || event["person_id"] != person || event["event_id"] != "account-deleted:"+person || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Idempotency-Key") != event["event_id"] || r.Method != http.MethodPost {
			t.Error("invalid deletion contract")
		}
		w.WriteHeader(status)
	}))
	defer receiver.Close()
	for _, code := range []int{503, 202, 200, 204} {
		status = code
		err := postAccountDeletion(context.Background(), receiver.URL, token, person)
		if (err == nil) != (code == 204) {
			t.Fatal(code, err)
		}
	}
	for _, body := range bodies {
		if body != bodies[0] {
			t.Fatal("retry changed event")
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, receiver.URL, 307) }))
	defer redirect.Close()
	if err := postAccountDeletion(context.Background(), redirect.URL, token, person); err == nil || len(bodies) != 4 {
		t.Fatal("redirect followed", err, len(bodies))
	}
	for _, endpoint := range []string{"http://example.com/delete", "https://user:secret@example.com/delete", "https://example.com/delete?token=secret", "https://example.com/delete#fragment"} {
		if validAccountDeletionEndpoint(endpoint, token) {
			t.Fatal("unsafe endpoint accepted", endpoint)
		}
	}
}
