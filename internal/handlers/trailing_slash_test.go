package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectTrailingSlash(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	tests := []struct {
		name     string
		method   string
		target   string
		code     int
		location string
	}{
		{
			name:     "get redirects and preserves query",
			method:   http.MethodGet,
			target:   "/hackathons/?next=1",
			code:     http.StatusPermanentRedirect,
			location: "/hackathons?next=1",
		},
		{
			name:     "head redirects",
			method:   http.MethodHead,
			target:   "/hackathons/",
			code:     http.StatusPermanentRedirect,
			location: "/hackathons",
		},
		{
			name:   "root is unchanged",
			method: http.MethodGet,
			target: "/",
			code:   http.StatusNoContent,
		},
		{
			name:   "post is unchanged",
			method: http.MethodPost,
			target: "/hackathons/",
			code:   http.StatusNoContent,
		},
		{
			name:   "static directory slash is unchanged",
			method: http.MethodGet,
			target: "/static/atx23/",
			code:   http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			rec := httptest.NewRecorder()

			redirectTrailingSlash(next).ServeHTTP(rec, req)

			if rec.Code != tt.code {
				t.Fatalf("status = %d, want %d", rec.Code, tt.code)
			}
			if got := rec.Header().Get("Location"); got != tt.location {
				t.Fatalf("Location = %q, want %q", got, tt.location)
			}
		})
	}
}
