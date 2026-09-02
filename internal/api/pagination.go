package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func pageLimit(r *http.Request) (int, error) {
	limit := defaultPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximumPageSize {
			return 0, fmt.Errorf("limit must be between 1 and %d", maximumPageSize)
		}
		limit = parsed
	}
	return limit, nil
}

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
	limit, err := pageLimit(r)
	if err != nil {
		return nil, "", 0, err
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

func decodeKeysetCursor(raw string) (time.Time, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cursor is invalid")
	}
	parts := strings.SplitN(string(decoded), "|", 3)
	if len(parts) != 3 || parts[0] != "v1" || strings.TrimSpace(parts[2]) == "" {
		return time.Time{}, "", fmt.Errorf("cursor is invalid")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cursor is invalid")
	}
	return updatedAt.UTC(), parts[2], nil
}

func encodeKeysetCursor(updatedAt time.Time, sourceID string) string {
	if updatedAt.IsZero() || strings.TrimSpace(sourceID) == "" {
		return ""
	}
	value := "v1|" + updatedAt.UTC().Format(time.RFC3339Nano) + "|" + sourceID
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
