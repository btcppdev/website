package main

import "testing"

func TestBypassSessionMiddleware(t *testing.T) {
	for _, path := range []string{"/live/status", "/static/css/site.css", "/dev26/run-of-show/events"} {
		if !bypassSessionMiddleware(path) {
			t.Errorf("bypassSessionMiddleware(%q) = false", path)
		}
	}
	for _, path := range []string{"/", "/dashboard", "/watch/recording-id/status", "/live/status/extra"} {
		if bypassSessionMiddleware(path) {
			t.Errorf("bypassSessionMiddleware(%q) = true", path)
		}
	}
}
