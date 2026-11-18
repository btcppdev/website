package xapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostAmbiguityAndPayload(t *testing.T) {
	for _, status := range []int{201, 400, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/2/tweets" || r.Header.Get("Authorization") != "Bearer private-test-token" {
					t.Error("wrong request")
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["text"] != "hello" || body["reply"] == nil || body["media"] == nil {
					t.Error(body)
				}
				w.WriteHeader(status)
				if status == 201 {
					w.Write([]byte(`{"data":{"id":"123"}}`))
				} else {
					w.Write([]byte(`{"error":"private-test-token"}`))
				}
			}))
			defer srv.Close()
			id, e := (Client{HTTP: srv.Client(), BaseURL: srv.URL}).Post(context.Background(), "private-test-token", "hello", "parent", []string{"image"})
			if status == 201 {
				if e != nil || id != "123" {
					t.Fatal(id, e)
				}
				return
			}
			var api *APIError
			if !errors.As(e, &api) || api.Ambiguous != (status >= 500) || strings.Contains(e.Error(), "private-test-token") {
				t.Fatalf("unsafe error %v", e)
			}
		})
	}
}
func TestSuccessWithoutIDIsAmbiguous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":{}}`)) }))
	defer srv.Close()
	_, e := (Client{HTTP: srv.Client(), BaseURL: srv.URL}).Post(context.Background(), "test", "hello", "", nil)
	var api *APIError
	if !errors.As(e, &api) || !api.Ambiguous {
		t.Fatal(e)
	}
}
