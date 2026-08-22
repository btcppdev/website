package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi-v1.json
var openAPIV1 []byte

func (s *server) openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1")
	// This URL is intentionally stable while the v1 contract evolves through
	// backward-compatible additions. Revalidate it so documentation clients do
	// not pair a newly deployed renderer with an older cached contract.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(openAPIV1)
}
