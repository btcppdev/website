// Package i18n owns server-rendered messages and opt-in event translations.
package i18n

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"text/template"
)

//go:embed messages/*.json events/*.json
var files embed.FS

type EventCatalog struct {
	Event    string `json:"event"`
	Locale   string `json:"locale"`
	Language string `json:"language"`
	// Exact source matching prevents an old translation overriding edited copy.
	Content map[string]string `json:"content"`
}

type Catalog struct {
	Messages map[string]map[string]*template.Template
	Events   []EventCatalog
}

func Load() (*Catalog, error) {
	c := &Catalog{Messages: map[string]map[string]*template.Template{}}
	entries, err := files.ReadDir("messages")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := files.ReadFile("messages/" + entry.Name())
		if err != nil {
			return nil, err
		}
		values := map[string]string{}
		if err = json.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		locale := strings.TrimSuffix(entry.Name(), ".json")
		c.Messages[locale] = map[string]*template.Template{}
		for key, value := range values {
			t, err := template.New(key).Option("missingkey=error").Parse(value)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", locale, key, err)
			}
			c.Messages[locale][key] = t
		}
	}
	if c.Messages["en"] == nil {
		return nil, fmt.Errorf("English message catalog required")
	}
	for lang, messages := range c.Messages {
		for key := range messages {
			if c.Messages["en"][key] == nil {
				return nil, fmt.Errorf("unknown message %s/%s", lang, key)
			}
		}
	}
	entries, err = files.ReadDir("events")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := files.ReadFile("events/" + entry.Name())
		if err != nil {
			return nil, err
		}
		var event EventCatalog
		if err = json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		if event.Event == "" || event.Locale == "en" || c.Messages[event.Locale] == nil {
			return nil, fmt.Errorf("invalid event catalog %s", entry.Name())
		}
		c.Events = append(c.Events, event)
	}
	return c, nil
}

func (c *Catalog) Message(locale, key string, args ...map[string]any) (string, error) {
	t := c.Messages[locale][key]
	if t == nil {
		t = c.Messages["en"][key]
	}
	if t == nil {
		return "", fmt.Errorf("unknown translation key %q", key)
	}
	data := map[string]any{}
	if len(args) > 0 {
		data = args[0]
	}
	var out bytes.Buffer
	err := t.Execute(&out, data)
	return out.String(), err
}

func (c *Catalog) Supports(event, locale string) bool {
	if locale == "en" {
		return true
	}
	for _, entry := range c.Events {
		if entry.Event == event && entry.Locale == locale {
			return true
		}
	}
	return false
}

func (c *Catalog) Content(event, locale, source string) string {
	for _, entry := range c.Events {
		if entry.Event == event && entry.Locale == locale {
			if value, ok := entry.Content[source]; ok {
				return value
			}
		}
	}
	return source
}

type LanguageLink struct {
	Locale, Label, URL string
	Current            bool
}

func (c *Catalog) Languages(event, current string) []LanguageLink {
	var out []LanguageLink
	for _, entry := range c.Events {
		if entry.Event == event {
			out = append(out, LanguageLink{entry.Locale, entry.Language, "/" + entry.Locale + "/" + event, entry.Locale == current})
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Locale < out[j].Locale })
	return append([]LanguageLink{{"en", "English", "/" + event, current == "en"}}, out...)
}

// URL changes only translated landing pages, never inventing translated
// agenda, checkout, account, or hackathon routes.
func (c *Catalog) URL(locale, path string) string {
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" {
		return path
	}
	event := strings.TrimPrefix(u.Path, "/")
	if locale != "en" && c.Supports(event, locale) {
		u.Path = "/" + locale + "/" + event
	}
	return u.String()
}

// CheckoutURL preserves payment/discount parameters while carrying the language
// through existing checkout routes. Provider callbacks use the same return URL.
func CheckoutURL(locale, path string) string {
	if locale == "en" || locale == "" {
		return path
	}
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	query := u.Query()
	query.Set("lang", locale)
	u.RawQuery = query.Encode()
	return u.String()
}
