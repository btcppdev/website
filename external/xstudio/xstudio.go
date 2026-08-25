// Package xstudio implements the private browser endpoints currently used by
// X Studio to create a scheduled broadcast and attach its poster. These are
// unsupported endpoints and must remain isolated behind this package.
package xstudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://studio.x.com"
	MaxPosterSize  = 10 << 20
	maxResponse    = 1 << 20
)

type Config struct {
	Cookie     string
	UserAgent  string
	IngestID   string
	BaseURL    string
	HTTPClient *http.Client
}

type Client struct {
	config Config
}

type RateLimit struct {
	Limit     int
	Remaining int
	ResetAt   time.Time
}

type CreateInput struct {
	Title               string
	StartsAt            time.Time
	SessionID           string
	OptimisticPosterURL string
	Locked              bool
	HighLatency         bool
	ChatOption          int
}

type Created struct {
	ScheduledBroadcastID string
	BroadcastID          string
	RateLimit            RateLimit
}

type UploadedPoster struct {
	MediaID   string
	RateLimit RateLimit
}

type FinalizeInput struct {
	ScheduledBroadcastID string
	BroadcastID          string
	PosterMediaID        string
	StartsAt             time.Time
	SessionID            string
}

type Finalized struct {
	ScheduledBroadcastID string
	BroadcastID          string
	PosterURL            string
	ChatOption           int
	RateLimit            RateLimit
}

type HTTPError struct {
	StatusCode int
	Detail     string
	RateLimit  RateLimit
}

func (e *HTTPError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("X Studio returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("X Studio returned HTTP %d: %s", e.StatusCode, e.Detail)
}

type envelope[T any] struct {
	Value T               `json:"value"`
	Error json.RawMessage `json:"error"`
}

func New(config Config) (*Client, error) {
	config.Cookie = strings.TrimSpace(config.Cookie)
	config.IngestID = strings.TrimSpace(config.IngestID)
	if config.Cookie == "" || config.IngestID == "" {
		return nil, errors.New("X Studio cookie and ingest ID are required")
	}
	if config.BaseURL == "" {
		config.BaseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("valid X Studio base URL is required")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{config: config}, nil
}

func (c *Client) Create(ctx context.Context, input CreateInput) (*Created, error) {
	if strings.TrimSpace(input.Title) == "" || input.StartsAt.IsZero() || strings.TrimSpace(input.SessionID) == "" {
		return nil, errors.New("title, start time, and session ID are required")
	}
	payload := struct {
		AutoStart           bool   `json:"autoStart"`
		ChatOption          int    `json:"chatOption"`
		IngestID            string `json:"ingestId"`
		HighLatency         bool   `json:"isHighLatency"`
		Locked              bool   `json:"isLocked"`
		OptimisticPosterURL string `json:"optimisticPosterUrl"`
		StartTimeUTC        int64  `json:"startTimeUtc"`
		Title               string `json:"title"`
	}{true, input.ChatOption, c.config.IngestID, input.HighLatency, input.Locked, input.OptimisticPosterURL, input.StartsAt.UnixMilli(), input.Title}
	var response envelope[struct {
		ScheduledBroadcastID string `json:"scheduledBroadcastId"`
		BroadcastID          string `json:"broadcastId"`
	}]
	meta, err := c.doJSON(ctx, http.MethodPost, "/api/live/create-scheduled-broadcast?rwebShell=1", input.SessionID, payload, &response)
	if err != nil {
		return nil, err
	}
	if err := checkEnvelope(response.Error); err != nil {
		return nil, err
	}
	if response.Value.ScheduledBroadcastID == "" || response.Value.BroadcastID == "" {
		return nil, errors.New("X Studio create response omitted broadcast IDs")
	}
	return &Created{response.Value.ScheduledBroadcastID, response.Value.BroadcastID, meta}, nil
}

func (c *Client) UploadPoster(ctx context.Context, sessionID, filename, contentType string, reader io.Reader) (*UploadedPoster, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(filename) == "" || reader == nil {
		return nil, errors.New("session ID, poster filename, and reader are required")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuoted(filepath.Base(filename))))
	if strings.TrimSpace(contentType) == "" {
		contentType = mime.TypeByExtension(filepath.Ext(filename))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("create poster form: %w", err)
	}
	written, err := io.Copy(part, io.LimitReader(reader, MaxPosterSize+1))
	if err != nil {
		return nil, fmt.Errorf("read poster: %w", err)
	}
	if written > MaxPosterSize {
		return nil, errors.New("poster exceeds 10 MiB limit")
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish poster form: %w", err)
	}
	var response envelope[string]
	meta, err := c.do(ctx, http.MethodPost, "/api/live/upload-poster-image?rwebShell=1", sessionID, writer.FormDataContentType(), &body, &response)
	if err != nil {
		return nil, err
	}
	if err := checkEnvelope(response.Error); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.Value) == "" {
		return nil, errors.New("X Studio poster response omitted media ID")
	}
	return &UploadedPoster{MediaID: response.Value, RateLimit: meta}, nil
}

func (c *Client) Finalize(ctx context.Context, input FinalizeInput) (*Finalized, error) {
	if input.ScheduledBroadcastID == "" || input.BroadcastID == "" || input.PosterMediaID == "" || input.StartsAt.IsZero() || input.SessionID == "" {
		return nil, errors.New("broadcast IDs, poster ID, start time, and session ID are required")
	}
	payload := struct {
		BroadcastID          string `json:"broadcastId"`
		PosterMediaID        string `json:"preLiveSlateMediaId"`
		ScheduledBroadcastID string `json:"scheduledBroadcastId"`
		StartTimeUTC         int64  `json:"startTimeUtc"`
	}{input.BroadcastID, input.PosterMediaID, input.ScheduledBroadcastID, input.StartsAt.UnixMilli()}
	var response envelope[struct {
		ScheduledBroadcastID string `json:"scheduledBroadcastId"`
		BroadcastID          string `json:"broadcastId"`
		PosterURL            string `json:"preLiveSlateUrl"`
		ChatOption           int    `json:"chatOption"`
	}]
	meta, err := c.doJSON(ctx, http.MethodPost, "/api/live/update-scheduled-broadcast?rwebShell=1", input.SessionID, payload, &response)
	if err != nil {
		return nil, err
	}
	if err := checkEnvelope(response.Error); err != nil {
		return nil, err
	}
	if response.Value.BroadcastID != input.BroadcastID || response.Value.ScheduledBroadcastID != input.ScheduledBroadcastID || response.Value.PosterURL == "" {
		return nil, errors.New("X Studio finalize response did not match the requested broadcast")
	}
	return &Finalized{response.Value.ScheduledBroadcastID, response.Value.BroadcastID, response.Value.PosterURL, response.Value.ChatOption, meta}, nil
}

func (c *Client) doJSON(ctx context.Context, method, path, sessionID string, input, output any) (RateLimit, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return RateLimit{}, err
	}
	return c.do(ctx, method, path, sessionID, "application/json", bytes.NewReader(body), output)
}

func (c *Client) do(ctx context.Context, method, path, sessionID, contentType string, body io.Reader, output any) (RateLimit, error) {
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.config.BaseURL, "/")+path, body)
	if err != nil {
		return RateLimit{}, err
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("Cookie", c.config.Cookie)
	request.Header.Set("Origin", DefaultBaseURL)
	request.Header.Set("Pragma", "no-cache")
	request.Header.Set("Referer", DefaultBaseURL+"/live?rwebShell=1")
	request.Header.Set("X-Session-ID", sessionID)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if c.config.UserAgent != "" {
		request.Header.Set("User-Agent", c.config.UserAgent)
	}
	client := *c.config.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return RateLimit{}, fmt.Errorf("call X Studio: %w", err)
	}
	defer response.Body.Close()
	meta := parseRateLimit(response.Header)
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponse))
	if err != nil {
		return meta, fmt.Errorf("read X Studio response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return meta, &HTTPError{response.StatusCode, safeDetail(raw, c.config), meta}
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return meta, fmt.Errorf("decode X Studio response: %w", err)
	}
	return meta, nil
}

func checkEnvelope(raw json.RawMessage) error {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" || value == "{}" || value == "[]" {
		return nil
	}
	return errors.New("X Studio returned an application error")
}

func parseRateLimit(header http.Header) RateLimit {
	limit, _ := strconv.Atoi(header.Get("X-Rate-Limit-Limit"))
	remaining, _ := strconv.Atoi(header.Get("X-Rate-Limit-Remaining"))
	reset, _ := strconv.ParseInt(header.Get("X-Rate-Limit-Reset"), 10, 64)
	out := RateLimit{Limit: limit, Remaining: remaining}
	if reset > 0 {
		out.ResetAt = time.Unix(reset, 0).UTC()
	}
	return out
}

func safeDetail(raw []byte, config Config) string {
	value := string(raw)
	for _, secret := range []string{config.Cookie, config.IngestID} {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 500 {
		value = value[:500] + "…"
	}
	return value
}

func escapeQuoted(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
