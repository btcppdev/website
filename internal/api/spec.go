package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi-v1.json
var openAPIV1 []byte

func (s *server) openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(openAPIV1)
}
