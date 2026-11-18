// Package xapi uses the supported X OAuth 2 and v2 posting APIs.
package xapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

type Client struct {
	HTTP    *http.Client
	BaseURL string
}
type APIError struct {
	Status    int
	Ambiguous bool
}

func (e *APIError) Error() string {
	if e.Status == 0 {
		return "X did not return a complete response"
	}
	return fmt.Sprintf("X returned HTTP %d", e.Status)
}
func Config(id, secret, redirect string) *oauth2.Config {
	return &oauth2.Config{ClientID: id, ClientSecret: secret, RedirectURL: redirect, Scopes: []string{"tweet.read", "tweet.write", "users.read", "media.write", "offline.access"}, Endpoint: oauth2.Endpoint{AuthURL: "https://x.com/i/oauth2/authorize", TokenURL: "https://api.x.com/2/oauth2/token", AuthStyle: oauth2.AuthStyleInHeader}}
}
func (c Client) request(ctx context.Context, token, method, path string, body any, out any, posting bool) error {
	var raw []byte
	var e error
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.x.com"
	}
	req, e := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 40 * time.Second}
	}
	res, e := client.Do(req)
	if e != nil {
		return &APIError{Ambiguous: posting}
	}
	defer res.Body.Close()
	data, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if e != nil {
		return &APIError{Status: res.StatusCode, Ambiguous: posting}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &APIError{Status: res.StatusCode, Ambiguous: posting && (res.StatusCode >= 500 || res.StatusCode == 408)}
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return &APIError{Status: res.StatusCode, Ambiguous: posting}
	}
	return nil
}
func (c Client) Me(ctx context.Context, token string) (id, name string, err error) {
	var v struct{ Data struct{ ID, Username string } }
	err = c.request(ctx, token, "GET", "/2/users/me", nil, &v, false)
	if err == nil && (v.Data.ID == "" || v.Data.Username == "") {
		err = fmt.Errorf("X account identity is missing")
	}
	return v.Data.ID, v.Data.Username, err
}
func (c Client) UploadImage(ctx context.Context, token string, data []byte) (string, error) {
	if len(data) == 0 || len(data) > 5<<20 {
		return "", fmt.Errorf("X images must be at most 5 MB")
	}
	kind := http.DetectContentType(data)
	if kind != "image/jpeg" && kind != "image/png" && kind != "image/webp" {
		return "", fmt.Errorf("Use a JPEG, PNG or WebP image")
	}
	var v struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	err := c.request(ctx, token, "POST", "/2/media/upload", map[string]string{"media": base64.StdEncoding.EncodeToString(data), "media_category": "tweet_image"}, &v, false)
	if err == nil && v.Data.ID == "" {
		err = fmt.Errorf("X returned no media ID")
	}
	return v.Data.ID, err
}
func (c Client) Post(ctx context.Context, token, text, replyTo string, media []string) (string, error) {
	body := map[string]any{"text": text}
	if replyTo != "" {
		body["reply"] = map[string]string{"in_reply_to_tweet_id": replyTo}
	}
	if len(media) > 0 {
		body["media"] = map[string]any{"media_ids": media}
	}
	var v struct{ Data struct{ ID string } }
	err := c.request(ctx, token, "POST", "/2/tweets", body, &v, true)
	if err == nil && v.Data.ID == "" {
		err = &APIError{Ambiguous: true}
	}
	return v.Data.ID, err
}
func PostURL(id string) string { return "https://x.com/i/status/" + url.PathEscape(id) }
