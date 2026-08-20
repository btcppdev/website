// Package publicid owns the stable, public-facing identifiers used by
// /whois and API person links. Keeping this outside either HTTP surface keeps
// browser pages and JSON clients from inventing different profile URLs.
package publicid

import (
	"fmt"
	"net/url"
	"strings"

	"btcpp-web/internal/types"
)

// AssignSpeakers returns a person-ID-to-public-ID map. Duplicate preferred
// handles receive a stable suffix derived from the canonical person UUID.
func AssignSpeakers(speakers []*types.Speaker) map[string]string {
	publicIDs := make(map[string]string, len(speakers))
	bases := make(map[string]string, len(speakers))
	counts := make(map[string]int, len(speakers))
	for _, speaker := range speakers {
		if speaker == nil || strings.TrimSpace(speaker.ID) == "" {
			continue
		}
		base := SpeakerSlug(speaker)
		bases[speaker.ID] = base
		counts[base]++
	}

	used := map[string]bool{}
	for _, speaker := range speakers {
		if speaker == nil || strings.TrimSpace(speaker.ID) == "" {
			continue
		}
		base := bases[speaker.ID]
		slug := base
		if counts[base] > 1 {
			suffix := strings.ReplaceAll(speaker.ID, "-", "")
			if len(suffix) > 8 {
				suffix = suffix[:8]
			}
			slug = strings.Trim(base+"-"+suffix, "-")
			for n := 2; used[slug]; n++ {
				slug = fmt.Sprintf("%s-%d", strings.Trim(base, "-"), n)
			}
		}
		used[slug] = true
		publicIDs[speaker.ID] = slug
	}
	return publicIDs
}

func SpeakerSlug(speaker *types.Speaker) string {
	if speaker == nil {
		return "speaker"
	}
	for _, raw := range []string{
		ProfileHandle(speaker.Github, "github.com"),
		speaker.Twitter.Handle,
		speaker.Name,
	} {
		if slug := Slug(raw); slug != "" {
			return slug
		}
	}
	id := strings.ReplaceAll(speaker.ID, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	if id == "" {
		return "speaker"
	}
	return "speaker-" + id
}

func ProfileHandle(raw string, host string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimPrefix(raw, "@")
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	if strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		u, err := url.Parse(raw)
		if err != nil || strings.TrimPrefix(strings.ToLower(u.Host), "www.") != host {
			return ""
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			return ""
		}
		raw = parts[0]
	} else {
		lower := strings.TrimPrefix(strings.ToLower(raw), "www.")
		if strings.HasPrefix(lower, host+"/") {
			raw = raw[len(host)+1:]
		}
	}
	raw = strings.Trim(raw, " /")
	if idx := strings.IndexAny(raw, "/?#"); idx >= 0 {
		raw = raw[:idx]
	}
	if host == "github.com" && !validGitHubHandle(raw) {
		return ""
	}
	return raw
}

func Slug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func validGitHubHandle(handle string) bool {
	if handle == "" || len(handle) > 39 || handle[0] == '-' || handle[len(handle)-1] == '-' {
		return false
	}
	lastHyphen := false
	for _, r := range handle {
		if r == '-' {
			if lastHyphen {
				return false
			}
			lastHyphen = true
			continue
		}
		lastHyphen = false
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
