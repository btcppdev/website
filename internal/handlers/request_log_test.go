package handlers

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/config"
)

func TestRequestLogSkipsStaticAssets(t *testing.T) {
	var infoLog, errorLog bytes.Buffer
	ctx := &config.AppContext{
		Infos: log.New(&infoLog, "", 0),
		Err:   log.New(&errorLog, "", 0),
	}
	handler := requestLog(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/static/css/shop.css", nil))

	if infoLog.Len() != 0 || errorLog.Len() != 0 {
		t.Fatalf("static request was logged: info=%q error=%q", infoLog.String(), errorLog.String())
	}
}

func TestRequestLogStillLogsApplicationRoutes(t *testing.T) {
	var infoLog, errorLog bytes.Buffer
	ctx := &config.AppContext{
		Infos: log.New(&infoLog, "", 0),
		Err:   log.New(&errorLog, "", 0),
	}
	handler := requestLog(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/shop", nil))

	logged := infoLog.String()
	if !strings.Contains(logged, "→ request") || !strings.Contains(logged, "← request") || !strings.Contains(logged, "path=/shop") {
		t.Fatalf("application request log missing fields: %q", logged)
	}
}

func TestRequestLogRedactsSponsorInvitationToken(t *testing.T) {
	var infoLog bytes.Buffer
	ctx := &config.AppContext{
		Infos: log.New(&infoLog, "", 0),
		Err:   log.New(&bytes.Buffer{}, "", 0),
	}
	handler := requestLog(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sponsor-invites/highly-secret-token", nil))

	logged := infoLog.String()
	if strings.Contains(logged, "highly-secret-token") || !strings.Contains(logged, "path=/sponsor-invites/[redacted]") {
		t.Fatalf("sponsor invitation token was not redacted: %q", logged)
	}
}
