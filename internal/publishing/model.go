// Package publishing owns the shared calendar and durable social delivery queue.
package publishing

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Slot struct {
	Day  int    `json:"day"`
	Time string `json:"time"`
}
type Channel struct {
	RouteEventPosts bool   `json:"route_event_posts"`
	ID              string `json:"id"`
	Platform        string `json:"platform"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	RemoteID        string `json:"remote_id"`
	Timezone        string `json:"timezone"`
	Slots           []Slot `json:"slots"`
	Paused          bool   `json:"paused"`
	Connected       bool   `json:"connected"`
}
type Media struct {
	URL  string `json:"url"`
	Kind string `json:"kind"`
}
type Delivery struct {
	ClientKey  string     `json:"client_key"`
	ID         string     `json:"id"`
	CampaignID string     `json:"campaign_id"`
	ChannelID  string     `json:"channel_id"`
	Conference string     `json:"conference"`
	SourceRef  string     `json:"source_ref"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Reply      string     `json:"reply"`
	Media      []Media    `json:"media"`
	Mode       string     `json:"mode"`
	State      string     `json:"state"`
	At         *time.Time `json:"at"`
	RootID     string     `json:"root_id"`
	URL        string     `json:"url"`
	Error      string     `json:"error"`
	Version    int        `json:"version"`
	Attempts   int        `json:"attempts"`
}
type Entry struct {
	ID         string     `json:"id"`
	ChannelID  string     `json:"channel_id"`
	Conference string     `json:"conference"`
	Title      string     `json:"title"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	At         *time.Time `json:"at"`
	End        *time.Time `json:"end"`
	URL        string     `json:"url"`
	Provider   string     `json:"provider"`
}

var ErrChanged = errors.New("This item changed. Refresh before trying again.")

func ValidateSlots(zone string, slots []Slot) error {
	if _, err := time.LoadLocation(zone); err != nil {
		return fmt.Errorf("Choose a valid IANA timezone")
	}
	if len(slots) > 100 {
		return fmt.Errorf("At most 100 weekly slots are supported")
	}
	seen := map[string]bool{}
	for _, s := range slots {
		if s.Day < 0 || s.Day > 6 {
			return fmt.Errorf("Invalid weekday")
		}
		t, e := time.Parse("15:04", s.Time)
		if e != nil || t.Format("15:04") != s.Time {
			return fmt.Errorf("Slot times must use HH:MM")
		}
		key := fmt.Sprintf("%d/%s", s.Day, s.Time)
		if seen[key] {
			return fmt.Errorf("Duplicate posting slot")
		}
		seen[key] = true
	}
	return nil
}

// SlotsBetween uses local calendar dates, skips nonexistent spring-forward times,
// and emits one occurrence during the fall-back repeated hour.
func SlotsBetween(zone string, slots []Slot, start, end time.Time) []time.Time {
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return nil
	}
	local := start.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
	var out []time.Time
	for i := 0; i < 740 && day.Add(-24*time.Hour).Before(end); i++ {
		for _, s := range slots {
			if int(day.Weekday()) != s.Day {
				continue
			}
			parsed, err := time.Parse("15:04", s.Time)
			if err != nil {
				continue
			}
			at := time.Date(day.Year(), day.Month(), day.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
			if at.Hour() != parsed.Hour() || at.Minute() != parsed.Minute() {
				continue
			}
			if !at.Before(start) && at.Before(end) {
				out = append(out, at.UTC())
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
func ValidateDelivery(d Delivery, now time.Time) error {
	if strings.TrimSpace(d.Body) == "" || len(d.Body) > 10000 || len(d.Reply) > 10000 {
		return fmt.Errorf("Add post text (at most 10,000 bytes per post)")
	}
	if len(d.Title) > 240 || len(d.Conference) > 120 {
		return fmt.Errorf("Title or event is too long")
	}
	if d.Mode != "draft" && d.Mode != "queue" && d.Mode != "fixed" {
		return fmt.Errorf("Choose draft, queue, or a specific time")
	}
	if d.Mode == "fixed" && (d.At == nil || !d.At.After(now.Add(time.Minute))) {
		return fmt.Errorf("Choose a time at least one minute in the future")
	}
	if len(d.Media) > 4 {
		return fmt.Errorf("X supports at most four images")
	}
	for _, m := range d.Media {
		if m.Kind != "image" {
			return fmt.Errorf("Direct X publishing currently accepts images; schedule videos through recordings")
		}
	}
	return nil
}
