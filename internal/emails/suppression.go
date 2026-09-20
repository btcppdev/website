package emails

import (
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SuppressJob acknowledges only a durable mailer suppression, never a generic
// asynchronous acceptance. Replays and cancellation-before-enqueue are safe.
func SuppressJob(ctx context.Context, app *config.AppContext, key string) error {
	if key == "" || len(key) > 256 || strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("invalid mail job key")
	}
	endpoint, err := url.Parse(strings.TrimRight(app.Env.MailEndpoint, "/") + "/suppression")
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || app.Env.MailerSecret == "" {
		return errors.New("mailer suppression is not configured")
	}
	local := endpoint.Hostname() == "localhost" || endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "::1"
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && local) {
		return errors.New("unsafe mailer suppression endpoint")
	}
	payload, _ := json.Marshal(struct {
		Scope string `json:"scope"`
		Key   string `json:"key"`
	}{"job", key})
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	addAuthStamp(app, request)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("mailer suppression not confirmed")
	}
	var result struct {
		Success bool `json:"success"`
		Code    int  `json:"code"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4097))
	if err := decoder.Decode(&result); err != nil || !result.Success || result.Code != 200 {
		return errors.New("invalid mailer suppression acknowledgment")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid mailer suppression acknowledgment")
	}
	return nil
}
