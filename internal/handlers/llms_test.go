package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLLMSTextServesConciseMachineReadableIndex(t *testing.T) {
	t.Chdir(findRepoRoot(t))

	response := httptest.NewRecorder()
	LLMSText(response, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	body := response.Body.String()
	for _, expected := range []string{
		"# bitcoin++",
		"> bitcoin++ organizes technical Bitcoin conferences",
		"https://btcpp.dev/developers/api",
		"https://btcpp.dev/api/v1/openapi.json",
		"https://btcpp.dev/.well-known/oauth-authorization-server",
		"Never place bearer credentials in URLs.",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("llms.txt omitted %q", expected)
		}
	}
}
