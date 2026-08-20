package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultPageSize = 50
	maximumPageSize = 100
)

func writePublicCollection[T any](server *server, w http.ResponseWriter, r *http.Request, values []T) {
	page, next, limit, err := paginate(r, values)
	if err != nil {
		server.writeError(w, r, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	server.writePublicWithMeta(w, r, http.StatusOK, page, responseMeta{NextCursor: next, Limit: limit})
}

func paginate[T any](r *http.Request, values []T) ([]T, string, int, error) {
	limit := defaultPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximumPageSize {
			return nil, "", 0, fmt.Errorf("limit must be between 1 and %d", maximumPageSize)
		}
		limit = parsed
	}
	offset := 0
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		parts := strings.Split(string(decoded), ":")
		if err != nil || len(parts) != 2 || parts[0] != "v1" {
			return nil, "", 0, fmt.Errorf("cursor is invalid")
		}
		offset, err = strconv.Atoi(parts[1])
		if err != nil || offset < 0 || offset > len(values) {
			return nil, "", 0, fmt.Errorf("cursor is invalid")
		}
	}
	end := offset + limit
	if end > len(values) {
		end = len(values)
	}
	page := values[offset:end]
	next := ""
	if end < len(values) {
		next = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("v1:%d", end)))
	}
	return page, next, limit, nil
}
