package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedSignerEmbeddingRestrictedToBadgeStudio(t *testing.T) {
	for _, tt := range []struct{ configured, ancestor string }{
		{"https://badges.btcpp.dev/", "https://badges.btcpp.dev"},
		{"http://localhost:4173", "http://localhost:4173"},
		{"", "'none'"}, {"https://*.btcpp.dev", "'none'"},
		{"http://badges.btcpp.dev", "'none'"}, {"https://user@badges.btcpp.dev", "'none'"},
		{"https://badges.btcpp.dev;evil", "'none'"},
	} {
		t.Run(tt.configured, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx := &config.AppContext{Env: &types.EnvConfig{BadgeStudioURL: tt.configured, SignerURL: "https://bunker.btcpp.dev"}}
			setManagedSignerHeaders(response, ctx)
			if !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors "+tt.ancestor+";") {
				t.Fatal(response.Header())
			}
			want := ""
			if tt.ancestor == "'none'" {
				want = "DENY"
			}
			if response.Header().Get("X-Frame-Options") != want {
				t.Fatal(response.Header())
			}
		})
	}
}
