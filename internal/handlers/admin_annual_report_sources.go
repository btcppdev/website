package handlers

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// New events enter the live report automatically; only explicit exclusions
// persist. Legacy selections retain their exclusions for events already complete
// when saved, while newly completed events are picked up without another click.
func annualEventIncluded(p annualReportPage, event reportEvent) bool {
	if p.Saved["event_selection_mode"] == "exclude" {
		return p.Saved["exclude_event_"+event.ID] != "yes"
	}
	if p.Saved["selection_saved"] != "yes" || p.Saved["include_"+event.ID] == "yes" {
		return true
	}
	return p.DraftUpdatedAt != nil && event.End != nil && event.End.After(*p.DraftUpdatedAt)
}

type annualSourceChoice struct {
	Key, Title, Detail, URL string
	Excluded                bool
}

func (m reportMedia) ExclusionKey() string { return "exclude_media_" + m.Platform + "_" + m.ID }
func (p reportPost) ExclusionKey() string  { return "exclude_post_" + p.ID }

func (p annualReportPage) SourceChoices() []annualSourceChoice {
	var media []reportMedia
	var posts []reportPost
	for _, e := range p.Included() {
		if e.SourceMedia != nil {
			media = append(media, e.SourceMedia...)
		} else {
			media = append(media, e.Media...)
		}
		if e.SourcePosts != nil {
			posts = append(posts, e.SourcePosts...)
		} else {
			posts = append(posts, e.Posts...)
		}
	}
	var choices []annualSourceChoice
	for _, m := range uniqueReportMedia(media) {
		key := m.ExclusionKey()
		choices = append(choices, annualSourceChoice{key, m.Title,
			fmt.Sprintf("%s · %s · %s views · %s", m.Event, m.Platform, compactReportSats(m.Views), m.CollectionDate()),
			m.URL, p.Saved[key] == "yes"})
	}
	for _, post := range uniqueReportPosts(posts) {
		key := post.ExclusionKey()
		choices = append(choices, annualSourceChoice{key, post.Text,
			fmt.Sprintf("X post · %s impressions · collected %s", post.Display(), post.Collected),
			post.URL(), p.Saved[key] == "yes"})
	}
	return choices
}

var annualSourceKey = regexp.MustCompile(`^exclude_(media_(YouTube|X)_[A-Za-z0-9_-]{1,100}|post_[A-Za-z0-9_-]{1,100})$`)

func saveAnnualExclusions(p *annualReportPage, form url.Values, saved map[string]string) error {
	// Keep choices for sources that temporarily disappear or are not in this form.
	for key, value := range p.Saved {
		if strings.HasPrefix(key, "exclude_") {
			saved[key] = value
		}
	}
	if form.Get("event_selection_mode") == "exclude" {
		saved["event_selection_mode"] = "exclude"
		included := map[string]bool{}
		for _, id := range form["event"] {
			included[id] = true
		}
		for _, id := range form["event_seen"] {
			if _, err := uuid.Parse(id); err != nil {
				return fmt.Errorf("invalid event exclusion")
			}
			key := "exclude_event_" + id
			delete(saved, key)
			if !included[id] {
				saved[key] = "yes"
			}
		}
	}
	if form.Get("action") == "include_all" {
		saved["event_selection_mode"] = "exclude"
		for key := range saved {
			if strings.HasPrefix(key, "exclude_event_") {
				delete(saved, key)
			}
		}
	}
	for _, key := range form["source_seen"] {
		if !annualSourceKey.MatchString(key) {
			return fmt.Errorf("invalid report source exclusion")
		}
		delete(saved, key)
		if form.Get(key) == "yes" {
			saved[key] = "yes"
		}
	}
	return nil
}
