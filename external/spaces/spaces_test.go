package spaces

import (
	"net/http"
	"testing"
)

func TestStreamingHTTPClientDoesNotTimeOutResponseBody(t *testing.T) {
	client := newStreamingHTTPClient()
	if client.Timeout != 0 {
		t.Fatalf("streaming client timeout = %s, want no whole-request timeout", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("streaming transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != spacesRequestTimeout {
		t.Fatalf("response header timeout = %s, want %s", transport.ResponseHeaderTimeout, spacesRequestTimeout)
	}
}
