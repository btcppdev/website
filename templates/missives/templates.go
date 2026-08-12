package conferencemissives

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"btcpp-web/internal/types"
)

//go:embed *.md
var templateFiles embed.FS

type Definition struct {
	Kind     string
	OnlyFor  string
	Label    string
	Title    string
	Markdown string
	Order    int
}

func OnlyFor(kind string) string {
	return types.ConferenceCampaignTemplateOnlyFor(strings.TrimSpace(kind))
}

func Definitions() ([]Definition, error) {
	entries, err := templateFiles.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("list conference missive templates: %w", err)
	}
	definitions := make([]Definition, 0, len(entries))
	seen := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := entry.Name()
		raw, err := templateFiles.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		meta, err := conferenceMissiveFrontmatter(string(raw))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		definition := Definition{
			Kind:     strings.TrimSpace(meta["kind"]),
			Label:    strings.TrimSpace(meta["label"]),
			Markdown: strings.TrimSpace(string(raw)),
		}
		definition.OnlyFor = OnlyFor(definition.Kind)
		definition.Title = types.ConferenceCampaignSubject(meta["title"])
		if definition.Order, err = strconv.Atoi(strings.TrimSpace(meta["order"])); err != nil {
			return nil, fmt.Errorf("order must be an integer")
		}
		if definition.Kind == "" || definition.Label == "" || strings.TrimSpace(meta["title"]) == "" {
			return nil, fmt.Errorf("kind, label, and title are required")
		}
		if previous := seen[definition.Kind]; previous != "" {
			return nil, fmt.Errorf("kind %q is duplicated in %s and %s", definition.Kind, previous, entry.Name())
		}
		seen[definition.Kind] = entry.Name()
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].Order != definitions[j].Order {
			return definitions[i].Order < definitions[j].Order
		}
		return definitions[i].Kind < definitions[j].Kind
	})
	return definitions, nil
}

func DefinitionForKind(kind string) (*Definition, error) {
	definitions, err := Definitions()
	if err != nil {
		return nil, err
	}
	for i := range definitions {
		if definitions[i].Kind == strings.TrimSpace(kind) {
			return &definitions[i], nil
		}
	}
	return nil, fmt.Errorf("conference missive template %q not found", kind)
}

func conferenceMissiveFrontmatter(markdown string) (map[string]string, error) {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	if !strings.HasPrefix(markdown, "---\n") {
		return nil, fmt.Errorf("frontmatter is required")
	}
	end := strings.Index(markdown[4:], "\n---")
	if end < 0 {
		return nil, fmt.Errorf("frontmatter is not closed")
	}
	meta := make(map[string]string)
	for _, line := range strings.Split(markdown[4:4+end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid frontmatter line %q", line)
		}
		meta[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return meta, nil
}
