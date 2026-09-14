// Package cloudflare implements bip353.Provider using Cloudflare's DNS API.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/internal/bip353"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

var ErrDNSSECInactive = errors.New("Cloudflare DNSSEC is not active")

type Client struct {
	token   string
	zoneID  string
	baseURL string
	http    *http.Client
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.http = client
		}
	}
}

// WithBaseURL overrides the API endpoint, primarily for tests.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(baseURL, "/") }
}

func New(token, zoneID string, options ...Option) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Cloudflare API token is required")
	}
	if strings.TrimSpace(zoneID) == "" {
		return nil, errors.New("Cloudflare zone ID is required")
	}
	c := &Client{
		token:   token,
		zoneID:  zoneID,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope[T any] struct {
	Success    bool       `json:"success"`
	Errors     []apiError `json:"errors"`
	Result     T          `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

type dnsRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     uint32 `json:"ttl"`
	Comment string `json:"comment,omitempty"`
}

func (c *Client) CheckDNSSEC(ctx context.Context) error {
	var response envelope[struct {
		Status string `json:"status"`
	}]
	if err := c.do(ctx, http.MethodGet, c.zonePath("/dnssec"), nil, &response); err != nil {
		return err
	}
	if response.Result.Status != "active" {
		return fmt.Errorf("%w (status %q)", ErrDNSSECInactive, response.Result.Status)
	}
	return nil
}

func (c *Client) ListTXT(ctx context.Context, name string) ([]bip353.Record, error) {
	var all []bip353.Record
	for page := 1; ; page++ {
		query := url.Values{
			"type":     {"TXT"},
			"name":     {name},
			"page":     {strconv.Itoa(page)},
			"per_page": {"100"},
		}
		var response envelope[[]dnsRecord]
		path := c.zonePath("/dns_records") + "?" + query.Encode()
		if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		for _, record := range response.Result {
			if record.Type == "TXT" && strings.EqualFold(strings.TrimSuffix(record.Name, "."), strings.TrimSuffix(name, ".")) {
				all = append(all, fromAPI(record))
			}
		}
		if response.ResultInfo.TotalPages <= page {
			return all, nil
		}
	}
}

func (c *Client) CreateTXT(ctx context.Context, record bip353.Record) (bip353.Record, error) {
	body := toAPI(record)
	var response envelope[dnsRecord]
	if err := c.do(ctx, http.MethodPost, c.zonePath("/dns_records"), body, &response); err != nil {
		return bip353.Record{}, err
	}
	return fromAPI(response.Result), nil
}

func (c *Client) UpdateTXT(ctx context.Context, record bip353.Record) (bip353.Record, error) {
	if record.ID == "" {
		return bip353.Record{}, errors.New("record ID is required for update")
	}
	body := toAPI(record)
	var response envelope[dnsRecord]
	if err := c.do(ctx, http.MethodPut, c.zonePath("/dns_records/")+url.PathEscape(record.ID), body, &response); err != nil {
		return bip353.Record{}, err
	}
	return fromAPI(response.Result), nil
}

func (c *Client) DeleteTXT(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("record ID is required for delete")
	}
	var response envelope[struct {
		ID string `json:"id"`
	}]
	return c.do(ctx, http.MethodDelete, c.zonePath("/dns_records/")+url.PathEscape(id), nil, &response)
}

func (c *Client) zonePath(suffix string) string {
	return "/zones/" + url.PathEscape(c.zoneID) + suffix
}

func toAPI(record bip353.Record) dnsRecord {
	return dnsRecord{
		Type:    "TXT",
		Name:    record.Name,
		Content: record.Content,
		TTL:     record.TTL,
		Comment: "Managed by btcpp-web/internal/bip353",
	}
}

func fromAPI(record dnsRecord) bip353.Record {
	return bip353.Record{ID: record.ID, Name: record.Name, Content: record.Content, TTL: record.TTL}
}

func (c *Client) do(ctx context.Context, method, path string, requestBody, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	if err := decoder.Decode(responseBody); err != nil {
		return fmt.Errorf("Cloudflare API returned HTTP %d with an invalid response: %w", resp.StatusCode, err)
	}

	apiResponse, ok := responseBody.(interface {
		apiErrors() (bool, []apiError)
	})
	if ok {
		success, apiErrors := apiResponse.apiErrors()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || !success {
			return formatAPIError(resp.StatusCode, apiErrors)
		}
		return nil
	}

	// All internal callers use *envelope[T]. This fallback keeps failures safe
	// if a future caller accidentally supplies a different response type.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Cloudflare API returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (e *envelope[T]) apiErrors() (bool, []apiError) { return e.Success, e.Errors }

func formatAPIError(status int, apiErrors []apiError) error {
	if len(apiErrors) == 0 {
		return fmt.Errorf("Cloudflare API returned HTTP %d", status)
	}
	messages := make([]string, 0, len(apiErrors))
	for _, item := range apiErrors {
		messages = append(messages, fmt.Sprintf("%d: %s", item.Code, item.Message))
	}
	return fmt.Errorf("Cloudflare API returned HTTP %d: %s", status, strings.Join(messages, "; "))
}
